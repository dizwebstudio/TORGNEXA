package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

type orderStatusRepositoryStub struct {
	result   orders.Order
	err      error
	scope    orders.Scope
	command  orders.ChangeStatus
	mutation orders.Mutation
}

func (s *orderStatusRepositoryStub) ChangeStatus(_ context.Context, scope orders.Scope, command orders.ChangeStatus, mutation orders.Mutation) (orders.Order, error) {
	s.scope, s.command, s.mutation = scope, command, mutation
	return s.result, s.err
}

func TestOrderStatusRouteChangesStatusWithOptimisticVersion(t *testing.T) {
	orderID := "018f0000-0000-7000-8000-000000000101"
	scope, err := tenancy.ParseScope(searchOrgID, searchWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	result := orders.Order{ID: orders.OrderID(orderID), Status: orders.StatusConfirmed, Version: 2, UpdatedAt: time.Now().UTC()}
	repository := &orderStatusRepositoryStub{result: result}
	route := newOrderStatusRoutes(repository)[0]
	request := httptest.NewRequest(http.MethodPatch, OrderStatusPath+orderID+"/status", strings.NewReader(`{"status":"confirmed","version":1}`))
	request.Header.Set("Idempotency-Key", "order-status-test")
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	request = request.WithContext(context.WithValue(request.Context(), requestIdentityKey{}, Principal{Issuer: "https://id.example", Subject: "operator-1"}))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"confirmed"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.command.ID.String() != orderID || repository.command.ExpectedVersion != 1 || repository.command.Status != orders.StatusConfirmed {
		t.Fatalf("unexpected command: %#v", repository.command)
	}
	if repository.scope.OrganizationID() != searchOrgID || repository.scope.WorkspaceID() != searchWorkspaceID || repository.mutation.CorrelationID != "order-status-test" {
		t.Fatalf("unexpected mutation scope: %#v %#v", repository.scope, repository.mutation)
	}
}

func TestOrderStatusRouteMapsLifecycleConflict(t *testing.T) {
	orderID := "018f0000-0000-7000-8000-000000000101"
	scope, err := tenancy.ParseScope(searchOrgID, searchWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	repository := &orderStatusRepositoryStub{err: orders.ErrInvalidState}
	request := httptest.NewRequest(http.MethodPatch, OrderStatusPath+orderID+"/status", strings.NewReader(`{"status":"fulfilled","version":1}`))
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	request = request.WithContext(context.WithValue(request.Context(), requestIdentityKey{}, Principal{Issuer: "https://id.example", Subject: "operator-1"}))
	response := httptest.NewRecorder()
	newOrderStatusRoutes(repository)[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("unexpected status=%d body=%s", response.Code, response.Body.String())
	}
}
