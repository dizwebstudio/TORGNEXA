package connectors

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
)

var ErrInvalidProductPublication = errors.New("connectors: invalid product publication")

// ProductPublicationRequest is the only payload accepted by the marketplace
// publication surface. The snapshot is provider-neutral and contains no raw
// provider JSON, access token, URL or object-store key.
type ProductPublicationRequest struct {
	Operation         marketplacepublication.OperationKind `json:"operation"`
	Snapshot          marketplacepublication.Snapshot      `json:"snapshot"`
	RemoteID          string                               `json:"remote_id,omitempty"`
	IdempotencyKey    string                               `json:"idempotency_key"`
	DryRun            bool                                 `json:"dry_run"`
	ApprovalRequestID string                               `json:"approval_request_id,omitempty"`
	QualityReceiptID  string                               `json:"quality_receipt_id"`
}

func (request ProductPublicationRequest) Validate() error {
	if !request.Operation.Valid() || request.Snapshot.Validate() != nil || !validIdempotencyKey(request.IdempotencyKey) || !validReference(request.QualityReceiptID) {
		return ErrInvalidProductPublication
	}
	digest, err := request.Snapshot.ComputeDigest()
	if err != nil || (request.Snapshot.Digest != "" && request.Snapshot.Digest != digest) {
		return ErrInvalidProductPublication
	}
	if request.RemoteID != "" && !validRemoteID(request.RemoteID) {
		return ErrInvalidProductPublication
	}
	if request.ApprovalRequestID != "" && !validReference(request.ApprovalRequestID) {
		return ErrInvalidProductPublication
	}
	if !request.DryRun && request.ApprovalRequestID == "" {
		return ErrInvalidProductPublication
	}
	if request.Operation == marketplacepublication.OperationCreateProduct && request.RemoteID != "" {
		return ErrInvalidProductPublication
	}
	if request.Operation != marketplacepublication.OperationCreateProduct && request.Operation != marketplacepublication.OperationStatusRead && request.RemoteID == "" {
		return ErrInvalidProductPublication
	}
	return nil
}

// PublicationResultStatus is a normalized connector result. Accepted and
// processing are not published; the worker must read remote status first.
type PublicationResultStatus string

const (
	PublicationAccepted   PublicationResultStatus = "accepted"
	PublicationProcessing PublicationResultStatus = "processing"
	PublicationPublished  PublicationResultStatus = "published"
	PublicationRejected   PublicationResultStatus = "rejected"
	PublicationUnknown    PublicationResultStatus = "unknown"
	PublicationDryRun     PublicationResultStatus = "dry_run"
)

func (status PublicationResultStatus) Valid() bool {
	switch status {
	case PublicationAccepted, PublicationProcessing, PublicationPublished, PublicationRejected, PublicationUnknown, PublicationDryRun:
		return true
	default:
		return false
	}
}

// ProductPublicationReceipt contains only bounded normalized metadata from a
// remote write. Provider response bodies are never returned to callers.
type ProductPublicationReceipt struct {
	Status            PublicationResultStatus `json:"status"`
	RemoteID          string                  `json:"remote_id,omitempty"`
	RemoteOperationID string                  `json:"remote_operation_id,omitempty"`
	RemoteRequestID   string                  `json:"remote_request_id,omitempty"`
	ErrorCode         string                  `json:"error_code,omitempty"`
	ObservedAt        time.Time               `json:"observed_at"`
}

func (receipt ProductPublicationReceipt) Validate() error {
	if !receipt.Status.Valid() || receipt.ObservedAt.IsZero() || receipt.ObservedAt.Location() != time.UTC {
		return ErrInvalidProductPublication
	}
	for _, value := range []string{receipt.RemoteID, receipt.RemoteOperationID, receipt.RemoteRequestID} {
		if value != "" && !validRemoteID(value) {
			return ErrInvalidProductPublication
		}
	}
	if receipt.ErrorCode != "" && !safeCodePattern.MatchString(receipt.ErrorCode) {
		return ErrInvalidProductPublication
	}
	if receipt.Status == PublicationRejected && receipt.ErrorCode == "" {
		return ErrInvalidProductPublication
	}
	return nil
}

// ProductPublicationStatusQuery is used to resolve asynchronous or unknown
// writes without exposing provider-specific query parameters.
type ProductPublicationStatusQuery struct {
	RemoteID          string
	RemoteOperationID string
	IdempotencyKey    string
}

func (query ProductPublicationStatusQuery) Validate() error {
	if (query.RemoteID == "" && query.RemoteOperationID == "") || (query.RemoteID != "" && !validRemoteID(query.RemoteID)) || (query.RemoteOperationID != "" && !validRemoteID(query.RemoteOperationID)) || !validIdempotencyKey(query.IdempotencyKey) {
		return ErrInvalidProductPublication
	}
	return nil
}

// ProductPublicationWriter is an additive SDK v1 capability. Existing
// storefront ProductWriter remains unchanged for backward compatibility.
type ProductPublicationWriter interface {
	WriteProductPublication(context.Context, Account, Runtime, ProductPublicationRequest) (ProductPublicationReceipt, error)
}

// ProductPublicationStatusReader resolves asynchronous and unknown outcomes.
type ProductPublicationStatusReader interface {
	ReadProductPublicationStatus(context.Context, Account, Runtime, ProductPublicationStatusQuery) (ProductPublicationReceipt, error)
}

func validReference(value string) bool {
	return value != "" && len(value) <= 192 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}
