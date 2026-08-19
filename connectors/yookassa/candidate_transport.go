package yookassa

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
func (candidateTransport) Create(_ context.Context, _ []byte, q sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	return sdk.PaymentCreateResult{RemoteID: "pay:1", Status: "pending", PaymentURL: "https://checkout.example/pay/1", ExpiresAt: q.ExpiresAt, ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Status(_ context.Context, _ []byte, q sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	return sdk.PaymentStatus{RemoteID: q.RemoteID, Status: "succeeded", Amount: sdk.PaymentAmount{MinorUnits: 1000, Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Refund(context.Context, []byte, sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	return sdk.PaymentRefundResult{RemoteRefundID: "refund:1", Status: "succeeded", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Reconcile(context.Context, []byte, sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	return sdk.PaymentReconcileResult{ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) VerifyWebhook(context.Context, []byte, []byte, []byte) (string, string, string, error) {
	return "delivery:1", "payment.succeeded", "pay:1", nil
}
