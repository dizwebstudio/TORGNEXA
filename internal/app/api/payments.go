package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	paymentsPath = "/api/v1/payments"
)

type paymentsAPIRepository interface {
	payments.Repository
	ListPayments(context.Context, payments.Scope, int) ([]payments.Payment, error)
}

type paymentsConnectorAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
}

type paymentsRuntimeConfig interface {
	Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error)
}

type paymentsAPI struct {
	repository paymentsAPIRepository
	accounts   paymentsConnectorAccounts
	configs    paymentsRuntimeConfig
	secrets    secrets.SecretProvider
	registry   *builtinruntime.Registry
}

type paymentView struct {
	ID                   string     `json:"id"`
	ConnectorAccountID   string     `json:"connector_account_id"`
	ExternalID           string     `json:"external_id"`
	RemoteID             string     `json:"remote_id,omitempty"`
	Purpose              string     `json:"purpose,omitempty"`
	Amount               moneyView  `json:"amount"`
	CommissionMinorUnits int64      `json:"commission_minor_units"`
	Status               string     `json:"status"`
	ReasonCode           string     `json:"reason_code,omitempty"`
	Version              int64      `json:"version"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	SucceededAt          *time.Time `json:"succeeded_at,omitempty"`
}

type moneyView struct {
	MinorUnits int64  `json:"minor_units"`
	Currency   string `json:"currency"`
}

type refundView struct {
	ID             string    `json:"id"`
	PaymentID      string    `json:"payment_id"`
	ExternalID     string    `json:"external_id"`
	RemoteRefundID string    `json:"remote_refund_id,omitempty"`
	Amount         moneyView `json:"amount"`
	Status         string    `json:"status"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
}

func newPaymentsRoutes(repository paymentsAPIRepository, accounts paymentsConnectorAccounts, configs paymentsRuntimeConfig, secretSource secrets.SecretProvider, registry *builtinruntime.Registry) []ProtectedRoute {
	api := paymentsAPI{repository: repository, accounts: accounts, configs: configs, secrets: secretSource, registry: registry}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: paymentsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listPayments)},
		{Method: http.MethodPost, Path: paymentsPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.createPayment)},
		{Method: http.MethodGet, Path: paymentsPath + "/", PathPrefix: true, Permission: "connectors.read", Handler: http.HandlerFunc(api.getOrRefund)},
		{Method: http.MethodPost, Path: paymentsPath + "/", PathPrefix: true, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.getOrRefund)},
	}
}

// getOrRefund dispatches GET /api/v1/payments/{id} and POST
// /api/v1/payments/{id}/refund from one prefix route, matching the
// PathPrefix registration social.go uses for its own nested resources.
func (api paymentsAPI) getOrRefund(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, paymentsPath+"/")
	if strings.HasSuffix(rest, "/refund") && r.Method == http.MethodPost {
		api.createRefund(w, r, strings.TrimSuffix(rest, "/refund"))
		return
	}
	if r.Method == http.MethodGet && !strings.Contains(rest, "/") {
		api.getPayment(w, r, rest)
		return
	}
	writeProblem(w, http.StatusNotFound, "Not Found")
}

func (api paymentsAPI) listPayments(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := paymentsRequestScope(w, r)
	if !ok || api.repository == nil {
		return
	}
	limit, valid := paymentsLimit(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, err := api.repository.ListPayments(r.Context(), scope, limit)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	views := make([]paymentView, 0, len(items))
	for _, item := range items {
		views = append(views, paymentResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api paymentsAPI) getPayment(w http.ResponseWriter, r *http.Request, rawID string) {
	tenantScope, scope, ok := paymentsRequestScope(w, r)
	if !ok || api.repository == nil {
		return
	}
	id, err := payments.ParsePaymentID(rawID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	payment, err := api.repository.Payment(r.Context(), scope, id)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	// There is no live webhook ingress yet (see the payments plan's "Known
	// limitation" — provider-to-us callbacks need their own unauthenticated,
	// signature-verified ingress, which is separate infrastructure this
	// session did not build). Until then, a single-payment read is the
	// natural place to refresh a still-pending remote status: the caller is
	// already waiting on this exact payment (e.g. polling after checkout
	// redirect), so one extra remote round trip here is the cheapest way to
	// avoid a stuck "created" row. Bulk freshness across many payments still
	// needs a periodic reconciliation job, which remains future work.
	if payment.Status == payments.StatusCreated && api.accounts != nil && api.registry != nil {
		if refreshed, refreshErr := api.refreshPaymentStatus(r.Context(), tenantScope, scope, payment); refreshErr == nil {
			payment = refreshed
		}
	}
	writeJSON(w, http.StatusOK, paymentResponse(payment))
}

// refreshPaymentStatus re-reads the remote-authoritative status for one
// payment and, if it has moved to a new canonical state, persists that
// transition. Any failure here (rail unavailable, unknown remote status,
// version conflict from a concurrent update) is swallowed by the caller,
// which falls back to the last-known DB row rather than failing the read.
func (api paymentsAPI) refreshPaymentStatus(ctx context.Context, tenantScope tenancy.Scope, scope payments.Scope, payment payments.Payment) (payments.Payment, error) {
	account, err := api.accounts.AccountByID(ctx, tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), payment.ConnectorAccountID)
	if err != nil || account.Family != sdk.FamilyPayment {
		return payments.Payment{}, errors.New("payments: account unavailable")
	}
	gateway, err := api.registry.PaymentGateway(account, api.configLoader(tenantScope))
	if err != nil {
		return payments.Payment{}, err
	}
	runtime, err := connectorruntime.New(api.secrets, tenantScope)
	if err != nil {
		return payments.Payment{}, err
	}
	remote, err := gateway.ReadPaymentStatus(ctx, account, runtime, sdk.PaymentStatusRequest{RemoteID: payment.RemoteID})
	if err != nil {
		return payments.Payment{}, err
	}
	target := paymentsCanonicalStatus(remote.Status)
	if target == payment.Status {
		return payment, nil
	}
	if payments.ValidatePaymentTransition(payment.Status, target) != nil {
		return payments.Payment{}, payments.ErrInvalidState
	}
	change := payments.ChangePaymentStatus{ID: payment.ID, ExpectedVersion: payment.Version, Status: target, RemoteStatus: remote.Status, CommissionMinorUnits: remote.CommissionMinorUnits}
	if target == payments.StatusSucceeded {
		now := time.Now().UTC()
		change.SucceededAt = &now
	}
	if target == payments.StatusFailed {
		change.ReasonCode = "provider_declined"
	}
	return api.repository.ChangePaymentStatus(ctx, scope, change, paymentsMutation("system:status_refresh", payment.ExternalID))
}

// paymentsCanonicalStatus maps each provider's own status vocabulary
// (YooKassa: succeeded/canceled; SBP-shaped gateways: paid/cancelled) onto
// TORGNEXA's canonical Status. Anything not recognized as a terminal state
// stays "created" rather than guessing.
func paymentsCanonicalStatus(remoteStatus string) payments.Status {
	switch remoteStatus {
	case "succeeded", "paid":
		return payments.StatusSucceeded
	case "canceled", "cancelled":
		return payments.StatusCanceled
	case "failed", "declined", "rejected":
		return payments.StatusFailed
	default:
		return payments.StatusCreated
	}
}

func (api paymentsAPI) createPayment(w http.ResponseWriter, r *http.Request) {
	tenantScope, scope, ok := paymentsRequestScope(w, r)
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID                 string `json:"id"`
		ConnectorAccountID string `json:"connector_account_id"`
		Purpose            string `json:"purpose"`
		Amount             struct {
			MinorUnits int64  `json:"minor_units"`
			Currency   string `json:"currency"`
		} `json:"amount"`
		ExpiresInSeconds int `json:"expires_in_seconds"`
	}
	if !ok || !principalOK || api.repository == nil || api.accounts == nil || api.registry == nil || decodeStrictJSON(r, &input) != nil || key != input.ID {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id, err := payments.ParsePaymentID(input.ID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if input.ExpiresInSeconds < 60 || input.ExpiresInSeconds > 86400 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	currency, err := domain.NewCurrency(input.Amount.Currency)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	amount, err := domain.NewMoney(input.Amount.MinorUnits, currency)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), strings.TrimSpace(input.ConnectorAccountID))
	if err != nil || account.Family != sdk.FamilyPayment || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Payment connector account unavailable")
		return
	}
	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInSeconds) * time.Second)
	command := payments.CreatePayment{ID: id, ConnectorAccountID: account.ID, ExternalID: input.ID, Purpose: strings.TrimSpace(input.Purpose), Amount: amount, ExpiresAt: expiresAt}
	created, err := api.repository.CreatePayment(r.Context(), scope, command, paymentsMutation(principal.Subject, key))
	if errors.Is(err, payments.ErrConflict) {
		created, err = api.repository.Payment(r.Context(), scope, id)
		if err == nil && (created.ConnectorAccountID != account.ID || created.ExternalID != input.ID || created.Amount.MinorUnits() != amount.MinorUnits() || created.Amount.Currency() != amount.Currency()) {
			err = payments.ErrConflict
		}
	}
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	if created.Status == payments.StatusPending {
		created, err = api.dispatchCreate(r.Context(), tenantScope, scope, account, created, principal.Subject, key)
		if err != nil {
			writePaymentsError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, paymentResponse(created))
}

// dispatchCreate makes the one outbound call to the payment rail, synchronous
// with the API request — matching how a real checkout flow needs the
// provider's redirect URL back immediately, not after a worker cycle. A
// failure here still leaves a durable "pending" row the reconciliation
// worker will pick up, so a request that times out client-side never loses
// track of money.
func (api paymentsAPI) dispatchCreate(ctx context.Context, tenantScope tenancy.Scope, scope payments.Scope, account sdk.Account, payment payments.Payment, actor, correlation string) (payments.Payment, error) {
	gateway, err := api.registry.PaymentGateway(account, api.configLoader(tenantScope))
	if err != nil {
		return payments.Payment{}, err
	}
	runtime, err := connectorruntime.New(api.secrets, tenantScope)
	if err != nil {
		return payments.Payment{}, err
	}
	result, createErr := gateway.CreatePayment(ctx, account, runtime, sdk.PaymentCreateRequest{
		ExternalID: payment.ExternalID, IdempotencyKey: payment.ExternalID, Purpose: payment.Purpose, Amount: sdk.PaymentAmount{MinorUnits: payment.Amount.MinorUnits(), Currency: string(payment.Amount.Currency())}, ExpiresAt: payment.ExpiresAt,
	})
	if createErr != nil {
		reason := paymentsReasonCode(createErr)
		return api.repository.ChangePaymentStatus(ctx, scope, payments.ChangePaymentStatus{ID: payment.ID, ExpectedVersion: payment.Version, Status: payments.StatusFailed, ReasonCode: reason}, paymentsMutation(actor, correlation))
	}
	return api.repository.ChangePaymentStatus(ctx, scope, payments.ChangePaymentStatus{ID: payment.ID, ExpectedVersion: payment.Version, Status: payments.StatusCreated, RemoteID: result.RemoteID, RemoteStatus: result.Status}, paymentsMutation(actor, correlation))
}

func (api paymentsAPI) createRefund(w http.ResponseWriter, r *http.Request, rawPaymentID string) {
	tenantScope, scope, ok := paymentsRequestScope(w, r)
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input struct {
		ID     string `json:"id"`
		Amount struct {
			MinorUnits int64  `json:"minor_units"`
			Currency   string `json:"currency"`
		} `json:"amount"`
	}
	paymentID, idErr := payments.ParsePaymentID(rawPaymentID)
	if !ok || !principalOK || api.repository == nil || api.accounts == nil || api.registry == nil || idErr != nil || decodeStrictJSON(r, &input) != nil || key != input.ID {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	refundID, err := payments.ParseRefundID(input.ID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	payment, err := api.repository.Payment(r.Context(), scope, paymentID)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	currency, err := domain.NewCurrency(input.Amount.Currency)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	amount, err := domain.NewMoney(input.Amount.MinorUnits, currency)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), payment.ConnectorAccountID)
	if err != nil || account.Family != sdk.FamilyPayment || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Payment connector account unavailable")
		return
	}
	command := payments.CreateRefund{ID: refundID, PaymentID: paymentID, ExternalID: input.ID, Amount: amount}
	refund, err := api.repository.CreateRefund(r.Context(), scope, command, paymentsMutation(principal.Subject, key))
	if errors.Is(err, payments.ErrConflict) {
		refund, err = api.repository.Refund(r.Context(), scope, refundID)
		if err == nil && (refund.PaymentID != paymentID || refund.ExternalID != input.ID) {
			err = payments.ErrConflict
		}
	}
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	if refund.Status == payments.RefundPending {
		refund, err = api.dispatchRefund(r.Context(), tenantScope, scope, account, payment, refund, principal.Subject, key)
		if err != nil {
			writePaymentsError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, refundResponse(refund))
}

func (api paymentsAPI) dispatchRefund(ctx context.Context, tenantScope tenancy.Scope, scope payments.Scope, account sdk.Account, payment payments.Payment, refund payments.Refund, actor, correlation string) (payments.Refund, error) {
	gateway, err := api.registry.PaymentGateway(account, api.configLoader(tenantScope))
	if err != nil {
		return payments.Refund{}, err
	}
	runtime, err := connectorruntime.New(api.secrets, tenantScope)
	if err != nil {
		return payments.Refund{}, err
	}
	result, refundErr := gateway.RefundPayment(ctx, account, runtime, sdk.PaymentRefundRequest{
		RemotePaymentID: payment.RemoteID, ExternalID: refund.ExternalID, IdempotencyKey: refund.ExternalID, Amount: sdk.PaymentAmount{MinorUnits: refund.Amount.MinorUnits(), Currency: string(refund.Amount.Currency())},
	})
	if refundErr != nil {
		return refund, refundErr
	}
	status := payments.RefundAccepted
	if result.Status == "succeeded" {
		status = payments.RefundSucceeded
	}
	return api.repository.ChangeRefundStatus(ctx, scope, payments.ChangeRefundStatus{ID: refund.ID, ExpectedVersion: refund.Version, Status: status, RemoteRefundID: result.RemoteRefundID}, paymentsMutation(actor, correlation))
}

func (api paymentsAPI) configLoader(tenantScope tenancy.Scope) builtinruntime.ConfigLoader {
	if api.configs == nil {
		return nil
	}
	return func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, err := api.configs.Config(ctx, tenantScope, accountID)
		return raw, err
	}
}

func paymentsReasonCode(err error) string {
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		value := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			if r == '.' || r == '-' {
				return '_'
			}
			return -1
		}, remote.Code)
		if len(value) > 0 && len(value) <= 64 && value[0] >= 'a' && value[0] <= 'z' {
			return value
		}
	}
	return "remote_rejected"
}

func paymentsMutation(actor, correlation string) payments.Mutation {
	now := time.Now().UTC()
	return payments.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: boundedActorRef(actor), Source: "api.payments", CorrelationID: correlation, OccurredAt: now}
}

func paymentResponse(payment payments.Payment) paymentView {
	return paymentView{
		ID: payment.ID.String(), ConnectorAccountID: payment.ConnectorAccountID, ExternalID: payment.ExternalID, RemoteID: payment.RemoteID, Purpose: payment.Purpose,
		Amount: moneyView{MinorUnits: payment.Amount.MinorUnits(), Currency: string(payment.Amount.Currency())}, CommissionMinorUnits: payment.CommissionMinorUnits,
		Status: string(payment.Status), ReasonCode: payment.ReasonCode, Version: payment.Version, CreatedAt: payment.CreatedAt, UpdatedAt: payment.UpdatedAt, SucceededAt: payment.SucceededAt,
	}
}

func refundResponse(refund payments.Refund) refundView {
	return refundView{
		ID: refund.ID.String(), PaymentID: refund.PaymentID.String(), ExternalID: refund.ExternalID, RemoteRefundID: refund.RemoteRefundID,
		Amount: moneyView{MinorUnits: refund.Amount.MinorUnits(), Currency: string(refund.Amount.Currency())}, Status: string(refund.Status), Version: refund.Version, CreatedAt: refund.CreatedAt,
	}
}

func paymentsRequestScope(w http.ResponseWriter, r *http.Request) (tenancy.Scope, payments.Scope, bool) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, payments.Scope{}, false
	}
	value, err := payments.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, payments.Scope{}, false
	}
	return scope, value, true
}

func paymentsLimit(r *http.Request) (int, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		limit = value
	}
	return limit, limit >= 1 && limit <= 200
}

func writePaymentsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, payments.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, payments.ErrConflict), errors.Is(err, payments.ErrInvalidState), errors.Is(err, payments.ErrRailUnavailable):
		writeProblem(w, http.StatusConflict, "Payment state conflict")
	case errors.Is(err, payments.ErrInvalidRecord):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, builtinruntime.ErrUnavailable), errors.Is(err, builtinruntime.ErrConfigurationNeeded):
		writeProblem(w, http.StatusConflict, "Payment rail unavailable")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
