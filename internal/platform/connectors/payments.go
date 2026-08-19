package connectors

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidPaymentRequest = errors.New("connectors: invalid payment request")
var paymentRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

type PaymentAmount struct {
	MinorUnits int64
	Currency   string
}

func (a PaymentAmount) Validate() error {
	if a.MinorUnits <= 0 || len(a.Currency) != 3 {
		return ErrInvalidPaymentRequest
	}
	for _, c := range []byte(a.Currency) {
		if c < 'A' || c > 'Z' {
			return ErrInvalidPaymentRequest
		}
	}
	return nil
}

type PaymentCreateRequest struct {
	ExternalID, IdempotencyKey, Purpose string
	Amount                              PaymentAmount
	ExpiresAt                           time.Time
}
type PaymentCreateResult struct {
	RemoteID, Status, PaymentURL string
	ExpiresAt, ObservedAt        time.Time
}

func (r PaymentCreateRequest) Validate() error {
	if !paymentRefPattern.MatchString(r.ExternalID) || !paymentRefPattern.MatchString(r.IdempotencyKey) || r.Amount.Validate() != nil || len(r.Purpose) > 210 || r.ExpiresAt.IsZero() {
		return ErrInvalidPaymentRequest
	}
	return nil
}

type PaymentStatusRequest struct{ RemoteID string }
type PaymentStatus struct {
	RemoteID, Status     string
	Amount               PaymentAmount
	CommissionMinorUnits int64
	ObservedAt           time.Time
}

func (r PaymentStatusRequest) Validate() error {
	if !paymentRefPattern.MatchString(r.RemoteID) {
		return ErrInvalidPaymentRequest
	}
	return nil
}
func (s PaymentStatus) Validate() error {
	if !paymentRefPattern.MatchString(s.RemoteID) || !safeCodePattern.MatchString(s.Status) || s.Amount.Validate() != nil || s.CommissionMinorUnits < 0 || s.ObservedAt.IsZero() || s.ObservedAt.Location() != time.UTC {
		return ErrInvalidPaymentRequest
	}
	return nil
}

type PaymentRefundRequest struct {
	RemotePaymentID, ExternalID, IdempotencyKey string
	Amount                                      PaymentAmount
}
type PaymentRefundResult struct {
	RemoteRefundID, Status string
	ObservedAt             time.Time
}

func (r PaymentRefundRequest) Validate() error {
	if !paymentRefPattern.MatchString(r.RemotePaymentID) || !paymentRefPattern.MatchString(r.ExternalID) || !paymentRefPattern.MatchString(r.IdempotencyKey) || r.Amount.Validate() != nil {
		return ErrInvalidPaymentRequest
	}
	return nil
}

type PaymentReconcileRequest struct{ From, To time.Time }
type PaymentSettlement struct {
	RemoteID, Kind       string
	Amount               PaymentAmount
	CommissionMinorUnits int64
	OccurredAt           time.Time
}
type PaymentReconcileResult struct {
	Items      []PaymentSettlement
	ObservedAt time.Time
}

type PaymentWebhook struct {
	DeliveryID, EventType, RemotePaymentID string
	BodyDigest                             string
	OccurredAt                             time.Time
}

func (w PaymentWebhook) Validate() error {
	if !paymentRefPattern.MatchString(w.DeliveryID) || !safeCodePattern.MatchString(w.EventType) || !paymentRefPattern.MatchString(w.RemotePaymentID) || len(w.BodyDigest) != 64 || w.OccurredAt.IsZero() {
		return ErrInvalidPaymentRequest
	}
	return nil
}

type PaymentCreator interface {
	CreatePayment(context.Context, Account, Runtime, PaymentCreateRequest) (PaymentCreateResult, error)
}
type PaymentStatusReader interface {
	ReadPaymentStatus(context.Context, Account, Runtime, PaymentStatusRequest) (PaymentStatus, error)
}
type PaymentRefunder interface {
	RefundPayment(context.Context, Account, Runtime, PaymentRefundRequest) (PaymentRefundResult, error)
}
type PaymentReconciler interface {
	ReconcilePayments(context.Context, Account, Runtime, PaymentReconcileRequest) (PaymentReconcileResult, error)
}
type PaymentWebhookVerifier interface {
	VerifyPaymentWebhook(context.Context, Account, Runtime, []byte, []byte) (PaymentWebhook, error)
}
