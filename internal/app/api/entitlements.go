package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
)

const EntitlementEvaluatePath = "/api/v1/entitlements/evaluate"

type EntitlementScopeResolver interface {
	EntitlementScope(*http.Request) (entitlements.Scope, error)
}
type EntitlementEvaluator interface {
	Evaluate(context.Context, entitlements.Scope, entitlements.FeatureKey, time.Time) (entitlements.Evaluation, error)
}
type EntitlementQuotaReader interface {
	Status(context.Context, entitlements.Scope, entitlements.MetricKey, time.Time) (entitlements.QuotaStatus, error)
}
type EntitlementClock interface{ Now() time.Time }
type systemEntitlementClock struct{}

func (systemEntitlementClock) Now() time.Time { return time.Now().UTC() }

type entitlementEvaluateRequest struct {
	Feature string `json:"feature"`
	Metric  string `json:"metric,omitempty"`
}
type entitlementEvaluateResponse struct {
	Evaluation entitlements.Evaluation   `json:"evaluation"`
	Quota      *entitlements.QuotaStatus `json:"quota,omitempty"`
}

func newHandlerWithEntitlements(logger *slog.Logger, e EntitlementEvaluator, q EntitlementQuotaReader, r EntitlementScopeResolver) http.Handler {
	return newHandlerWithEntitlementsClock(logger, e, q, r, systemEntitlementClock{})
}

func newEntitlementRoutes(e EntitlementEvaluator, q EntitlementQuotaReader) []ProtectedRoute {
	return []ProtectedRoute{{Method: http.MethodPost, Path: EntitlementEvaluatePath, Permission: "entitlements.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entitlementEvaluate(w, r, e, q, productionScopeResolver{}, systemEntitlementClock{})
	})}}
}
func newHandlerWithEntitlementsClock(logger *slog.Logger, e EntitlementEvaluator, q EntitlementQuotaReader, r EntitlementScopeResolver, clock EntitlementClock) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == EntitlementEvaluatePath {
			entitlementEvaluate(w, req, e, q, r, clock)
			return
		}
		route(w, req)
	}))
}
func entitlementEvaluate(w http.ResponseWriter, r *http.Request, e EntitlementEvaluator, q EntitlementQuotaReader, resolver EntitlementScopeResolver, clock EntitlementClock) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if e == nil || resolver == nil || clock == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, err := resolver.EntitlementScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	var in entitlementEvaluateRequest
	if err = dec.Decode(&in); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	feature, err := entitlements.ParseFeatureKey(in.Feature)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	at := clock.Now()
	if at.IsZero() {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	at = at.UTC()
	eval, err := e.Evaluate(r.Context(), scope, feature, at)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	out := entitlementEvaluateResponse{Evaluation: eval}
	if in.Metric != "" {
		if q == nil {
			writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
			return
		}
		metric, parseErr := entitlements.ParseMetricKey(in.Metric)
		if parseErr != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		status, statusErr := q.Status(r.Context(), scope, metric, at)
		if statusErr != nil {
			if errors.Is(statusErr, entitlements.ErrNotFound) {
				writeProblem(w, http.StatusNotFound, "Not Found")
				return
			}
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		out.Quota = &status
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_ = jsonEncode(w, out)
}
