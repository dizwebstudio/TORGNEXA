package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	core "github.com/torgnexa/torgnexa/internal/core/returns"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	orderCancellationsPath = "/api/v1/order-cancellations"
	returnsPath            = "/api/v1/returns"
	returnLogisticsPath    = "/api/v1/return-logistics/"
	refundAllocationsPath  = "/api/v1/refund-allocations"
)

type returnsAPIRepository interface{ core.Repository }

func newReturnsRoutes(repository returnsAPIRepository) []ProtectedRoute {
	api := returnsAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: orderCancellationsPath + "/", PathPrefix: true, Permission: "orders.returns.read", Handler: http.HandlerFunc(api.cancellationRoute)},
		{Method: http.MethodPatch, Path: orderCancellationsPath + "/", PathPrefix: true, Permission: "orders.returns.write", Handler: http.HandlerFunc(api.cancellationRoute)},
		{Method: http.MethodPost, Path: orderCancellationsPath, Permission: "orders.returns.write", Handler: http.HandlerFunc(api.createCancellation)},
		{Method: http.MethodGet, Path: returnsPath, Permission: "orders.returns.read", Handler: http.HandlerFunc(api.listReturns)},
		{Method: http.MethodPost, Path: returnsPath, Permission: "orders.returns.write", Handler: http.HandlerFunc(api.createReturn)},
		{Method: http.MethodGet, Path: returnsPath + "/", PathPrefix: true, Permission: "orders.returns.read", Handler: http.HandlerFunc(api.returnRoute)},
		{Method: http.MethodPatch, Path: returnsPath + "/", PathPrefix: true, Permission: "orders.returns.write", Handler: http.HandlerFunc(api.returnRoute)},
		{Method: http.MethodPost, Path: returnsPath + "/", PathPrefix: true, Permission: "orders.returns.write", Handler: http.HandlerFunc(api.returnRoute)},
		{Method: http.MethodGet, Path: returnLogisticsPath, PathPrefix: true, Permission: "orders.returns.read", Handler: http.HandlerFunc(api.returnLogisticsRoute)},
		{Method: http.MethodPost, Path: refundAllocationsPath, Permission: "payments.refunds.write", Handler: http.HandlerFunc(api.createRefundAllocation)},
	}
}

type returnsAPI struct{ repository returnsAPIRepository }

type cancellationView struct {
	ID             string    `json:"id"`
	OrderID        string    `json:"order_id"`
	Status         string    `json:"status"`
	ReasonCode     string    `json:"reason_code"`
	Source         string    `json:"source"`
	IdempotencyKey string    `json:"idempotency_key"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type returnView struct {
	ID                     string    `json:"id"`
	OrderID                string    `json:"order_id"`
	Status                 string    `json:"status"`
	ReasonCode             string    `json:"reason_code"`
	Source                 string    `json:"source"`
	IdempotencyKey         string    `json:"idempotency_key"`
	Currency               string    `json:"currency"`
	RequestedShippingMinor int64     `json:"requested_shipping_minor"`
	RequestedTaxMinor      int64     `json:"requested_tax_minor"`
	Version                int64     `json:"version"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type returnItemView struct {
	ID          string       `json:"id"`
	ReturnID    string       `json:"return_id"`
	OrderItemID string       `json:"order_item_id"`
	Unit        string       `json:"unit"`
	Disposition string       `json:"disposition"`
	Requested   quantityView `json:"requested"`
	Received    quantityView `json:"received"`
	Accepted    quantityView `json:"accepted"`
	Version     int64        `json:"version"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type returnLogisticsView struct {
	ID                 string    `json:"id"`
	ReturnID           string    `json:"return_id"`
	ConnectorAccountID string    `json:"connector_account_id"`
	OriginalRemoteID   string    `json:"original_remote_id"`
	ExternalID         string    `json:"external_id"`
	MailType           string    `json:"mail_type"`
	TariffCode         int       `json:"tariff_code,omitempty"`
	Status             string    `json:"status"`
	RemoteID           string    `json:"remote_id,omitempty"`
	TrackingNumber     string    `json:"tracking_number,omitempty"`
	FailureCode        string    `json:"failure_code,omitempty"`
	IdempotencyKey     string    `json:"idempotency_key"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type quantityView struct {
	Coefficient int64  `json:"coefficient"`
	Scale       uint8  `json:"scale"`
	Unit        string `json:"unit"`
}

type refundAllocationView struct {
	ID             string    `json:"id"`
	PaymentID      string    `json:"payment_id"`
	RefundID       string    `json:"refund_id"`
	ReturnID       string    `json:"return_id"`
	OrderItemID    string    `json:"order_item_id,omitempty"`
	Component      string    `json:"component"`
	IdempotencyKey string    `json:"idempotency_key"`
	Amount         moneyView `json:"amount"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
}

func (a returnsAPI) createCancellation(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID         string `json:"id"`
		OrderID    string `json:"order_id"`
		ReasonCode string `json:"reason_code"`
	}
	if !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || input.ID == "" || input.OrderID == "" || input.ReasonCode == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id, err := core.ParseCancellationID(input.ID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if _, err := orders.ParseOrderID(input.OrderID); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	now := time.Now().UTC()
	command := core.CancellationRequest{ID: id, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), OrderID: input.OrderID, Status: core.CancellationRequested, ReasonCode: strings.TrimSpace(input.ReasonCode), Source: "api.returns", IdempotencyKey: key, Version: 1, CreatedAt: now, UpdatedAt: now}
	result, err := a.repository.CreateCancellation(r.Context(), scopeToReturns(scope), command, returnsMutation(principal.Subject, key))
	if err != nil {
		writeReturnsError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, cancellationResponse(result))
}

func (a returnsAPI) cancellationRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, orderCancellationsPath), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	id, err := core.ParseCancellationID(parts[0])
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, principal, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	if r.Method == http.MethodGet && len(parts) == 1 {
		result, getErr := a.repository.Cancellation(r.Context(), scopeToReturns(scope), id)
		if getErr != nil {
			writeReturnsError(w, getErr)
			return
		}
		writeJSON(w, http.StatusOK, cancellationResponse(result))
		return
	}
	if r.Method == http.MethodPatch && len(parts) == 2 && parts[1] == "status" {
		var input struct {
			Status  core.CancellationStatus `json:"status"`
			Version int64                   `json:"version"`
		}
		if !validIdempotencyKey(r.Header.Get("Idempotency-Key")) || decodeStrictJSON(r, &input) != nil || !input.Status.Valid() || input.Version < 1 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		result, changeErr := a.repository.ChangeCancellationStatus(r.Context(), scopeToReturns(scope), id, input.Status, input.Version, returnsMutation(principal.Subject, r.Header.Get("Idempotency-Key")))
		if changeErr != nil {
			writeReturnsError(w, changeErr)
			return
		}
		writeJSON(w, http.StatusOK, cancellationResponse(result))
		return
	}
	writeProblem(w, http.StatusNotFound, "Not Found")
}

func (a returnsAPI) createReturn(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID            string `json:"id"`
		OrderID       string `json:"order_id"`
		ReasonCode    string `json:"reason_code"`
		Currency      string `json:"currency"`
		ShippingMinor int64  `json:"shipping_minor"`
		TaxMinor      int64  `json:"tax_minor"`
	}
	if !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || input.ID == "" || input.OrderID == "" || input.ReasonCode == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id, err := core.ParseReturnID(input.ID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if _, err := orders.ParseOrderID(input.OrderID); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	currency, err := domain.NewCurrency(strings.TrimSpace(input.Currency))
	if err != nil || input.ShippingMinor < 0 || input.TaxMinor < 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	now := time.Now().UTC()
	command := core.ReturnRequest{ID: id, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), OrderID: input.OrderID, Status: core.ReturnRequested, ReasonCode: strings.TrimSpace(input.ReasonCode), Source: "api.returns", Currency: currency, RequestedShippingMinor: input.ShippingMinor, RequestedTaxMinor: input.TaxMinor, IdempotencyKey: key, Version: 1, CreatedAt: now, UpdatedAt: now}
	result, err := a.repository.CreateReturn(r.Context(), scopeToReturns(scope), command, returnsMutation(principal.Subject, key))
	if err != nil {
		writeReturnsError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, returnResponse(result))
}

func (a returnsAPI) listReturns(w http.ResponseWriter, r *http.Request) {
	scope, _, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
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
	items, err := a.repository.ListReturns(r.Context(), scopeToReturns(scope), limit)
	if err != nil {
		writeReturnsError(w, err)
		return
	}
	views := make([]returnView, 0, len(items))
	for _, item := range items {
		views = append(views, returnResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (a returnsAPI) returnRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, returnsPath), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	id, err := core.ParseReturnID(parts[0])
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, principal, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	if r.Method == http.MethodGet && len(parts) == 1 {
		result, getErr := a.repository.Return(r.Context(), scopeToReturns(scope), id)
		if getErr != nil {
			writeReturnsError(w, getErr)
			return
		}
		items, itemErr := a.repository.ReturnItems(r.Context(), scopeToReturns(scope), id, 200)
		if itemErr != nil {
			writeReturnsError(w, itemErr)
			return
		}
		views := make([]returnItemView, 0, len(items))
		for _, item := range items {
			views = append(views, returnItemResponse(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"return": returnResponse(result), "items": views})
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "logistics" {
		var input struct {
			ID                 string `json:"id"`
			ConnectorAccountID string `json:"connector_account_id"`
			OriginalRemoteID   string `json:"original_remote_id"`
			ExternalID         string `json:"external_id"`
			MailType           string `json:"mail_type"`
			TariffCode         int    `json:"tariff_code"`
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || input.ID == "" || input.ConnectorAccountID == "" || input.OriginalRemoteID == "" || input.ExternalID == "" || input.MailType == "" || input.TariffCode < 0 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		operationID, idErr := core.ParseReturnLogisticsOperationID(input.ID)
		if idErr != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		result, operationErr := a.repository.CreateReturnLogistics(r.Context(), scopeToReturns(scope), core.ReturnLogisticsCommand{ID: operationID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ReturnID: id, ConnectorAccountID: strings.TrimSpace(input.ConnectorAccountID), OriginalRemoteID: strings.TrimSpace(input.OriginalRemoteID), ExternalID: strings.TrimSpace(input.ExternalID), MailType: strings.TrimSpace(input.MailType), TariffCode: input.TariffCode, IdempotencyKey: key}, returnsMutation(principal.Subject, key))
		if operationErr != nil {
			writeReturnsError(w, operationErr)
			return
		}
		writeJSON(w, http.StatusAccepted, returnLogisticsResponse(result))
		return
	}
	if r.Method == http.MethodPatch && len(parts) == 2 && parts[1] == "status" {
		var input struct {
			Status  core.ReturnStatus `json:"status"`
			Version int64             `json:"version"`
		}
		if !validIdempotencyKey(r.Header.Get("Idempotency-Key")) || decodeStrictJSON(r, &input) != nil || !input.Status.Valid() || input.Version < 1 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		result, changeErr := a.repository.ChangeReturnStatus(r.Context(), scopeToReturns(scope), id, input.Status, input.Version, returnsMutation(principal.Subject, r.Header.Get("Idempotency-Key")))
		if changeErr != nil {
			writeReturnsError(w, changeErr)
			return
		}
		writeJSON(w, http.StatusOK, returnResponse(result))
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "inspection" {
		var input struct {
			ID              string `json:"id"`
			ReturnItemID    string `json:"return_item_id"`
			Outcome         string `json:"outcome"`
			ConditionCode   string `json:"condition_code"`
			DiscrepancyCode string `json:"discrepancy_code"`
			Unit            string `json:"unit"`
			Disposition     string `json:"disposition"`
			ArtifactRef     string `json:"artifact_ref"`
			Coefficient     int64  `json:"coefficient"`
			Scale           uint8  `json:"scale"`
		}
		if !validIdempotencyKey(r.Header.Get("Idempotency-Key")) || decodeStrictJSON(r, &input) != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		inspectionID, idErr := core.ParseEvidenceID(input.ID)
		itemID, itemErr := core.ParseReturnItemID(input.ReturnItemID)
		quantity, qtyErr := core.NewQuantity(input.Coefficient, input.Scale, strings.TrimSpace(input.Unit))
		if idErr != nil || itemErr != nil || qtyErr != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		inspection := core.InspectionResult{ID: inspectionID, ReturnID: id, ReturnItemID: itemID, Outcome: core.ReturnStatus(input.Outcome), ConditionCode: input.ConditionCode, DiscrepancyCode: input.DiscrepancyCode, Quantity: quantity, Disposition: core.Disposition(input.Disposition), ArtifactRef: input.ArtifactRef, OccurredAt: time.Now().UTC()}
		if err := a.repository.RecordInspection(r.Context(), scopeToReturns(scope), inspection, returnsMutation(principal.Subject, r.Header.Get("Idempotency-Key"))); err != nil {
			writeReturnsError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"id": inspection.ID.String(), "return_id": id.String(), "status": "recorded"})
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "items" {
		var input struct {
			ID             string `json:"id"`
			OrderItemID    string `json:"order_item_id"`
			Unit           string `json:"unit"`
			RequestedCoeff int64  `json:"requested_coefficient"`
			RequestedScale uint8  `json:"requested_scale"`
			Disposition    string `json:"disposition"`
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || input.ID == "" || input.OrderItemID == "" || input.RequestedCoeff <= 0 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		itemID, idErr := core.ParseReturnItemID(input.ID)
		requested, qtyErr := core.NewQuantity(input.RequestedCoeff, input.RequestedScale, strings.TrimSpace(input.Unit))
		zero, zeroErr := core.NewQuantity(0, 0, requested.Unit)
		if idErr != nil || qtyErr != nil || zeroErr != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		item := core.ReturnItem{ID: itemID, ReturnID: id, OrderItemID: strings.TrimSpace(input.OrderItemID), Requested: requested, Received: zero, Accepted: zero, Disposition: core.Disposition(input.Disposition), Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		result, itemErr := a.repository.CreateReturnItem(r.Context(), scopeToReturns(scope), item, returnsMutation(principal.Subject, key))
		if itemErr != nil {
			writeReturnsError(w, itemErr)
			return
		}
		writeJSON(w, http.StatusAccepted, returnItemResponse(result))
		return
	}
	writeProblem(w, http.StatusNotFound, "Not Found")
}

func (a returnsAPI) returnLogisticsRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, returnLogisticsPath), "/"), "/")
	if r.Method != http.MethodGet || len(parts) != 1 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	id, err := core.ParseReturnLogisticsOperationID(parts[0])
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, _, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	result, err := a.repository.ReturnLogistics(r.Context(), scopeToReturns(scope), id)
	if err != nil {
		writeReturnsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, returnLogisticsResponse(result))
}

func (a returnsAPI) createRefundAllocation(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := returnsContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID          string `json:"id"`
		PaymentID   string `json:"payment_id"`
		RefundID    string `json:"refund_id"`
		ReturnID    string `json:"return_id"`
		OrderItemID string `json:"order_item_id"`
		Component   string `json:"component"`
		Currency    string `json:"currency"`
		AmountMinor int64  `json:"amount_minor"`
	}
	if !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || input.ID == "" || input.PaymentID == "" || input.RefundID == "" || input.ReturnID == "" || input.Component == "" || input.AmountMinor <= 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id, err := core.ParseRefundAllocationID(input.ID)
	returnID, returnErr := core.ParseReturnID(input.ReturnID)
	currency, currencyErr := domain.NewCurrency(strings.TrimSpace(input.Currency))
	if err != nil || returnErr != nil || currencyErr != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	amount, err := domain.NewMoney(input.AmountMinor, currency)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	allocation := core.RefundAllocation{ID: id, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), PaymentID: input.PaymentID, RefundID: input.RefundID, ReturnID: returnID, OrderItemID: input.OrderItemID, Component: core.RefundComponent(input.Component), Amount: amount, Currency: currency, IdempotencyKey: key, Version: 1, CreatedAt: time.Now().UTC()}
	result, err := a.repository.CreateRefundAllocation(r.Context(), scopeToReturns(scope), allocation, returnsMutation(principal.Subject, key))
	if err != nil {
		writeReturnsError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, refundAllocationResponse(result))
}

func returnsContext(w http.ResponseWriter, r *http.Request) (tenancy.Scope, Principal, bool) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, Principal{}, false
	}
	return scope, principal, true
}

func scopeToReturns(scope tenancy.Scope) core.Scope {
	result, _ := core.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	return result
}

func returnsMutation(actor, correlation string) core.Mutation {
	return core.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: boundedActorRef(actor), Source: "api.returns", CorrelationID: boundedActorRef(correlation), OccurredAt: time.Now().UTC()}
}

func cancellationResponse(value core.CancellationRequest) cancellationView {
	return cancellationView{ID: value.ID.String(), OrderID: value.OrderID, Status: string(value.Status), ReasonCode: value.ReasonCode, Source: value.Source, IdempotencyKey: value.IdempotencyKey, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}
func returnResponse(value core.ReturnRequest) returnView {
	return returnView{ID: value.ID.String(), OrderID: value.OrderID, Status: string(value.Status), ReasonCode: value.ReasonCode, Source: value.Source, IdempotencyKey: value.IdempotencyKey, Currency: value.Currency.String(), RequestedShippingMinor: value.RequestedShippingMinor, RequestedTaxMinor: value.RequestedTaxMinor, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}
func returnItemResponse(value core.ReturnItem) returnItemView {
	return returnItemView{ID: value.ID.String(), ReturnID: value.ReturnID.String(), OrderItemID: value.OrderItemID, Unit: value.Requested.Unit, Disposition: string(value.Disposition), Requested: quantityResponse(value.Requested), Received: quantityResponse(value.Received), Accepted: quantityResponse(value.Accepted), Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}
func returnLogisticsResponse(value core.ReturnLogisticsOperation) returnLogisticsView {
	return returnLogisticsView{ID: value.ID.String(), ReturnID: value.ReturnID.String(), ConnectorAccountID: value.ConnectorAccountID, OriginalRemoteID: value.OriginalRemoteID, ExternalID: value.ExternalID, MailType: value.MailType, TariffCode: value.TariffCode, Status: string(value.Status), RemoteID: value.RemoteID, TrackingNumber: value.TrackingNumber, FailureCode: value.FailureCode, IdempotencyKey: value.IdempotencyKey, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}
func quantityResponse(value core.Quantity) quantityView {
	return quantityView{Coefficient: value.Coefficient, Scale: value.Scale, Unit: value.Unit}
}
func refundAllocationResponse(value core.RefundAllocation) refundAllocationView {
	return refundAllocationView{ID: value.ID.String(), PaymentID: value.PaymentID, RefundID: value.RefundID, ReturnID: value.ReturnID.String(), OrderItemID: value.OrderItemID, Component: string(value.Component), IdempotencyKey: value.IdempotencyKey, Amount: moneyView{MinorUnits: value.Amount.MinorUnits(), Currency: value.Currency.String()}, Version: value.Version, CreatedAt: value.CreatedAt}
}

func writeReturnsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, core.ErrConflict), errors.Is(err, core.ErrInvalidState), errors.Is(err, core.ErrOverAllocated):
		writeProblem(w, http.StatusConflict, "Operation state conflict")
	case errors.Is(err, core.ErrInvalidRecord), errors.Is(err, core.ErrInvalidScope):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, core.ErrQuotaExceeded):
		writeProblem(w, http.StatusTooManyRequests, "Quota Exceeded")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
