package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	ConnectorReadinessPath       = "/api/v1/connector-readiness"
	ConnectorReadinessDetailPath = ConnectorReadinessPath + "/"
)

// connectorReadinessResponse is the stable, catalog-wide readiness view. It
// is intentionally independent of configured tenant accounts: an operator
// must see unsupported and health-only entries before attempting setup.
type connectorReadinessResponse struct {
	SchemaVersion int                    `json:"schema_version"`
	GeneratedAt   string                 `json:"generated_at"`
	Consistency   string                 `json:"consistency"`
	Summary       sdk.ReadinessSummary   `json:"summary"`
	Items         []sdk.ReadinessProfile `json:"items"`
	NextCursor    string                 `json:"next_cursor,omitempty"`
}

func newConnectorReadinessRoutes() []ProtectedRoute {
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ConnectorReadinessPath, Permission: "integrations.center.read", Handler: http.HandlerFunc(connectorReadinessList)},
		{Method: http.MethodGet, Path: ConnectorReadinessDetailPath, PathPrefix: true, Permission: "integrations.center.read", Handler: http.HandlerFunc(connectorReadinessDetail)},
	}
}

func connectorReadinessList(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	snapshot, err := sdk.ReadinessSnapshot()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	query := r.URL.Query()
	limit := 50
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
	}
	after, err := decodeAccountCursor(query.Get("cursor"))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	filters, ok := readinessFiltersFromQuery(query)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items := make([]sdk.ReadinessProfile, 0, limit)
	started := after == ""
	for _, profile := range snapshot.Profiles {
		if !started {
			if profile.ConnectorID == after {
				started = true
			}
			continue
		}
		if !readinessProfileMatches(profile, filters) {
			continue
		}
		items = append(items, profile)
		if len(items) == limit+1 {
			break
		}
	}
	if after != "" && !started {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	next := ""
	if len(items) > limit {
		next = encodeAccountCursor(items[limit-1].ConnectorID)
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, connectorReadinessResponse{SchemaVersion: snapshot.SchemaVersion, GeneratedAt: snapshot.GeneratedAt.UTC().Format(time.RFC3339Nano), Consistency: snapshot.Consistency, Summary: snapshot.Summary, Items: items, NextCursor: next})
}

func connectorReadinessDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, ConnectorReadinessDetailPath)
	if id == "" || strings.ContainsAny(id, "/\r\n\t") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	snapshot, err := sdk.ReadinessSnapshot()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	for _, profile := range snapshot.Profiles {
		if profile.ConnectorID == id {
			writeJSON(w, http.StatusOK, profile)
			return
		}
	}
	writeProblem(w, http.StatusNotFound, "Not Found")
}

type readinessFilters struct {
	family   string
	surface  string
	status   sdk.ReadinessStatus
	priority string
}

func readinessFiltersFromQuery(query mapQuery) (readinessFilters, bool) {
	filters := readinessFilters{family: query.Get("family"), surface: query.Get("surface"), priority: query.Get("priority")}
	for _, value := range []string{filters.family, filters.surface, filters.priority} {
		if len(value) > 64 || strings.ContainsAny(value, "\r\n") {
			return readinessFilters{}, false
		}
	}
	if raw := query.Get("status"); raw != "" {
		filters.status = sdk.ReadinessStatus(raw)
		switch filters.status {
		case sdk.ReadinessManifestOnly, sdk.ReadinessHealthOnly, sdk.ReadinessReadOnly, sdk.ReadinessPartiallySupported, sdk.ReadinessReady, sdk.ReadinessQualified, sdk.ReadinessDegraded, sdk.ReadinessReauthorizationNeeded, sdk.ReadinessNotAvailable:
		default:
			return readinessFilters{}, false
		}
	}
	return filters, true
}

// mapQuery keeps the parser testable without coupling it to an HTTP request.
type mapQuery interface {
	Get(string) string
}

func readinessProfileMatches(profile sdk.ReadinessProfile, filters readinessFilters) bool {
	return (filters.family == "" || profile.Family == filters.family) &&
		(filters.surface == "" || profile.Surface == filters.surface) &&
		(filters.status == "" || profile.Status == filters.status) &&
		(filters.priority == "" || profile.Priority == filters.priority)
}
