package sbp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"strings"
)

type Transport interface {
	Ping(context.Context, []byte) error
	Create(context.Context, []byte, sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error)
	Status(context.Context, []byte, sdk.PaymentStatusRequest) (sdk.PaymentStatus, error)
	Refund(context.Context, []byte, sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error)
	Reconcile(context.Context, []byte, sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error)
	VerifyWebhook(context.Context, []byte, []byte, []byte) (string, string, string, error)
}

func manifest() sdk.Manifest {
	return sdk.Manifest{ID: "sbp", Name: "SBP", Family: sdk.FamilyPayment, Version: "1.0.0", SDKVersion: 1, Capabilities: []sdk.Capability{"payments.create", "payments.reconcile", "payments.refund", "payments.status.read", "payments.webhooks"}, Auth: []sdk.AuthRequirement{{Kind: sdk.AuthCertificate, SecretClass: "payment.certificate", Required: true}}, RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 200, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 500, MaxBackoffMS: 30000}}}
}
func (c *Connector) CreatePayment(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "payments.create") != nil {
		return sdk.PaymentCreateResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.PaymentCreateResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Create(ctx, s, q); return x })
	if e != nil {
		return sdk.PaymentCreateResult{}, e
	}
	if out.RemoteID == "" || out.Status == "" || (!strings.HasPrefix(out.PaymentURL, "https://") && out.PaymentURL != "") {
		return sdk.PaymentCreateResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}
func (c *Connector) ReadPaymentStatus(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "payments.status.read") != nil {
		return sdk.PaymentStatus{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.PaymentStatus
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Status(ctx, s, q); return x })
	if e != nil {
		return sdk.PaymentStatus{}, e
	}
	if out.Validate() != nil {
		return sdk.PaymentStatus{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}
func (c *Connector) RefundPayment(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	if q.Validate() != nil || sdk.RequireCapability(Manifest(), "payments.refund") != nil {
		return sdk.PaymentRefundResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.PaymentRefundResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Refund(ctx, s, q); return x })
	return out, e
}
func (c *Connector) ReconcilePayments(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	if q.From.IsZero() || q.To.Before(q.From) || sdk.RequireCapability(Manifest(), "payments.reconcile") != nil {
		return sdk.PaymentReconcileResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.PaymentReconcileResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Reconcile(ctx, s, q); return x })
	return out, e
}
func (c *Connector) VerifyPaymentWebhook(ctx context.Context, a sdk.Account, r sdk.Runtime, body, sig []byte) (sdk.PaymentWebhook, error) {
	if len(body) == 0 || len(body) > 1<<20 || len(sig) == 0 || sdk.RequireCapability(Manifest(), "payments.webhooks") != nil {
		return sdk.PaymentWebhook{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var delivery, event, remoteID string
	e := useSecret(ctx, r, a, func(s []byte) error {
		var x error
		delivery, event, remoteID, x = c.transport.VerifyWebhook(ctx, s, body, sig)
		return x
	})
	if e != nil {
		return sdk.PaymentWebhook{}, e
	}
	h := sha256.Sum256(body)
	out := sdk.PaymentWebhook{DeliveryID: delivery, EventType: event, RemotePaymentID: remoteID, BodyDigest: hex.EncodeToString(h[:]), OccurredAt: c.now().UTC()}
	if out.Validate() != nil {
		return sdk.PaymentWebhook{}, remote(sdk.ErrorUnauthorized, "webhook_unverified", 0)
	}
	return out, nil
}

var _ sdk.PaymentCreator = (*Connector)(nil)
var _ sdk.PaymentStatusReader = (*Connector)(nil)
var _ sdk.PaymentRefunder = (*Connector)(nil)
var _ sdk.PaymentReconciler = (*Connector)(nil)
var _ sdk.PaymentWebhookVerifier = (*Connector)(nil)
