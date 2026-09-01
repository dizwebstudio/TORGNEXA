package connectors

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
)

var ErrInvalidMarketplaceListing = errors.New("connectors: invalid marketplace listing")

// MarketplaceListingTaxonomyRequest asks a connector for its current,
// normalized taxonomy. Provider-specific category trees stay inside the
// adapter and are never returned as raw payloads.
type MarketplaceListingTaxonomyRequest struct {
	Locale       string
	Jurisdiction string
}

func (request MarketplaceListingTaxonomyRequest) Validate() error {
	if len(request.Locale) < 2 || len(request.Locale) > 16 || strings.TrimSpace(request.Locale) != request.Locale || len(request.Jurisdiction) != 2 || request.Jurisdiction != strings.ToUpper(request.Jurisdiction) {
		return ErrInvalidMarketplaceListing
	}
	return nil
}

// MarketplaceListingTaxonomyReader reads a provider taxonomy and normalizes
// it into the provider-neutral Core contract.
type MarketplaceListingTaxonomyReader interface {
	ReadMarketplaceListingTaxonomy(context.Context, Account, Runtime, MarketplaceListingTaxonomyRequest) (marketplacelisting.Taxonomy, error)
}

// MarketplaceListingWriter is the qualified remote write port for a complete
// provider-neutral listing draft. Implementations must support dry-run when
// the remote API offers validation and must resolve unknown outcomes by the
// status reader before retrying.
type MarketplaceListingWriter interface {
	WriteMarketplaceListing(context.Context, Account, Runtime, MarketplaceListingWriteRequest) (MarketplaceListingWriteReceipt, error)
}

// MarketplaceListingWriteRequest contains no provider payload, URL or secret.
type MarketplaceListingWriteRequest struct {
	Draft             marketplacelisting.ListingDraft `json:"draft"`
	IdempotencyKey    string                          `json:"idempotency_key"`
	DryRun            bool                            `json:"dry_run"`
	ApprovalRequestID string                          `json:"approval_request_id,omitempty"`
}

func (request MarketplaceListingWriteRequest) Validate() error {
	if request.Draft.Validate() != nil || !validIdempotencyKey(request.IdempotencyKey) || request.ApprovalRequestID != "" && !validReference(request.ApprovalRequestID) || !request.DryRun && request.ApprovalRequestID == "" {
		return ErrInvalidMarketplaceListing
	}
	return nil
}

// MarketplaceListingWriteReceipt is the bounded normalized result of a
// remote listing write.
type MarketplaceListingWriteReceipt struct {
	Status            PublicationResultStatus `json:"status"`
	RemoteID          string                  `json:"remote_id,omitempty"`
	RemoteOperationID string                  `json:"remote_operation_id,omitempty"`
	ErrorCode         string                  `json:"error_code,omitempty"`
	ObservedAt        time.Time               `json:"observed_at"`
}

func (receipt MarketplaceListingWriteReceipt) Validate() error {
	if !receipt.Status.Valid() || receipt.ObservedAt.IsZero() || receipt.ObservedAt.Location() != time.UTC {
		return ErrInvalidMarketplaceListing
	}
	if receipt.RemoteID != "" && !validRemoteID(receipt.RemoteID) || receipt.RemoteOperationID != "" && !validRemoteID(receipt.RemoteOperationID) || receipt.ErrorCode != "" && !safeCodePattern.MatchString(receipt.ErrorCode) {
		return ErrInvalidMarketplaceListing
	}
	if receipt.Status == PublicationRejected && receipt.ErrorCode == "" {
		return ErrInvalidMarketplaceListing
	}
	return nil
}

// MarketplaceListingStatusReader resolves accepted, processing and unknown
// writes using only normalized identifiers.
type MarketplaceListingStatusReader interface {
	ReadMarketplaceListingStatus(context.Context, Account, Runtime, MarketplaceListingStatusQuery) (MarketplaceListingObservation, error)
}

// MarketplaceListingStatusQuery is safe to retry after a timeout.
type MarketplaceListingStatusQuery struct {
	RemoteID          string `json:"remote_id,omitempty"`
	RemoteOperationID string `json:"remote_operation_id,omitempty"`
	IdempotencyKey    string `json:"idempotency_key"`
}

func (query MarketplaceListingStatusQuery) Validate() error {
	if query.RemoteID == "" && query.RemoteOperationID == "" || query.RemoteID != "" && !validRemoteID(query.RemoteID) || query.RemoteOperationID != "" && !validRemoteID(query.RemoteOperationID) || !validIdempotencyKey(query.IdempotencyKey) {
		return ErrInvalidMarketplaceListing
	}
	return nil
}

// MarketplaceListingObservation is the normalized read-after-write result.
type MarketplaceListingObservation struct {
	Observation marketplacelisting.RemoteObservation `json:"observation"`
}

func (observation MarketplaceListingObservation) Validate() error {
	if observation.Observation.RemoteID == "" && observation.Observation.RemoteOperationID == "" || observation.Observation.RemoteID != "" && !validRemoteID(observation.Observation.RemoteID) || observation.Observation.RemoteOperationID != "" && !validRemoteID(observation.Observation.RemoteOperationID) || observation.Observation.Status == "" || observation.Observation.ObservedAt.IsZero() || observation.Observation.ObservedAt.Location() != time.UTC || observation.Observation.SnapshotDigest != "" && !validDigest(observation.Observation.SnapshotDigest) {
		return ErrInvalidMarketplaceListing
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
