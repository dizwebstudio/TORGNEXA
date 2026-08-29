package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

var (
	ErrSecurityCompositionInvalid = errors.New("api: invalid security composition")
	ErrUnauthenticated            = errors.New("api: unauthenticated")
	ErrUnauthorized               = errors.New("api: unauthorized")
)

// Principal is the minimal authenticated identity propagated to application
// handlers. Raw bearer tokens and unbounded identity-provider claims are never
// copied into request context.
type Principal struct {
	Issuer         string
	Subject        string
	SessionRef     string
	SubjectRef     string
	Email          string
	Profile        userprofile.Identity
	Roles          []string
	OrganizationID string
	WorkspaceID    string
}

func (p Principal) Valid() bool {
	return strings.TrimSpace(p.Issuer) != "" && strings.TrimSpace(p.Subject) != ""
}

// Authenticator validates the caller credential and returns a minimized
// identity. Implementations own OIDC/JWT/session validation; composition never
// accepts identity directly from untrusted HTTP headers.
type Authenticator interface {
	Authenticate(context.Context, *http.Request) (Principal, error)
}

// TenantResolver resolves the canonical organization/workspace scope after
// authentication. A client-controlled tenant selector is insufficient on its
// own: the resolver must prove that the authenticated principal is linked to
// the returned scope.
type TenantResolver interface {
	ResolveTenant(context.Context, Principal, *http.Request) (tenancy.Scope, error)
}

// Authorizer makes the final permission decision for an authenticated
// principal inside the resolved tenant scope.
type Authorizer interface {
	Authorize(context.Context, Principal, tenancy.Scope, string) error
}

// ProtectedRoute is the only production registration surface for non-public
// HTTP operations. Private routes require a non-empty permission and pass
// authentication -> tenant resolution -> authorization in that exact order.
type ProtectedRoute struct {
	Method     string
	Path       string
	Permission string
	AdminOnly  bool
	PathPrefix bool
	Handler    http.Handler
}

// PublicWebhookRoute is the only production registration surface for
// unauthenticated, provider-verified inbound callbacks (ADR-0105). It never
// passes through Authenticator/TenantResolver/Authorizer and never populates
// PrincipalFromContext/ScopeFromContext — there is no principal to attach.
// The registered handler is solely responsible for resolving its own tenant
// scope from the URL and verifying the caller's authenticity (e.g. by
// re-fetching the claimed resource from the provider's own API) before
// trusting anything in the request. Every path must live under
// webhookPathPrefix, keeping this class of route structurally impossible to
// collide with or be confused for an authenticated ProtectedRoute.
type PublicWebhookRoute struct {
	Method     string
	Path       string
	PathPrefix bool
	Handler    http.Handler
}

const webhookPathPrefix = "/api/v1/webhooks/"

// webhookRatePerMinute and webhookMaxBodyBytes are fixed, not derived from
// the tenant-facing securityedge.Config, so one aggressive or misbehaving
// webhook source can never borrow budget from — or starve — authenticated
// tenant traffic, and vice versa.
const webhookRatePerMinute = 240
const webhookMaxBodyBytes = 1 << 20

type securityDependencies struct {
	authenticator Authenticator
	tenant        TenantResolver
	authorizer    Authorizer
}

type requestIdentityKey struct{}
type requestScopeKey struct{}
type requestClientIPKey struct{}

// PrincipalFromContext returns the minimized authenticated identity attached by
// the production composition root.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	value, ok := ctx.Value(requestIdentityKey{}).(Principal)
	return value, ok && value.Valid()
}

// ScopeFromContext returns the canonical tenant scope attached only after
// authorization succeeds.
func ScopeFromContext(ctx context.Context) (tenancy.Scope, bool) {
	value, ok := ctx.Value(requestScopeKey{}).(tenancy.Scope)
	return value, ok && value.Valid()
}

// ClientIPFromContext returns the securityedge-validated client IP.
func ClientIPFromContext(ctx context.Context) (netip.Addr, bool) {
	value, ok := ctx.Value(requestClientIPKey{}).(netip.Addr)
	return value, ok && value.IsValid()
}

// NewProductionHandler creates the mandatory runtime request composition.
// Startup fails closed when a private route is configured without every
// security dependency. webhooks registers the separate, unauthenticated
// PublicWebhookRoute table (ADR-0105); pass nil when a deployment has none.
func NewProductionHandler(logger *slog.Logger, edge securityedge.Config, limiter *securityedge.Limiter, authn Authenticator, tenant TenantResolver, authz Authorizer, routes []ProtectedRoute, webhooks []PublicWebhookRoute) (http.Handler, error) {
	if logger == nil || edge.Validate() != nil || limiter == nil {
		return nil, ErrSecurityCompositionInvalid
	}
	deps := securityDependencies{authenticator: authn, tenant: tenant, authorizer: authz}
	table := make(map[string]ProtectedRoute, len(routes)+1)
	prefixes := make([]ProtectedRoute, 0)
	publicHealth := ProtectedRoute{Method: http.MethodGet, Path: HealthPath, Handler: http.HandlerFunc(health)}
	table[routeKey(publicHealth.Method, publicHealth.Path)] = publicHealth
	table[routeKey(http.MethodHead, HealthPath)] = ProtectedRoute{Method: http.MethodHead, Path: HealthPath, Handler: http.HandlerFunc(health)}

	for _, route := range routes {
		if err := validateProtectedRoute(route, deps); err != nil {
			return nil, err
		}
		if route.PathPrefix {
			for _, existing := range prefixes {
				if existing.Method == route.Method && existing.Path == route.Path {
					return nil, fmt.Errorf("%w: duplicate route", ErrSecurityCompositionInvalid)
				}
			}
			prefixes = append(prefixes, route)
			continue
		}
		key := routeKey(route.Method, route.Path)
		if _, exists := table[key]; exists {
			return nil, fmt.Errorf("%w: duplicate route", ErrSecurityCompositionInvalid)
		}
		table[key] = route
	}

	webhookTable := make(map[string]PublicWebhookRoute, len(webhooks))
	webhookPrefixes := make([]PublicWebhookRoute, 0)
	for _, route := range webhooks {
		if err := validatePublicWebhookRoute(route); err != nil {
			return nil, err
		}
		if route.PathPrefix {
			for _, existing := range webhookPrefixes {
				if existing.Method == route.Method && existing.Path == route.Path {
					return nil, fmt.Errorf("%w: duplicate webhook route", ErrSecurityCompositionInvalid)
				}
			}
			webhookPrefixes = append(webhookPrefixes, route)
			continue
		}
		key := routeKey(route.Method, route.Path)
		if _, exists := webhookTable[key]; exists {
			return nil, fmt.Errorf("%w: duplicate webhook route", ErrSecurityCompositionInvalid)
		}
		webhookTable[key] = route
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveComposedRoute(w, r, edge, limiter, deps, table, prefixes, webhookTable, webhookPrefixes)
	})
	return recoverPanics(logger, handler), nil
}

func validatePublicWebhookRoute(route PublicWebhookRoute) error {
	if route.Handler == nil || route.Method == "" || route.Method != strings.ToUpper(route.Method) || route.Path == "" || !strings.HasPrefix(route.Path, webhookPathPrefix) || strings.ContainsAny(route.Path, "?#") {
		return ErrSecurityCompositionInvalid
	}
	if route.PathPrefix && !strings.HasSuffix(route.Path, "/") {
		return ErrSecurityCompositionInvalid
	}
	return nil
}

func validateProtectedRoute(route ProtectedRoute, deps securityDependencies) error {
	if route.Handler == nil || route.Method == "" || route.Method != strings.ToUpper(route.Method) || route.Path == "" || !strings.HasPrefix(route.Path, "/api/v1/") || strings.ContainsAny(route.Path, "?#") {
		return ErrSecurityCompositionInvalid
	}
	if route.Path == HealthPath {
		return ErrSecurityCompositionInvalid
	}
	if route.PathPrefix && !strings.HasSuffix(route.Path, "/") {
		return ErrSecurityCompositionInvalid
	}
	if strings.TrimSpace(route.Permission) == "" || deps.authenticator == nil || deps.tenant == nil || deps.authorizer == nil {
		return ErrSecurityCompositionInvalid
	}
	return nil
}

func routeKey(method, path string) string { return method + " " + path }

func serveComposedRoute(w http.ResponseWriter, r *http.Request, edge securityedge.Config, limiter *securityedge.Limiter, deps securityDependencies, table map[string]ProtectedRoute, prefixes []ProtectedRoute, webhookTable map[string]PublicWebhookRoute, webhookPrefixes []PublicWebhookRoute) {
	for name, value := range securityedge.SecurityHeaders(edge) {
		w.Header().Set(name, value)
	}
	w.Header().Set("Cache-Control", "no-store")

	clientIP, err := securityedge.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), edge)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}

	// Public webhook routes are checked, and dispatched, before any of the
	// tenant-authenticated machinery below: they carry their own rate-limit
	// budget and body bound (ADR-0105) precisely so they never compete with,
	// or get gated behind, authenticated tenant traffic. They never reach
	// CORS/Origin handling either — no browser ever calls this path.
	if strings.HasPrefix(r.URL.Path, webhookPathPrefix) {
		serveWebhookRoute(w, r, limiter, clientIP, webhookTable, webhookPrefixes)
		return
	}

	if err = limiter.Allow(clientIP.String(), edge); err != nil {
		if errors.Is(err, securityedge.ErrRateLimited) {
			w.Header().Set("Retry-After", "60")
			writeProblem(w, http.StatusTooManyRequests, "Too Many Requests")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if !securityedge.OriginAllowed(origin, edge) {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		if !securityedge.CSRFAllowed(r.Method, origin, edge) {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, edge.MaxRequestBytes)
	}

	route, ok := table[routeKey(r.Method, r.URL.Path)]
	if !ok {
		for _, candidate := range prefixes {
			if candidate.Method == r.Method && strings.HasPrefix(r.URL.Path, candidate.Path) && (!ok || len(candidate.Path) > len(route.Path)) {
				route, ok = candidate, true
			}
		}
	}
	if !ok {
		if r.URL.Path == HealthPath {
			w.Header().Set("Allow", "GET, HEAD")
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}

	ctx := context.WithValue(r.Context(), requestClientIPKey{}, clientIP)
	if route.Path == HealthPath {
		route.Handler.ServeHTTP(w, r.WithContext(ctx))
		return
	}

	principal, err := deps.authenticator.Authenticate(ctx, r)
	if err != nil || !principal.Valid() {
		w.Header().Set("WWW-Authenticate", `Bearer realm="torgnexa-api"`)
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	ctx = context.WithValue(ctx, requestIdentityKey{}, principal)
	scope, err := deps.tenant.ResolveTenant(ctx, principal, r.WithContext(ctx))
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	if route.AdminOnly && !securityedge.AdminAllowed(clientIP, edge) {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	if err = deps.authorizer.Authorize(ctx, principal, scope, route.Permission); err != nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	ctx = context.WithValue(ctx, requestScopeKey{}, scope)
	route.Handler.ServeHTTP(w, r.WithContext(ctx))
}

// serveWebhookRoute dispatches a PublicWebhookRoute under its own fixed
// rate-limit budget and body bound (ADR-0105), independent of the
// tenant-facing securityedge.Config the rest of this file uses. It never
// authenticates, resolves a tenant, or authorizes — the dispatched handler
// owns proving the caller is who it claims to be before trusting anything in
// the request, and never sees PrincipalFromContext/ScopeFromContext because
// neither is ever populated on this path.
func serveWebhookRoute(w http.ResponseWriter, r *http.Request, limiter *securityedge.Limiter, clientIP netip.Addr, table map[string]PublicWebhookRoute, prefixes []PublicWebhookRoute) {
	if err := limiter.Allow("webhook:"+clientIP.String(), securityedge.Config{RatePerMinute: webhookRatePerMinute}); err != nil {
		if errors.Is(err, securityedge.ErrRateLimited) {
			w.Header().Set("Retry-After", "60")
			writeProblem(w, http.StatusTooManyRequests, "Too Many Requests")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, webhookMaxBodyBytes)
	}
	route, ok := table[routeKey(r.Method, r.URL.Path)]
	if !ok {
		for _, candidate := range prefixes {
			if candidate.Method == r.Method && strings.HasPrefix(r.URL.Path, candidate.Path) && (!ok || len(candidate.Path) > len(route.Path)) {
				route, ok = candidate, true
			}
		}
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	route.Handler.ServeHTTP(w, r)
}
