package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

const AuditPath = "/api/v1/audit"

type auditReader interface {
	List(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error)
}

type auditItem struct {
	ID            string        `json:"id"`
	ActorID       string        `json:"actor_id"`
	Source        string        `json:"source"`
	Action        string        `json:"action"`
	ResourceType  string        `json:"resource_type"`
	ResourceID    string        `json:"resource_id"`
	CorrelationID string        `json:"correlation_id"`
	Risk          audit.Risk    `json:"risk"`
	Summary       audit.Summary `json:"summary"`
	CreatedAt     time.Time     `json:"created_at"`
}

func newAuditRoutes(repository auditReader) []ProtectedRoute {
	return []ProtectedRoute{{Method: http.MethodGet, Path: AuditPath, Permission: "audit.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		if !ok || repository == nil {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			limit = parsed
		}
		cursor := r.URL.Query().Get("cursor")
		if len(cursor) > 128 || cursor != strings.TrimSpace(cursor) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		items, next, err := repository.List(r.Context(), scope, limit, cursor)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		views := make([]auditItem, 0, len(items))
		for _, item := range items {
			views = append(views, auditItem{ID: item.ID, ActorID: item.ActorID, Source: item.Source, Action: item.Action, ResourceType: item.ResourceType, ResourceID: item.ResourceID, CorrelationID: item.CorrelationID, Risk: item.Risk, Summary: item.Summary, CreatedAt: item.CreatedAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
	})}}
}
