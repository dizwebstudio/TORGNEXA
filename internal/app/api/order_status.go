package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
)

// OrderStatusPath is the protected prefix for order lifecycle mutations.
const OrderStatusPath = "/api/v1/orders/"

type orderStatusRepository interface {
	ChangeStatus(context.Context, orders.Scope, orders.ChangeStatus, orders.Mutation) (orders.Order, error)
}

type orderStatusAPI struct{ repository orderStatusRepository }

type orderStatusInput struct {
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"version"`
}

func newOrderStatusRoutes(repository orderStatusRepository) []ProtectedRoute {
	return []ProtectedRoute{{
		Method:     http.MethodPatch,
		Path:       OrderStatusPath,
		PathPrefix: true,
		Permission: "orders.status.write",
		Handler:    http.HandlerFunc(orderStatusAPI{repository: repository}.changeStatus),
	}}
}

func (a orderStatusAPI) changeStatus(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, OrderStatusPath), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "status" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	orderID, err := orders.ParseOrderID(parts[0])
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input orderStatusInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	status := orders.Status(strings.TrimSpace(input.Status))
	if !status.Valid() || input.ExpectedVersion < 1 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	orderScope, err := orders.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	result, err := a.repository.ChangeStatus(r.Context(), orderScope, orders.ChangeStatus{ID: orderID, ExpectedVersion: input.ExpectedVersion, Status: status}, orderStatusMutation(principal, r))
	if err != nil {
		switch {
		case errors.Is(err, orders.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "Not Found")
		case errors.Is(err, orders.ErrConflict), errors.Is(err, orders.ErrInvalidState):
			writeProblem(w, http.StatusConflict, "Conflict")
		case errors.Is(err, orders.ErrInvalidRecord):
			writeProblem(w, http.StatusBadRequest, "Bad Request")
		default:
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": result.ID.String(), "status": string(result.Status), "version": result.Version, "updated_at": result.UpdatedAt.UTC()})
}

func orderStatusMutation(principal Principal, r *http.Request) orders.Mutation {
	id := newApprovalID()
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if correlation == "" {
		correlation = id
	}
	return orders.Mutation{EventID: id, AuditID: newApprovalID(), ActorID: principal.Subject, Source: "api", CorrelationID: correlation, OccurredAt: time.Now().UTC()}
}
