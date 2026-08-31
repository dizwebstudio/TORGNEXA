package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/postgres/advertisingrepo"
)

const (
	AdvertisingCampaignsPath   = "/api/v1/advertising/campaigns"
	AdvertisingSpendPath       = "/api/v1/advertising/spend"
	AdvertisingPerformancePath = "/api/v1/advertising/performance"
	AdvertisingMetricsPath     = "/api/v1/advertising/metrics"
	AdvertisingFindingsPath    = "/api/v1/advertising/reconciliation"
	AdvertisingSyncRunsPath    = "/api/v1/advertising/sync-runs"
)

type advertisingAPI struct{ repository *advertisingrepo.Repository }

// newAdvertisingRoutes exposes the read-only MVP. Management routes are not
// registered until a provider passes the second-stage write qualification.
func newAdvertisingRoutes(repository *advertisingrepo.Repository) []ProtectedRoute {
	a := advertisingAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: AdvertisingCampaignsPath, Permission: "ads.read", Handler: http.HandlerFunc(a.campaigns)},
		{Method: http.MethodGet, Path: AdvertisingSpendPath, Permission: "ads.read", Handler: http.HandlerFunc(a.spend)},
		{Method: http.MethodGet, Path: AdvertisingPerformancePath, Permission: "ads.read", Handler: http.HandlerFunc(a.performance)},
		{Method: http.MethodGet, Path: AdvertisingMetricsPath, Permission: "ads.read", Handler: http.HandlerFunc(a.metrics)},
		{Method: http.MethodGet, Path: AdvertisingFindingsPath, Permission: "ads.read", Handler: http.HandlerFunc(a.findings)},
		{Method: http.MethodGet, Path: AdvertisingSyncRunsPath, Permission: "ads.read", Handler: http.HandlerFunc(a.syncRuns)},
	}
}

func (a advertisingAPI) campaigns(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	filter, err := parseAdvertisingFilter(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid advertising filters")
		return
	}
	items, err := a.repository.ListCampaigns(r.Context(), scope, filter)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Advertising campaigns unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a advertisingAPI) spend(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	filter, err := parseAdvertisingFilter(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid advertising filters")
		return
	}
	items, err := a.repository.ListSpend(r.Context(), scope, filter)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Advertising spend unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a advertisingAPI) performance(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	filter, err := parseAdvertisingFilter(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid advertising filters")
		return
	}
	items, err := a.repository.ListPerformance(r.Context(), scope, filter)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Advertising performance unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a advertisingAPI) metrics(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	filter, err := parseAdvertisingFilter(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid advertising filters")
		return
	}
	items, err := a.repository.ListMetrics(r.Context(), scope, filter)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Advertising metrics unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "generated_at": time.Now().UTC(), "source": "postgresql"})
}
func (a advertisingAPI) findings(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeProblem(w, http.StatusBadRequest, "Invalid limit")
			return
		}
	}
	items, err := a.repository.ListFindings(r.Context(), scope, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Advertising reconciliation unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a advertisingAPI) syncRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeProblem(w, http.StatusBadRequest, "Invalid limit")
			return
		}
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if len(accountID) > 192 {
		writeProblem(w, http.StatusBadRequest, "Invalid account_id")
		return
	}
	items, err := a.repository.ListSyncRuns(r.Context(), scope, accountID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Advertising sync runs unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseAdvertisingFilter(r *http.Request) (advertisingrepo.Filter, error) {
	q := r.URL.Query()
	filter := advertisingrepo.Filter{Channel: strings.ToLower(strings.TrimSpace(q.Get("channel"))), CampaignID: strings.TrimSpace(q.Get("campaign_id")), SKU: strings.TrimSpace(q.Get("sku")), Limit: 100}
	if len(filter.Channel) > 64 || len(filter.CampaignID) > 192 || len(filter.SKU) > 200 {
		return filter, errors.New("filter too long")
	}
	if raw := q.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return filter, err
		}
		filter.Limit = value
	}
	var err error
	if raw := q.Get("from"); raw != "" {
		filter.From, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, err
		}
		filter.From = filter.From.UTC()
	}
	if raw := q.Get("to"); raw != "" {
		filter.To, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, err
		}
		filter.To = filter.To.UTC()
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && !filter.To.After(filter.From) {
		return filter, errors.New("invalid range")
	}
	return filter, nil
}
