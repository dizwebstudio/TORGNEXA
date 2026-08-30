package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marking"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/markingrepo"
)

const (
	MarkingOverviewPath = "/api/v1/marking/overview"
	MarkingScansPath    = "/api/v1/marking/scans"
)

type markingAPI struct{ repository *markingrepo.Repository }

type markingScanInput struct {
	Barcode          string `json:"barcode"`
	GTIN             string `json:"gtin"`
	SKU              string `json:"sku"`
	WMSAction        string `json:"wms_action"`
	ExpectedQuantity int64  `json:"expected_quantity"`
}

func newMarkingRoutes(repository *markingrepo.Repository) []ProtectedRoute {
	api := markingAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: MarkingOverviewPath, Permission: "stock.read", Handler: http.HandlerFunc(api.overview)},
		{Method: http.MethodPost, Path: MarkingScansPath, Permission: "stock.write", Handler: http.HandlerFunc(api.scan)},
	}
}

type markingRouteScope interface {
	Overview(context.Context, tenancy.Scope, int) (markingrepo.Overview, error)
	RecordScan(context.Context, tenancy.Scope, marking.Scan, int64) (marking.Scan, error)
}

func (api markingAPI) overview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = parsed
	}
	result, err := api.repository.Overview(r.Context(), scope, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api markingAPI) scan(w http.ResponseWriter, r *http.Request) {
	scope, scoped := ScopeFromContext(r.Context())
	principal, identified := PrincipalFromContext(r.Context())
	if !scoped || !identified || api.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input markingScanInput
	if decodeStrictJSON(r, &input) != nil || input.ExpectedQuantity < 1 || input.ExpectedQuantity > 1000000000 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	fingerprint, err := marking.CodeFingerprint(input.Barcode)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid barcode")
		return
	}
	now := time.Now().UTC()
	scan := marking.Scan{ID: stableID("scan_", 40, scope, strings.TrimSpace(r.Header.Get("Idempotency-Key"))), Fingerprint: fingerprint, SKU: input.SKU, GTIN: input.GTIN, WMSAction: input.WMSAction, Result: marking.ScanAccepted, ActorID: principal.Subject, OccurredAt: now}
	result, err := api.repository.RecordScan(r.Context(), scope, scan, input.ExpectedQuantity)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Scan could not be recorded")
		return
	}
	status := http.StatusOK
	if result.Result == marking.ScanAccepted {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}
