package sbp

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, string, []byte) error { return nil }
func (candidateTransport) Create(_ context.Context, _ string, _ []byte, q sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	return sdk.PaymentCreateResult{RemoteID: "qrc:synthetic", Status: "created", PaymentURL: "https://synthetic.invalid/qr", ExpiresAt: q.ExpiresAt, ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Status(context.Context, string, []byte, sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	return sdk.PaymentStatus{RemoteID: "qrc:synthetic", Status: "paid", Amount: sdk.PaymentAmount{MinorUnits: 100, Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Refund(context.Context, string, []byte, sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	return sdk.PaymentRefundResult{RemoteRefundID: "refund:synthetic", Status: "accepted", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Reconcile(context.Context, string, []byte, sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	return sdk.PaymentReconcileResult{ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) VerifyWebhook(context.Context, string, []byte, []byte, []byte) (string, string, string, error) {
	return "delivery:synthetic", "payment_paid", "qrc:synthetic", nil
}
