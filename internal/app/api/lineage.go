package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/lineage"
)

const LineageTimelinePath = "/api/v1/lineage/timeline"

// LineageScopeResolver supplies an already-authenticated tenant scope. The API
// deliberately does not accept organization/workspace ids from query/header text.
type LineageScopeResolver interface {
	LineageScope(*http.Request) (lineage.Scope, error)
}

// newHandlerWithLineage mounts the read-only lineage timeline in addition to
// the base API. Production composition must supply an authenticated resolver.
func newHandlerWithLineage(logger *slog.Logger, reader lineage.Reader, resolver LineageScopeResolver) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == LineageTimelinePath {
			lineageTimeline(w, r, reader, resolver)
			return
		}
		route(w, r)
	}))
}

func newLineageRoutes(reader lineage.Reader) []ProtectedRoute {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lineageTimeline(w, r, reader, productionScopeResolver{})
	})
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: LineageTimelinePath, Permission: "lineage.read", Handler: handler},
		{Method: http.MethodHead, Path: LineageTimelinePath, Permission: "lineage.read", Handler: handler},
	}
}

func lineageTimeline(w http.ResponseWriter, r *http.Request, reader lineage.Reader, resolver LineageScopeResolver) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if reader == nil || resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, err := resolver.LineageScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	qv := r.URL.Query()
	limit := 50
	if raw := qv.Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = v
	}
	query := lineage.TimelineQuery{System: qv.Get("system"), EntityType: qv.Get("entity_type"), EntityID: qv.Get("entity_id"), Field: qv.Get("field"), Limit: limit}
	if raw := qv.Get("before_at"); raw != "" {
		parsed, e := time.Parse(time.RFC3339Nano, raw)
		if e != nil || parsed.Location() != time.UTC {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		parsed = parsed.UTC()
		query.BeforeAt = &parsed
		query.BeforeID = qv.Get("before_id")
	}
	if err := query.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	page, err := reader.Timeline(r.Context(), scope, query)
	if err != nil {
		if errors.Is(err, lineage.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_ = jsonEncode(w, page)
}

func jsonEncode(w http.ResponseWriter, v any) error {
	return json.NewEncoder(w).Encode(v)
}
