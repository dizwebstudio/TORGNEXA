package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

// fakePaymentsRepository is an in-memory stand-in for payments.Repository
// good enough to exercise payment_webhooks.go's logic: optimistic
// concurrency, ON CONFLICT-shaped idempotent evidence recording, and exact
// call counting — not a substitute for paymentsrepo's own SQL-level tests.
type fakePaymentsRepository struct {
	byID            map[payments.PaymentID]payments.Payment
	byRemote        map[string]payments.PaymentID // connectorAccountID+"|"+remoteID -> PaymentID
	evidence        map[string]bool               // connectorAccountID+"|"+deliveryID
	changeStatusLog []payments.ChangePaymentStatus
}

const testGatewayID = "test-gateway"

func compositeKey(left, right string) string { return left + "|" + right }

func newFakePaymentsRepository() *fakePaymentsRepository {
	return &fakePaymentsRepository{byID: map[payments.PaymentID]payments.Payment{}, byRemote: map[string]payments.PaymentID{}, evidence: map[string]bool{}}
}

func (f *fakePaymentsRepository) seed(p payments.Payment) {
	f.byID[p.ID] = p
	if p.RemoteID != "" {
		accountRef := p.ConnectorAccountID
		f.byRemote[compositeKey(accountRef, p.RemoteID)] = p.ID
	}
}

func (f *fakePaymentsRepository) Payment(_ context.Context, _ payments.Scope, id payments.PaymentID) (payments.Payment, error) {
	p, ok := f.byID[id]
	if !ok {
		return payments.Payment{}, payments.ErrNotFound
	}
	return p, nil
}
func (f *fakePaymentsRepository) PaymentByExternalID(_ context.Context, _ payments.Scope, externalID string) (payments.Payment, error) {
	for _, p := range f.byID {
		if p.ExternalID == externalID {
			return p, nil
		}
	}
	return payments.Payment{}, payments.ErrNotFound
}
func (f *fakePaymentsRepository) PaymentByRemoteID(_ context.Context, _ payments.Scope, connectorAccountID, remoteID string) (payments.Payment, error) {
	accountRef := connectorAccountID
	id, ok := f.byRemote[compositeKey(accountRef, remoteID)]
	if !ok {
		return payments.Payment{}, payments.ErrNotFound
	}
	return f.byID[id], nil
}
func (f *fakePaymentsRepository) ListPayments(context.Context, payments.Scope, int) ([]payments.Payment, error) {
	out := make([]payments.Payment, 0, len(f.byID))
	for _, p := range f.byID {
		out = append(out, p)
	}
	return out, nil
}
func (f *fakePaymentsRepository) CreatePayment(context.Context, payments.Scope, payments.CreatePayment, payments.Mutation) (payments.Payment, error) {
	return payments.Payment{}, errNotImplementedInFake
}
func (f *fakePaymentsRepository) ChangePaymentStatus(_ context.Context, _ payments.Scope, command payments.ChangePaymentStatus, _ payments.Mutation) (payments.Payment, error) {
	f.changeStatusLog = append(f.changeStatusLog, command)
	current, ok := f.byID[command.ID]
	if !ok {
		return payments.Payment{}, payments.ErrNotFound
	}
	if current.Version != command.ExpectedVersion {
		return payments.Payment{}, payments.ErrConflict
	}
	if payments.ValidatePaymentTransition(current.Status, command.Status) != nil {
		return payments.Payment{}, payments.ErrInvalidState
	}
	current.Status = command.Status
	current.Version++
	if command.RemoteID != "" {
		current.RemoteID = command.RemoteID
	}
	if command.SucceededAt != nil {
		current.SucceededAt = command.SucceededAt
	}
	f.byID[command.ID] = current
	return current, nil
}
func (f *fakePaymentsRepository) Refund(context.Context, payments.Scope, payments.RefundID) (payments.Refund, error) {
	return payments.Refund{}, errNotImplementedInFake
}
func (f *fakePaymentsRepository) CreateRefund(context.Context, payments.Scope, payments.CreateRefund, payments.Mutation) (payments.Refund, error) {
	return payments.Refund{}, errNotImplementedInFake
}
func (f *fakePaymentsRepository) ChangeRefundStatus(context.Context, payments.Scope, payments.ChangeRefundStatus, payments.Mutation) (payments.Refund, error) {
	return payments.Refund{}, errNotImplementedInFake
}
func (f *fakePaymentsRepository) RecordWebhookEvidence(_ context.Context, _ payments.Scope, evidence payments.WebhookEvidence) (bool, error) {
	accountRef := evidence.ConnectorAccountID
	key := compositeKey(accountRef, evidence.DeliveryID)
	if f.evidence[key] {
		return false, nil
	}
	f.evidence[key] = true
	return true, nil
}

var errNotImplementedInFake = payments.ErrInvalidRecord

type fakeWebhookAccounts struct{ account sdk.Account }

func (f fakeWebhookAccounts) AccountByID(_ context.Context, _, _, accountID string) (sdk.Account, error) {
	if accountID != f.account.ID {
		return sdk.Account{}, sdk.ErrAccountNotFound
	}
	return f.account, nil
}

type fakeWebhookGateway struct {
	verify func(ctx context.Context, body, sig []byte) (sdk.PaymentWebhook, error)
}

func (f fakeWebhookGateway) CreatePayment(context.Context, sdk.Account, sdk.Runtime, sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	return sdk.PaymentCreateResult{}, errNotImplementedInFake
}
func (f fakeWebhookGateway) ReadPaymentStatus(context.Context, sdk.Account, sdk.Runtime, sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	return sdk.PaymentStatus{}, errNotImplementedInFake
}
func (f fakeWebhookGateway) RefundPayment(context.Context, sdk.Account, sdk.Runtime, sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	return sdk.PaymentRefundResult{}, errNotImplementedInFake
}
func (f fakeWebhookGateway) ReconcilePayments(context.Context, sdk.Account, sdk.Runtime, sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	return sdk.PaymentReconcileResult{}, errNotImplementedInFake
}
func (f fakeWebhookGateway) VerifyPaymentWebhook(ctx context.Context, _ sdk.Account, _ sdk.Runtime, body, sig []byte) (sdk.PaymentWebhook, error) {
	return f.verify(ctx, body, sig)
}

type fakeGatewayResolver struct {
	gateway fakeWebhookGateway
	err     error
	calls   int
}

func (f *fakeGatewayResolver) PaymentGateway(sdk.Account, builtinruntime.ConfigLoader) (builtinruntime.PaymentGateway, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.gateway, nil
}

// fakeWebhookSecrets satisfies secrets.SecretProvider for connectorruntime.New.
// The fake gateway in these tests never actually reads through it (it
// ignores the sdk.Runtime it is handed), so every method is a harmless stub.
type fakeWebhookSecrets struct{}

func (fakeWebhookSecrets) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errNotImplementedInFake
}
func (fakeWebhookSecrets) Use(_ context.Context, _ tenancy.Scope, _ secrets.Reference, fn func([]byte) error) error {
	return fn([]byte("secret-1\nsecret-2"))
}
func (fakeWebhookSecrets) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errNotImplementedInFake
}
func (fakeWebhookSecrets) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errNotImplementedInFake
}
func (fakeWebhookSecrets) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errNotImplementedInFake
}

var _ secrets.SecretProvider = fakeWebhookSecrets{}

func testWebhookAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "acct-1", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: testGatewayID, Family: sdk.FamilyPayment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef",
		Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func testWebhookScope() payments.Scope {
	scope, _ := payments.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	return scope
}

func testWebhookPayment() payments.Payment {
	now := time.Now().UTC()
	amount, _ := domain.NewMoney(15000, "RUB")
	return payments.Payment{
		ID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorAccountID: "acct-1", ExternalID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301", RemoteID: "yk_remote_1", Amount: amount,
		Status: payments.StatusCreated, Version: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
}

func webhookRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(body))
	req.RemoteAddr = "198.51.100.9:443"
	return req
}

func TestPaymentWebhookAppliesVerifiedTransitionExactlyOnce(t *testing.T) {
	repo := newFakePaymentsRepository()
	repo.seed(testWebhookPayment())
	resolver := &fakeGatewayResolver{gateway: fakeWebhookGateway{verify: func(context.Context, []byte, []byte) (sdk.PaymentWebhook, error) {
		return sdk.PaymentWebhook{DeliveryID: "delivery-1", EventType: "payment_succeeded", RemotePaymentID: "yk_remote_1", BodyDigest: strings.Repeat("a", 64), OccurredAt: time.Now().UTC()}, nil
	}}}
	routes := newPaymentWebhookRoutes(repo, fakeWebhookAccounts{account: testWebhookAccount()}, nil, fakeWebhookSecrets{}, nil)
	if len(routes) != 0 {
		t.Fatal("expected newPaymentWebhookRoutes to require a real *builtinruntime.Registry; got routes with nil registry")
	}
	api := paymentWebhookAPI{repository: repo, accounts: fakeWebhookAccounts{account: testWebhookAccount()}, secrets: fakeWebhookSecrets{}, registry: resolver}

	path := paymentWebhooksPathPrefix + testGatewayID + "/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002/acct-1"
	req := webhookRequest(path, `{"event":"payment.succeeded","object":{"id":"yk_remote_1","status":"succeeded"}}`)
	rr := httptest.NewRecorder()
	api.receive(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(repo.changeStatusLog) != 1 {
		t.Fatalf("expected exactly one ChangePaymentStatus call, got %d", len(repo.changeStatusLog))
	}
	updated := repo.byID[testWebhookPayment().ID]
	if updated.Status != payments.StatusSucceeded {
		t.Fatalf("status=%s, want succeeded", updated.Status)
	}
}

func TestPaymentWebhookRedeliveryIsANoOp(t *testing.T) {
	repo := newFakePaymentsRepository()
	repo.seed(testWebhookPayment())
	resolver := &fakeGatewayResolver{gateway: fakeWebhookGateway{verify: func(context.Context, []byte, []byte) (sdk.PaymentWebhook, error) {
		return sdk.PaymentWebhook{DeliveryID: "delivery-1", EventType: "payment_succeeded", RemotePaymentID: "yk_remote_1", BodyDigest: strings.Repeat("a", 64), OccurredAt: time.Now().UTC()}, nil
	}}}
	api := paymentWebhookAPI{repository: repo, accounts: fakeWebhookAccounts{account: testWebhookAccount()}, secrets: fakeWebhookSecrets{}, registry: resolver}
	path := paymentWebhooksPathPrefix + testGatewayID + "/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002/acct-1"
	body := `{"event":"payment.succeeded","object":{"id":"yk_remote_1","status":"succeeded"}}`

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		api.receive(rr, webhookRequest(path, body))
		if rr.Code != http.StatusOK {
			t.Fatalf("delivery %d status=%d", i, rr.Code)
		}
	}
	if len(repo.changeStatusLog) != 1 {
		t.Fatalf("expected exactly one ChangePaymentStatus call across two identical deliveries, got %d", len(repo.changeStatusLog))
	}
}

func TestPaymentWebhookIgnoresUnverifiedBodyStatus(t *testing.T) {
	repo := newFakePaymentsRepository()
	repo.seed(testWebhookPayment())
	// The verifier (as every real transport does) reports the AUTHORITATIVE
	// status independent of whatever the request body claims below.
	resolver := &fakeGatewayResolver{gateway: fakeWebhookGateway{verify: func(context.Context, []byte, []byte) (sdk.PaymentWebhook, error) {
		return sdk.PaymentWebhook{DeliveryID: "delivery-1", EventType: "payment_created", RemotePaymentID: "yk_remote_1", BodyDigest: strings.Repeat("a", 64), OccurredAt: time.Now().UTC()}, nil
	}}}
	api := paymentWebhookAPI{repository: repo, accounts: fakeWebhookAccounts{account: testWebhookAccount()}, secrets: fakeWebhookSecrets{}, registry: resolver}
	path := paymentWebhooksPathPrefix + testGatewayID + "/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002/acct-1"
	// Spoofed body claims succeeded; the verified EventType above says created.
	spoofed := `{"event":"payment.succeeded","object":{"id":"yk_remote_1","status":"succeeded"}}`
	rr := httptest.NewRecorder()
	api.receive(rr, webhookRequest(path, spoofed))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if len(repo.changeStatusLog) != 0 {
		t.Fatalf("no real status change (created==created) should not call ChangePaymentStatus, got %d calls", len(repo.changeStatusLog))
	}
	if got := repo.byID[testWebhookPayment().ID].Status; got != payments.StatusCreated {
		t.Fatalf("status=%s, want created (spoofed body must not move it to succeeded)", got)
	}
}

func TestPaymentWebhookUnknownAccountNeverReachesGatewayOrChangeStatus(t *testing.T) {
	repo := newFakePaymentsRepository()
	repo.seed(testWebhookPayment())
	resolver := &fakeGatewayResolver{gateway: fakeWebhookGateway{verify: func(context.Context, []byte, []byte) (sdk.PaymentWebhook, error) {
		t.Fatal("gateway must not be reached for an unknown account")
		return sdk.PaymentWebhook{}, nil
	}}}
	api := paymentWebhookAPI{repository: repo, accounts: fakeWebhookAccounts{account: testWebhookAccount()}, secrets: fakeWebhookSecrets{}, registry: resolver}
	path := paymentWebhooksPathPrefix + testGatewayID + "/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002/does-not-exist"
	rr := httptest.NewRecorder()
	api.receive(rr, webhookRequest(path, `{}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("unknown account should still get the uniform ack: status=%d", rr.Code)
	}
	if resolver.calls != 0 {
		t.Fatalf("gateway resolver should not be called for an unknown account, calls=%d", resolver.calls)
	}
	if len(repo.changeStatusLog) != 0 {
		t.Fatalf("ChangePaymentStatus must not run for an unknown account, got %d calls", len(repo.changeStatusLog))
	}
}

func TestPaymentWebhookVerificationFailureProducesSameResponseAsSuccess(t *testing.T) {
	repo := newFakePaymentsRepository()
	repo.seed(testWebhookPayment())
	resolver := &fakeGatewayResolver{gateway: fakeWebhookGateway{verify: func(context.Context, []byte, []byte) (sdk.PaymentWebhook, error) {
		return sdk.PaymentWebhook{}, errNotImplementedInFake
	}}}
	api := paymentWebhookAPI{repository: repo, accounts: fakeWebhookAccounts{account: testWebhookAccount()}, secrets: fakeWebhookSecrets{}, registry: resolver}
	path := paymentWebhooksPathPrefix + testGatewayID + "/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002/acct-1"
	rr := httptest.NewRecorder()
	api.receive(rr, webhookRequest(path, `{"event":"payment.succeeded","object":{"id":"yk_remote_1"}}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("verification failure should still get the uniform ack: status=%d", rr.Code)
	}
	if len(repo.changeStatusLog) != 0 {
		t.Fatalf("ChangePaymentStatus must not run after a verification failure, got %d calls", len(repo.changeStatusLog))
	}
}

func TestParsePaymentWebhookPath(t *testing.T) {
	gatewayID, orgID, wsID, accountID, ok := parsePaymentWebhookPath(paymentWebhooksPathPrefix + testGatewayID + "/org-1/ws-1/acct-1")
	if !ok || gatewayID != testGatewayID || orgID != "org-1" || wsID != "ws-1" || accountID != "acct-1" {
		t.Fatalf("parse = %q %q %q %q %v", gatewayID, orgID, wsID, accountID, ok)
	}
	for _, bad := range []string{
		paymentWebhooksPathPrefix + testGatewayID + "/org-1/ws-1",
		paymentWebhooksPathPrefix + testGatewayID + "/org-1/ws-1/acct-1/extra",
		paymentWebhooksPathPrefix + testGatewayID + "//ws-1/acct-1",
		webhookPathPrefix + "other/" + testGatewayID + "/org-1/ws-1/acct-1",
	} {
		if _, _, _, _, ok := parsePaymentWebhookPath(bad); ok {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}
