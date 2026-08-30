package connectors

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidMarkingOperation = errors.New("connectors: invalid marking operation")
var markingOperationRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var markingGTINPattern = regexp.MustCompile(`^[0-9]{8,14}$`)
var hexDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// MarkingOperationRequest is the common safe envelope for regulated writes.
// ArtifactRef points to a host-controlled short-lived payload; raw codes are
// deliberately absent from every SDK request type.
type MarkingOperationRequest struct {
	ExternalID     string            `json:"external_id"`
	ArtifactRef    string            `json:"artifact_ref,omitempty"`
	ApprovalRef    string            `json:"approval_ref,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	DryRun         bool              `json:"dry_run"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

func (r MarkingOperationRequest) validate() error {
	for _, value := range []string{r.ExternalID, r.IdempotencyKey} {
		if !markingOperationRefPattern.MatchString(value) {
			return ErrInvalidMarkingOperation
		}
	}
	for _, value := range []string{r.ArtifactRef, r.ApprovalRef} {
		if value != "" && !markingOperationRefPattern.MatchString(value) {
			return ErrInvalidMarkingOperation
		}
	}
	if !r.DryRun && r.ApprovalRef == "" {
		return ErrInvalidMarkingOperation
	}
	for key, value := range r.Metadata {
		if !safeCodePattern.MatchString(key) || len(value) > 512 {
			return ErrInvalidMarkingOperation
		}
	}
	return nil
}

type MarkingOperationStatus string

const (
	MarkingAccepted MarkingOperationStatus = "accepted"
	MarkingPartial  MarkingOperationStatus = "partial"
	MarkingRejected MarkingOperationStatus = "rejected"
	MarkingUnknown  MarkingOperationStatus = "unknown"
	MarkingDryRun   MarkingOperationStatus = "dry_run"
)

func (s MarkingOperationStatus) Valid() bool {
	return s == MarkingAccepted || s == MarkingPartial || s == MarkingRejected || s == MarkingUnknown || s == MarkingDryRun
}

// MarkingOperationReceipt is the only result shape returned by marking writes.
// Unknown means the remote may have accepted the request and requires a read
// or reconciliation before a retry.
type MarkingOperationReceipt struct {
	RemoteID        string                 `json:"remote_id,omitempty"`
	Status          MarkingOperationStatus `json:"status"`
	Requested       int64                  `json:"requested,omitempty"`
	Accepted        int64                  `json:"accepted,omitempty"`
	RemoteRequestID string                 `json:"remote_request_id,omitempty"`
	ObservedAt      time.Time              `json:"observed_at"`
}

func (r MarkingOperationReceipt) Validate() error {
	if !r.Status.Valid() || r.Requested < 0 || r.Accepted < 0 || r.Accepted > r.Requested || r.ObservedAt.IsZero() || r.ObservedAt.Location() != time.UTC {
		return ErrInvalidMarkingOperation
	}
	if r.RemoteID != "" && !markingOperationRefPattern.MatchString(r.RemoteID) || r.RemoteRequestID != "" && !markingOperationRefPattern.MatchString(r.RemoteRequestID) {
		return ErrInvalidMarkingOperation
	}
	return nil
}

type MarkingCodesRequest struct {
	MarkingOperationRequest
	GTIN         string `json:"gtin"`
	ProductRef   string `json:"product_ref"`
	ProductGroup string `json:"product_group"`
	Quantity     int64  `json:"quantity"`
}

func (r MarkingCodesRequest) Validate() error {
	if r.MarkingOperationRequest.validate() != nil || !markingGTINPattern.MatchString(r.GTIN) || !markingOperationRefPattern.MatchString(r.ProductRef) || !safeCodePattern.MatchString(r.ProductGroup) || r.Quantity < 1 || r.Quantity > 1000000000 {
		return ErrInvalidMarkingOperation
	}
	return nil
}

type MarkingCodesRequester interface {
	RequestMarkingCodes(context.Context, Account, Runtime, MarkingCodesRequest) (MarkingOperationReceipt, error)
}

type MarkingCodesReserveRequest struct {
	MarkingOperationRequest
	BatchRef string `json:"batch_ref"`
	Quantity int64  `json:"quantity"`
	Cancel   bool   `json:"cancel"`
}

func (r MarkingCodesReserveRequest) Validate() error {
	if r.MarkingOperationRequest.validate() != nil || !markingOperationRefPattern.MatchString(r.BatchRef) || r.Quantity < 1 || r.Quantity > 1000000000 {
		return ErrInvalidMarkingOperation
	}
	return nil
}

type MarkingCodesReserver interface {
	ReserveMarkingCodes(context.Context, Account, Runtime, MarkingCodesReserveRequest) (MarkingOperationReceipt, error)
}

type MarkingAggregationRequest struct {
	MarkingOperationRequest
	PackageRef        string   `json:"package_ref"`
	ParentPackageRef  string   `json:"parent_package_ref,omitempty"`
	ChildFingerprints []string `json:"child_fingerprints"`
	Close             bool     `json:"close"`
	Dissolve          bool     `json:"dissolve"`
}

func (r MarkingAggregationRequest) Validate() error {
	if r.MarkingOperationRequest.validate() != nil || !markingOperationRefPattern.MatchString(r.PackageRef) || len(r.ChildFingerprints) == 0 || len(r.ChildFingerprints) > 100000 {
		return ErrInvalidMarkingOperation
	}
	if r.ParentPackageRef != "" && !markingOperationRefPattern.MatchString(r.ParentPackageRef) {
		return ErrInvalidMarkingOperation
	}
	seen := make(map[string]struct{}, len(r.ChildFingerprints))
	for _, fingerprint := range r.ChildFingerprints {
		if len(fingerprint) != 64 || !hexDigestPattern.MatchString(fingerprint) {
			return ErrInvalidMarkingOperation
		}
		if _, exists := seen[fingerprint]; exists {
			return ErrInvalidMarkingOperation
		}
		seen[fingerprint] = struct{}{}
	}
	return nil
}

type MarkingAggregationWriter interface {
	WriteMarkingAggregation(context.Context, Account, Runtime, MarkingAggregationRequest) (MarkingOperationReceipt, error)
}

type MarkingCirculationKind string

const (
	CirculationProduction MarkingCirculationKind = "production"
	CirculationImport     MarkingCirculationKind = "import"
	CirculationSale       MarkingCirculationKind = "sale"
	CirculationWriteOff   MarkingCirculationKind = "writeoff"
	CirculationReturn     MarkingCirculationKind = "return"
)

func (k MarkingCirculationKind) Valid() bool {
	return k == CirculationProduction || k == CirculationImport || k == CirculationSale || k == CirculationWriteOff || k == CirculationReturn
}

type MarkingCirculationRequest struct {
	MarkingOperationRequest
	Kind             MarkingCirculationKind `json:"kind"`
	DocumentRef      string                 `json:"document_ref"`
	CodeFingerprints []string               `json:"code_fingerprints"`
	LocationRef      string                 `json:"location_ref,omitempty"`
}

func (r MarkingCirculationRequest) Validate() error {
	if r.MarkingOperationRequest.validate() != nil || !r.Kind.Valid() || !markingOperationRefPattern.MatchString(r.DocumentRef) || len(r.CodeFingerprints) == 0 || len(r.CodeFingerprints) > 100000 {
		return ErrInvalidMarkingOperation
	}
	if r.LocationRef != "" && !markingOperationRefPattern.MatchString(r.LocationRef) {
		return ErrInvalidMarkingOperation
	}
	for _, fingerprint := range r.CodeFingerprints {
		if len(fingerprint) != 64 || !hexDigestPattern.MatchString(fingerprint) {
			return ErrInvalidMarkingOperation
		}
	}
	return nil
}

type MarkingCirculationWriter interface {
	IntroduceMarking(context.Context, Account, Runtime, MarkingCirculationRequest) (MarkingOperationReceipt, error)
	WithdrawMarking(context.Context, Account, Runtime, MarkingCirculationRequest) (MarkingOperationReceipt, error)
}

type MarkingTransferRequest struct {
	MarkingOperationRequest
	DocumentRef      string   `json:"document_ref"`
	FromLocationRef  string   `json:"from_location_ref"`
	ToLocationRef    string   `json:"to_location_ref"`
	CodeFingerprints []string `json:"code_fingerprints"`
}

func (r MarkingTransferRequest) Validate() error {
	if r.MarkingOperationRequest.validate() != nil || !markingOperationRefPattern.MatchString(r.DocumentRef) || !markingOperationRefPattern.MatchString(r.FromLocationRef) || !markingOperationRefPattern.MatchString(r.ToLocationRef) || r.FromLocationRef == r.ToLocationRef || len(r.CodeFingerprints) == 0 || len(r.CodeFingerprints) > 100000 {
		return ErrInvalidMarkingOperation
	}
	for _, fingerprint := range r.CodeFingerprints {
		if len(fingerprint) != 64 || !hexDigestPattern.MatchString(fingerprint) {
			return ErrInvalidMarkingOperation
		}
	}
	return nil
}

type MarkingTransferWriter interface {
	WriteMarkingTransfer(context.Context, Account, Runtime, MarkingTransferRequest) (MarkingOperationReceipt, error)
}

// MarkingStatusReader is the existing read surface. It is kept separate from
// write interfaces so an account cannot accidentally gain regulated writes.
type MarkingStatusReaderV2 interface {
	ReadMarkingOperationStatus(context.Context, Account, Runtime, string) (MarkingOperationReceipt, error)
}
