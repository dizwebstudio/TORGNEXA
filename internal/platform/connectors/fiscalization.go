package connectors

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidFiscalRequest = errors.New("connectors: invalid fiscal request")

type FiscalKind string

const (
	FiscalSale       FiscalKind = "sale"
	FiscalRefund     FiscalKind = "refund"
	FiscalCorrection FiscalKind = "correction"
)

func (k FiscalKind) Validate() error {
	if k != FiscalSale && k != FiscalRefund && k != FiscalCorrection {
		return ErrInvalidFiscalRequest
	}
	return nil
}

type FiscalMarkingLink struct {
	CodeFingerprint    string
	VerificationStatus string
}

type FiscalReceiptRequest struct {
	ExternalID, IdempotencyKey, ArtifactRef string
	Kind                                    FiscalKind
	Total                                   PaymentAmount
	Marking                                 []FiscalMarkingLink
	CorrectionOf                            string
	CreatedAt                               time.Time
}

func (r FiscalReceiptRequest) Validate() error {
	if !paymentRefPattern.MatchString(r.ExternalID) || !paymentRefPattern.MatchString(r.IdempotencyKey) || r.Kind.Validate() != nil || r.Total.Validate() != nil || r.CreatedAt.IsZero() || r.CreatedAt.Location() != time.UTC {
		return ErrInvalidFiscalRequest
	}
	if r.ArtifactRef != "" && !paymentRefPattern.MatchString(r.ArtifactRef) {
		return ErrInvalidFiscalRequest
	}
	if r.Kind == FiscalCorrection && !paymentRefPattern.MatchString(r.CorrectionOf) {
		return ErrInvalidFiscalRequest
	}
	if len(r.Marking) > 1000 {
		return ErrInvalidFiscalRequest
	}
	for _, item := range r.Marking {
		if len(item.CodeFingerprint) != 64 || !safeCodePattern.MatchString(item.VerificationStatus) {
			return ErrInvalidFiscalRequest
		}
	}
	return nil
}

type FiscalReceiptResult struct {
	RemoteID, State, FiscalDocumentRef string
	ObservedAt                         time.Time
}

func (r FiscalReceiptResult) Validate() error {
	if !paymentRefPattern.MatchString(r.RemoteID) || !safeCodePattern.MatchString(r.State) || r.ObservedAt.IsZero() || r.ObservedAt.Location() != time.UTC {
		return ErrInvalidFiscalRequest
	}
	if r.FiscalDocumentRef != "" && !paymentRefPattern.MatchString(r.FiscalDocumentRef) {
		return ErrInvalidFiscalRequest
	}
	return nil
}

type FiscalStatusRequest struct{ RemoteID string }

func (r FiscalStatusRequest) Validate() error {
	if !paymentRefPattern.MatchString(r.RemoteID) {
		return ErrInvalidFiscalRequest
	}
	return nil
}

type FiscalReceiptWriter interface {
	WriteFiscalReceipt(context.Context, Account, Runtime, FiscalReceiptRequest) (FiscalReceiptResult, error)
}

type FiscalStatusReader interface {
	ReadFiscalStatus(context.Context, Account, Runtime, FiscalStatusRequest) (FiscalReceiptResult, error)
}
