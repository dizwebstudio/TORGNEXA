package connectors

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"time"
)

var ErrInvalidGovernmentRequest = errors.New("connectors: invalid government request")
var governmentRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

type GovernmentReferenceRequest struct{ Kind, Key string }
type GovernmentReference struct {
	Kind, Key, RemoteID, Status string
	Attributes                  map[string]string
	ObservedAt                  time.Time
}

func (r GovernmentReferenceRequest) Validate() error {
	if !safeCodePattern.MatchString(r.Kind) || !governmentRefPattern.MatchString(r.Key) {
		return ErrInvalidGovernmentRequest
	}
	return nil
}
func (r GovernmentReference) Validate() error {
	if !safeCodePattern.MatchString(r.Kind) || !governmentRefPattern.MatchString(r.Key) || !governmentRefPattern.MatchString(r.RemoteID) || !safeCodePattern.MatchString(r.Status) || r.ObservedAt.IsZero() || r.ObservedAt.Location() != time.UTC {
		return ErrInvalidGovernmentRequest
	}
	for k, v := range r.Attributes {
		if !safeCodePattern.MatchString(k) || len(v) > 512 {
			return ErrInvalidGovernmentRequest
		}
	}
	return nil
}

type MarkingStatusRequest struct{ Codes []string }
type MarkingCodeStatus struct {
	CodeFingerprint, Status, ProductGTIN string
	ObservedAt                           time.Time
}
type MarkingStatusObservation struct {
	Items           []MarkingCodeStatus
	RemoteRequestID string
}

func (r MarkingStatusRequest) Validate(max int) error {
	if max < 1 || len(r.Codes) < 1 || len(r.Codes) > max {
		return ErrInvalidGovernmentRequest
	}
	seen := map[string]struct{}{}
	for _, c := range r.Codes {
		if len(c) < 6 || len(c) > 256 {
			return ErrInvalidGovernmentRequest
		}
		if _, ok := seen[c]; ok {
			return ErrInvalidGovernmentRequest
		}
		seen[c] = struct{}{}
	}
	return nil
}
func (o MarkingStatusObservation) Validate() error {
	if len(o.Items) == 0 {
		return ErrInvalidGovernmentRequest
	}
	for _, i := range o.Items {
		if len(i.CodeFingerprint) != 64 || !safeCodePattern.MatchString(i.Status) || i.ObservedAt.IsZero() || i.ObservedAt.Location() != time.UTC || (i.ProductGTIN != "" && !governmentRefPattern.MatchString(i.ProductGTIN)) {
			return ErrInvalidGovernmentRequest
		}
	}
	return nil
}

type GovernmentDocumentRequest struct{ RemoteID string }
type GovernmentDocument struct {
	RemoteID, Kind, Status, StockRef string
	EffectiveAt, ObservedAt          time.Time
}

func (r GovernmentDocumentRequest) Validate() error {
	if !governmentRefPattern.MatchString(r.RemoteID) {
		return ErrInvalidGovernmentRequest
	}
	return nil
}
func (d GovernmentDocument) Validate() error {
	if !governmentRefPattern.MatchString(d.RemoteID) || !safeCodePattern.MatchString(d.Kind) || !safeCodePattern.MatchString(d.Status) || d.ObservedAt.IsZero() || d.ObservedAt.Location() != time.UTC {
		return ErrInvalidGovernmentRequest
	}
	return nil
}

type GovernmentWriteRequest struct {
	Kind, ExternalID, ArtifactRef, ApprovalRef, IdempotencyKey string
	Metadata                                                   map[string]string
}
type GovernmentWriteResult struct {
	RemoteID, Status string
	AcceptedAt       time.Time
}

func (r GovernmentWriteRequest) Validate() error {
	if !safeCodePattern.MatchString(r.Kind) || !governmentRefPattern.MatchString(r.ExternalID) || !governmentRefPattern.MatchString(r.ArtifactRef) || !governmentRefPattern.MatchString(r.ApprovalRef) || !governmentRefPattern.MatchString(r.IdempotencyKey) {
		return ErrInvalidGovernmentRequest
	}
	for k, v := range r.Metadata {
		if !safeCodePattern.MatchString(k) || len(v) > 512 {
			return ErrInvalidGovernmentRequest
		}
	}
	return nil
}
func (r GovernmentWriteResult) Validate() error {
	if !governmentRefPattern.MatchString(r.RemoteID) || !safeCodePattern.MatchString(r.Status) || r.AcceptedAt.IsZero() || r.AcceptedAt.Location() != time.UTC {
		return ErrInvalidGovernmentRequest
	}
	return nil
}

type GovernmentInventoryRequest struct{ LocationRef string }
type GovernmentStockItem struct {
	ProductRef, LotRef string
	Quantity           string
	UpdatedAt          time.Time
}
type GovernmentInventoryObservation struct {
	Items      []GovernmentStockItem
	ObservedAt time.Time
}

func (r GovernmentInventoryRequest) Validate() error {
	if r.LocationRef != "" && !governmentRefPattern.MatchString(r.LocationRef) {
		return ErrInvalidGovernmentRequest
	}
	return nil
}
func (o GovernmentInventoryObservation) Validate() error {
	if o.ObservedAt.IsZero() || o.ObservedAt.Location() != time.UTC {
		return ErrInvalidGovernmentRequest
	}
	for _, i := range o.Items {
		if !governmentRefPattern.MatchString(i.ProductRef) || len(i.Quantity) == 0 || len(i.Quantity) > 64 {
			return ErrInvalidGovernmentRequest
		}
	}
	return nil
}

type GovernmentReconciliationRequest struct{ RemoteIDs []string }
type GovernmentReconciliationItem struct {
	RemoteID, RemoteStatus string
	ObservedAt             time.Time
}
type GovernmentReconciliationResult struct {
	Items []GovernmentReconciliationItem
}

func (r GovernmentReconciliationRequest) Validate(max int) error {
	if len(r.RemoteIDs) < 1 || len(r.RemoteIDs) > max {
		return ErrInvalidGovernmentRequest
	}
	ids := append([]string(nil), r.RemoteIDs...)
	sort.Strings(ids)
	for i, id := range ids {
		if !governmentRefPattern.MatchString(id) || (i > 0 && ids[i-1] == id) {
			return ErrInvalidGovernmentRequest
		}
	}
	return nil
}

type GovernmentReferenceReader interface {
	ReadGovernmentReference(context.Context, Account, Runtime, GovernmentReferenceRequest) (GovernmentReference, error)
}
type MarkingStatusReader interface {
	ReadMarkingStatus(context.Context, Account, Runtime, MarkingStatusRequest) (MarkingStatusObservation, error)
}
type GovernmentDocumentReader interface {
	ReadGovernmentDocument(context.Context, Account, Runtime, GovernmentDocumentRequest) (GovernmentDocument, error)
}
type GovernmentDocumentWriter interface {
	WriteGovernmentDocument(context.Context, Account, Runtime, GovernmentWriteRequest) (GovernmentWriteResult, error)
}
type GovernmentInventoryReader interface {
	ReadGovernmentInventory(context.Context, Account, Runtime, GovernmentInventoryRequest) (GovernmentInventoryObservation, error)
}
type GovernmentReconciler interface {
	ReconcileGovernment(context.Context, Account, Runtime, GovernmentReconciliationRequest) (GovernmentReconciliationResult, error)
}
