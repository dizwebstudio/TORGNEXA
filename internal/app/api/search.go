package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/searchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/search"
)

const (
	ProductSearchPath = "/api/v1/products"
	OrderSearchPath   = "/api/v1/orders"
	DemoOrdersPath    = "/api/v1/orders/demo"
)

// SearchScopeResolver returns tenant/workspace scope derived from authenticated
// request identity. Search endpoints never trust tenant identifiers supplied in
// query parameters or request bodies.
type SearchScopeResolver interface {
	SearchScope(*http.Request) (tenancy.Scope, error)
}

type contextSearchScopeResolver struct{}

func (contextSearchScopeResolver) SearchScope(request *http.Request) (tenancy.Scope, error) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok {
		return tenancy.Scope{}, ErrUnauthorized
	}
	return scope, nil
}

// newSearchRoutes mounts PostgreSQL-backed search inside the mandatory
// production authentication/tenant/authorization composition.
func newSearchRoutes(engine search.Provider, auditService auditCapturer) []ProtectedRoute {
	resolver := contextSearchScopeResolver{}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ProductSearchPath, Permission: "products.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { productSearch(w, r, engine, resolver) })},
		{Method: http.MethodGet, Path: OrderSearchPath, Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { orderSearch(w, r, engine, resolver) })},
		{Method: http.MethodGet, Path: OrderSearchPath + "/", PathPrefix: true, Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := ScopeFromContext(r.Context())
			if !ok {
				writeProblem(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			id := strings.TrimPrefix(r.URL.Path, OrderSearchPath+"/")
			if id == "" || strings.Contains(id, "/") {
				writeProblem(w, http.StatusNotFound, "Not Found")
				return
			}
			repository, ok := engine.(interface {
				OrderDetail(context.Context, tenancy.Scope, string) (searchrepo.OrderDetail, error)
			})
			if !ok {
				writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
				return
			}
			detail, err := repository.OrderDetail(r.Context(), scope, id)
			if errors.Is(err, sql.ErrNoRows) {
				writeProblem(w, http.StatusNotFound, "Not Found")
				return
			}
			if err != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})},
		{Method: http.MethodPost, Path: DemoOrdersPath, Permission: "orders.demo.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := ScopeFromContext(r.Context())
			principal, principalOK := PrincipalFromContext(r.Context())
			if !ok || !principalOK || principal.Subject == "" {
				writeProblem(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			repository, ok := engine.(interface {
				SeedDemoOrders(context.Context, tenancy.Scope, string) (int, error)
			})
			if !ok {
				writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
				return
			}
			count, err := repository.SeedDemoOrders(r.Context(), scope, principal.Subject)
			if err != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			if auditService == nil {
				writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
				return
			}
			correlationID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if correlationID == "" {
				correlationID = "demo-dataset:create"
			}
			if _, err := auditService.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: "demo.dataset.created", ResourceType: "demo_dataset", ResourceID: scope.WorkspaceID().String(), CorrelationID: correlationID, Risk: audit.RiskWriteSafe, Summary: audit.Summary{"orders_created": count, "synthetic": true}}); err != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int{"created": count})
		})},
		{Method: http.MethodDelete, Path: DemoOrdersPath, Permission: "orders.demo.delete", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := ScopeFromContext(r.Context())
			principal, principalOK := PrincipalFromContext(r.Context())
			if !ok || !principalOK || principal.Subject == "" {
				writeProblem(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			// Sensitive deletion is executed only by the approval execution route,
			// after an exact action/resource grant wins optimistic ownership.
			writeProblem(w, http.StatusConflict, "Approval required")
			return
		})},
	}
}

// newHandlerWithSearch mounts tenant-authorized product/order search over the
// provider-neutral SearchProvider boundary.
func newHandlerWithSearch(logger *slog.Logger, engine search.Provider, resolver SearchScopeResolver) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ProductSearchPath:
			productSearch(w, r, engine, resolver)
		case OrderSearchPath:
			orderSearch(w, r, engine, resolver)
		default:
			route(w, r)
		}
	}))
}

func productSearch(w http.ResponseWriter, r *http.Request, engine search.Provider, resolver SearchScopeResolver) {
	if !searchMethod(w, r) {
		return
	}
	if engine == nil || resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, err := resolver.SearchScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	limit, ok := searchLimit(w, r)
	if !ok {
		return
	}
	query := search.ProductQuery{
		Text:   r.URL.Query().Get("q"),
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	}
	if query.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	page, err := engine.SearchProducts(r.Context(), scope, query)
	if err != nil {
		if errors.Is(err, search.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeSearchJSON(w, r, page)
}

func orderSearch(w http.ResponseWriter, r *http.Request, engine search.Provider, resolver SearchScopeResolver) {
	if !searchMethod(w, r) {
		return
	}
	if engine == nil || resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, err := resolver.SearchScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	limit, ok := searchLimit(w, r)
	if !ok {
		return
	}
	from, ok := searchTime(w, r.URL.Query().Get("placed_from"))
	if !ok {
		return
	}
	to, ok := searchTime(w, r.URL.Query().Get("placed_to"))
	if !ok {
		return
	}
	query := search.OrderQuery{
		Text:       r.URL.Query().Get("q"),
		Status:     r.URL.Query().Get("status"),
		PlacedFrom: from,
		PlacedTo:   to,
		Limit:      limit,
		Cursor:     r.URL.Query().Get("cursor"),
	}
	if query.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	page, err := engine.SearchOrders(r.Context(), scope, query)
	if err != nil {
		if errors.Is(err, search.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeSearchJSON(w, r, page)
}

func searchMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
	return false
}

func searchLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return 0, false
		}
		limit = parsed
	}
	if limit < 1 || limit > search.MaxPageSize {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return 0, false
	}
	return limit, true
}

func searchTime(w http.ResponseWriter, raw string) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || parsed.Location() != time.UTC {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return nil, false
	}
	parsed = parsed.UTC()
	return &parsed, true
}

func writeSearchJSON(w http.ResponseWriter, r *http.Request, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = jsonEncode(w, value)
	}
}
