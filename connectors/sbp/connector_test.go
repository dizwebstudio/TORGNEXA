package sbp

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type ft struct{}

func (ft) Ping(context.Context, []byte) error { return nil }
func (ft) Create(_ context.Context, _ []byte, q sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	return sdk.PaymentCreateResult{RemoteID: "qrc:1", Status: "created", PaymentURL: "https://qr.nspk.example/synthetic", ExpiresAt: q.ExpiresAt, ObservedAt: time.Now().UTC()}, nil
}
func (ft) Status(context.Context, []byte, sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	return sdk.PaymentStatus{RemoteID: "qrc:1", Status: "paid", Amount: sdk.PaymentAmount{MinorUnits: 100, Currency: "RUB"}, ObservedAt: time.Now().UTC()}, nil
}
func (ft) Refund(context.Context, []byte, sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	return sdk.PaymentRefundResult{RemoteRefundID: "r1", Status: "accepted", ObservedAt: time.Now().UTC()}, nil
}
func (ft) Reconcile(context.Context, []byte, sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	return sdk.PaymentReconcileResult{ObservedAt: time.Now().UTC()}, nil
}
func (ft) VerifyWebhook(context.Context, []byte, []byte, []byte) (string, string, string, error) {
	return "delivery:1", "payment_paid", "qrc:1", nil
}

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return sec{} }

type sec struct{}

func (sec) UseSecret(_ context.Context, _ sdk.SecretReference, f func([]byte) error) error {
	return f([]byte("cert-bundle"))
}
func acc() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "s", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "sbp", Family: sdk.FamilyPayment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestIdempotentReferenceAndVerifiedWebhook(t *testing.T) {
	c := New(ft{}, nil)
	q := sdk.PaymentCreateRequest{ExternalID: "order:1", IdempotencyKey: "idem:1", Purpose: "order", Amount: sdk.PaymentAmount{MinorUnits: 100, Currency: "RUB"}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if _, e := c.CreatePayment(context.Background(), acc(), rt{}, q); e != nil {
		t.Fatal(e)
	}
	w, e := c.VerifyPaymentWebhook(context.Background(), acc(), rt{}, []byte(`{"status":"paid"}`), []byte("sig"))
	if e != nil || w.BodyDigest == "" {
		t.Fatalf("%+v %v", w, e)
	}
}
