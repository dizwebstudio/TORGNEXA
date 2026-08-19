package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

type auditReaderStub struct{ scope tenancy.Scope }

func (stub *auditReaderStub) List(_ context.Context, scope tenancy.Scope, limit int, cursor string) ([]audit.Record, string, error) {
	stub.scope = scope
	return []audit.Record{{ID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", ActorID: "user-1", Source: "api", Action: "settings.workspace.updated", ResourceType: "workspace", ResourceID: "workspace-1", CorrelationID: "request-1", Risk: audit.RiskWriteSafe, Summary: audit.Summary{}, CreatedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}}, "", nil
}

func TestAuditRouteUsesAuthorizedScope(t *testing.T) {
	scope := validTestScope(t)
	stub := &auditReaderStub{}
	route := newAuditRoutes(stub)[0]
	request := httptest.NewRequest(http.MethodGet, AuditPath+"?limit=25", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || stub.scope != scope {
		t.Fatalf("status=%d scope=%v body=%s", response.Code, stub.scope, response.Body.String())
	}
}
