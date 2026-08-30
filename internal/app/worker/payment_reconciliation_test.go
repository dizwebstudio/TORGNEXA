package worker

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	corepayments "github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type paymentGatewayResolverStub struct {
	gateway builtinruntime.PaymentGateway
}

func (s paymentGatewayResolverStub) paymentGateway(tenancy.Scope, connectors.Account) (builtinruntime.PaymentGateway, error) {
	return s.gateway, nil
}

type paymentReconcileGatewayStub struct {
	result connectors.PaymentReconcileResult
}

func (s paymentReconcileGatewayStub) CreatePayment(context.Context, connectors.Account, connectors.Runtime, connectors.PaymentCreateRequest) (connectors.PaymentCreateResult, error) {
	return connectors.PaymentCreateResult{}, errors.New("not used")
}
func (s paymentReconcileGatewayStub) ReadPaymentStatus(context.Context, connectors.Account, connectors.Runtime, connectors.PaymentStatusRequest) (connectors.PaymentStatus, error) {
	return connectors.PaymentStatus{}, errors.New("not used")
}
func (s paymentReconcileGatewayStub) RefundPayment(context.Context, connectors.Account, connectors.Runtime, connectors.PaymentRefundRequest) (connectors.PaymentRefundResult, error) {
	return connectors.PaymentRefundResult{}, errors.New("not used")
}
func (s paymentReconcileGatewayStub) ReconcilePayments(context.Context, connectors.Account, connectors.Runtime, connectors.PaymentReconcileRequest) (connectors.PaymentReconcileResult, error) {
	return s.result, nil
}
func (s paymentReconcileGatewayStub) VerifyPaymentWebhook(context.Context, connectors.Account, connectors.Runtime, []byte, []byte) (connectors.PaymentWebhook, error) {
	return connectors.PaymentWebhook{}, errors.New("not used")
}

type paymentReconcileSecretsStub struct{}

func (paymentReconcileSecretsStub) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("not used")
}
func (paymentReconcileSecretsStub) Use(context.Context, tenancy.Scope, secrets.Reference, func([]byte) error) error {
	return errors.New("not used")
}
func (paymentReconcileSecretsStub) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("not used")
}
func (paymentReconcileSecretsStub) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("not used")
}
func (paymentReconcileSecretsStub) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("not used")
}

type paymentReconcileRefreshStub struct{}

func (paymentReconcileRefreshStub) WithRefreshLock(context.Context, tenancy.Scope, secrets.Reference, func(context.Context) error) error {
	return errors.New("not used")
}

type paymentAccountsStub struct{ account connectors.Account }

func (s paymentAccountsStub) ListAccounts(context.Context, string, string, string, int) ([]connectors.Account, error) {
	return []connectors.Account{s.account}, nil
}

type paymentStoreStub struct {
	payment       corepayments.Payment
	refund        corepayments.Refund
	change        *corepayments.ChangePaymentStatus
	changes       int
	refundChanges []corepayments.ChangeRefundStatus
}

func (s *paymentStoreStub) PaymentByRemoteID(context.Context, corepayments.Scope, string, string) (corepayments.Payment, error) {
	return s.payment, nil
}
func (s *paymentStoreStub) RefundByRemoteID(context.Context, corepayments.Scope, string, string) (corepayments.Refund, error) {
	if !s.refund.ID.Valid() {
		return corepayments.Refund{}, corepayments.ErrNotFound
	}
	return s.refund, nil
}
func (s *paymentStoreStub) ChangePaymentStatus(_ context.Context, _ corepayments.Scope, command corepayments.ChangePaymentStatus, _ corepayments.Mutation) (corepayments.Payment, error) {
	s.changes++
	s.change = &command
	s.payment.Status = command.Status
	s.payment.Version++
	return s.payment, nil
}
func (s *paymentStoreStub) ChangeRefundStatus(_ context.Context, _ corepayments.Scope, command corepayments.ChangeRefundStatus, _ corepayments.Mutation) (corepayments.Refund, error) {
	s.refundChanges = append(s.refundChanges, command)
	s.refund.Status = command.Status
	s.refund.RemoteRefundID = command.RemoteRefundID
	s.refund.Version++
	return s.refund, nil
}

func testPaymentReconciliationAccount() connectors.Account {
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	return connectors.Account{ID: "payment-account", OrganizationID: testTenantOrg, WorkspaceID: testTenantWorkspace, ConnectorID: "yoo" + "kassa", Family: connectors.FamilyPayment, Status: connectors.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: connectors.Health{Status: connectors.HealthHealthy, CheckedAt: now}, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
}

const (
	testTenantOrg       = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testTenantWorkspace = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func testPaymentReconciliationRunner(result connectors.PaymentReconcileResult, store *paymentStoreStub) paymentReconciliationRunner {
	account := testPaymentReconciliationAccount()
	return paymentReconciliationRunner{
		accounts: paymentAccountsStub{account: account}, payments: store, secrets: paymentReconcileSecretsStub{}, refresh: paymentReconcileRefreshStub{},
		registry: paymentGatewayResolverStub{gateway: paymentReconcileGatewayStub{result: result}}, now: func() time.Time { return time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC) },
	}
}

func testPaymentRecord(t *testing.T) corepayments.Payment {
	t.Helper()
	now := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	amount, err := domain.NewMoney(12500, domain.Currency("RUB"))
	if err != nil {
		t.Fatal(err)
	}
	return corepayments.Payment{ID: corepayments.PaymentID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101"), OrganizationID: testTenantOrg, WorkspaceID: testTenantWorkspace, ConnectorAccountID: "payment-account", ExternalID: "demo-payment-001", RemoteID: "yk-payment-001", Amount: amount, Status: corepayments.StatusCreated, Version: 2, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}
}

func testRefundRecord(t *testing.T) corepayments.Refund {
	t.Helper()
	now := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	amount, err := domain.NewMoney(5000, domain.Currency("RUB"))
	if err != nil {
		t.Fatal(err)
	}
	return corepayments.Refund{ID: corepayments.RefundID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102"), OrganizationID: testTenantOrg, WorkspaceID: testTenantWorkspace, PaymentID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101", ExternalID: "refund-001", RemoteRefundID: "refund-001", Amount: amount, Status: corepayments.RefundAccepted, Version: 1, CreatedAt: now, UpdatedAt: now}
}

func TestPaymentReconciliationAppliesRemoteStatusOnce(t *testing.T) {
	store := &paymentStoreStub{payment: testPaymentRecord(t)}
	observedAt := time.Date(2026, 8, 30, 9, 30, 0, 0, time.UTC)
	runner := testPaymentReconciliationRunner(connectors.PaymentReconcileResult{Items: []connectors.PaymentSettlement{{RemoteID: "yk-payment-001", Kind: "sale", Status: "succeeded", Amount: connectors.PaymentAmount{MinorUnits: 12500, Currency: "RUB"}, OccurredAt: observedAt}}, ObservedAt: observedAt}, store)
	scope, err := tenancy.ParseScope(testTenantOrg, testTenantWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.reconcileScope(context.Background(), scope, observedAt.Add(-time.Hour), observedAt, slog.Default()); err != nil {
		t.Fatal(err)
	}
	if store.change == nil || store.change.Status != corepayments.StatusSucceeded || store.change.SucceededAt == nil {
		t.Fatalf("expected succeeded status change, got %#v", store.change)
	}
	if store.change.ExpectedVersion != 2 || store.change.RemoteID != "yk-payment-001" {
		t.Fatalf("unexpected status command: %#v", store.change)
	}
	if err := runner.reconcileScope(context.Background(), scope, observedAt.Add(-time.Hour), observedAt, slog.Default()); err != nil {
		t.Fatal(err)
	}
	if store.changes != 1 {
		t.Fatalf("expected idempotent second sweep, got %d status changes", store.changes)
	}
}

func TestPaymentReconciliationAppliesRefundThroughCoreTransition(t *testing.T) {
	store := &paymentStoreStub{payment: testPaymentRecord(t), refund: testRefundRecord(t)}
	now := time.Date(2026, 8, 30, 9, 30, 0, 0, time.UTC)
	runner := testPaymentReconciliationRunner(connectors.PaymentReconcileResult{Items: []connectors.PaymentSettlement{{RemoteID: "refund-001", Kind: "refund", Status: "succeeded", Amount: connectors.PaymentAmount{MinorUnits: 5000, Currency: "RUB"}, OccurredAt: now}}, ObservedAt: now}, store)
	scope, err := tenancy.ParseScope(testTenantOrg, testTenantWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.reconcileScope(context.Background(), scope, now.Add(-time.Hour), now, slog.Default()); err != nil {
		t.Fatal(err)
	}
	if len(store.refundChanges) != 1 || store.refundChanges[0].Status != corepayments.RefundSucceeded {
		t.Fatalf("expected succeeded refund transition, got %#v", store.refundChanges)
	}
}

func TestPaymentReconciliationSkipsUnknownOrMismatchedObservation(t *testing.T) {
	for name, observation := range map[string]connectors.PaymentSettlement{
		"unknown status":  {RemoteID: "yk-payment-001", Kind: "sale", Status: "settled_by_partner", Amount: connectors.PaymentAmount{MinorUnits: 12500, Currency: "RUB"}, OccurredAt: time.Now().UTC()},
		"amount mismatch": {RemoteID: "yk-payment-001", Kind: "sale", Status: "succeeded", Amount: connectors.PaymentAmount{MinorUnits: 1, Currency: "RUB"}, OccurredAt: time.Now().UTC()},
	} {
		t.Run(name, func(t *testing.T) {
			store := &paymentStoreStub{payment: testPaymentRecord(t)}
			now := time.Now().UTC()
			runner := testPaymentReconciliationRunner(connectors.PaymentReconcileResult{Items: []connectors.PaymentSettlement{observation}, ObservedAt: now}, store)
			scope, err := tenancy.ParseScope(testTenantOrg, testTenantWorkspace)
			if err != nil {
				t.Fatal(err)
			}
			if err := runner.reconcileScope(context.Background(), scope, now.Add(-time.Hour), now, slog.Default()); err != nil {
				t.Fatal(err)
			}
			if store.change != nil {
				t.Fatalf("unsafe observation was applied: %#v", store.change)
			}
		})
	}
}
