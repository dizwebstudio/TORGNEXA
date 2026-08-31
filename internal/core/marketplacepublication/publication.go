// Package marketplacepublication contains the provider-neutral product
// publication model. A connector receives a versioned snapshot, never a
// mutable catalog object or an arbitrary provider payload.
package marketplacepublication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid      = errors.New("marketplace publication: invalid value")
	ErrConflict     = errors.New("marketplace publication: conflict")
	ErrInvalidState = errors.New("marketplace publication: invalid state transition")
	ErrCycle        = errors.New("marketplace publication: packaging cycle")
)

var (
	refPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
	codePattern     = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	localePattern   = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Z][a-z]{3})?(?:-[A-Z]{2})?$`)
	countryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// OperationKind is the typed publication operation vocabulary.
type OperationKind string

const (
	OperationCreateProduct    OperationKind = "create_product"
	OperationUpdateProduct    OperationKind = "update_product"
	OperationUpdateVariant    OperationKind = "update_variant"
	OperationUpdateAttributes OperationKind = "update_attributes"
	OperationUpdateMedia      OperationKind = "update_media"
	OperationArchive          OperationKind = "archive"
	OperationUnarchive        OperationKind = "unarchive"
	OperationPublish          OperationKind = "publish"
	OperationUnpublish        OperationKind = "unpublish"
	OperationStatusRead       OperationKind = "status_read"
)

func (kind OperationKind) Valid() bool {
	switch kind {
	case OperationCreateProduct, OperationUpdateProduct, OperationUpdateVariant,
		OperationUpdateAttributes, OperationUpdateMedia, OperationArchive,
		OperationUnarchive, OperationPublish, OperationUnpublish, OperationStatusRead:
		return true
	default:
		return false
	}
}

// State is the durable local operation lifecycle.
type State string

const (
	StateDraft          State = "draft"
	StatePreflight      State = "preflight"
	StateQueued         State = "queued"
	StateSending        State = "sending"
	StateAccepted       State = "accepted"
	StateProcessing     State = "processing"
	StatePublished      State = "published"
	StateRejected       State = "rejected"
	StateUnknown        State = "unknown"
	StateNeedsAttention State = "needs_attention"
	StateCancelled      State = "cancelled"
)

func (state State) Valid() bool {
	switch state {
	case StateDraft, StatePreflight, StateQueued, StateSending, StateAccepted,
		StateProcessing, StatePublished, StateRejected, StateUnknown,
		StateNeedsAttention, StateCancelled:
		return true
	default:
		return false
	}
}

// ModerationStatus is a normalized remote moderation projection.
type ModerationStatus string

const (
	ModerationUnknown  ModerationStatus = "unknown"
	ModerationPending  ModerationStatus = "pending"
	ModerationApproved ModerationStatus = "approved"
	ModerationRejected ModerationStatus = "rejected"
)

func (status ModerationStatus) Valid() bool {
	return status == ModerationUnknown || status == ModerationPending || status == ModerationApproved || status == ModerationRejected
}

// Target binds a snapshot to one tenant and connector account.
type Target struct {
	OrganizationID     string `json:"organization_id"`
	WorkspaceID        string `json:"workspace_id"`
	ProductID          string `json:"product_id"`
	OfferID            string `json:"offer_id,omitempty"`
	ConnectorAccountID string `json:"connector_account_id"`
	ConnectorID        string `json:"connector_id"`
	Locale             string `json:"locale"`
	Jurisdiction       string `json:"jurisdiction"`
}

func (target Target) Validate() error {
	for index, value := range []string{target.OrganizationID, target.WorkspaceID, target.ProductID, target.OfferID, target.ConnectorAccountID, target.ConnectorID} {
		if index == 3 && value == "" {
			continue
		}
		if !refPattern.MatchString(value) {
			return ErrInvalid
		}
	}
	if !localePattern.MatchString(target.Locale) || !countryPattern.MatchString(target.Jurisdiction) {
		return ErrInvalid
	}
	return nil
}

// Dimension stores integer millimetres and grams to avoid provider-dependent
// floating-point conversions in the core contract.
type Dimension struct {
	LengthMM int64 `json:"length_mm"`
	WidthMM  int64 `json:"width_mm"`
	HeightMM int64 `json:"height_mm"`
	WeightG  int64 `json:"weight_g"`
}

func (dimension Dimension) Validate() error {
	if dimension.LengthMM < 0 || dimension.WidthMM < 0 || dimension.HeightMM < 0 || dimension.WeightG < 0 || dimension.LengthMM > 10_000_000 || dimension.WidthMM > 10_000_000 || dimension.HeightMM > 10_000_000 || dimension.WeightG > 10_000_000_000 {
		return ErrInvalid
	}
	return nil
}

// Attribute is a normalized category attribute. Connector-specific names and
// conversions are kept at the adapter edge.
type Attribute struct {
	Code   string `json:"code"`
	Value  string `json:"value"`
	Unit   string `json:"unit,omitempty"`
	Locale string `json:"locale,omitempty"`
}

func (attribute Attribute) Validate() error {
	if !codePattern.MatchString(attribute.Code) || strings.TrimSpace(attribute.Value) != attribute.Value || attribute.Value == "" || !utf8.ValidString(attribute.Value) || utf8.RuneCountInString(attribute.Value) > 2000 || strings.ContainsAny(attribute.Value, "\x00\r\n") {
		return ErrInvalid
	}
	if attribute.Unit != "" && !codePattern.MatchString(attribute.Unit) {
		return ErrInvalid
	}
	if attribute.Locale != "" && !localePattern.MatchString(attribute.Locale) {
		return ErrInvalid
	}
	return nil
}

// MediaAsset contains only released-object metadata. URL, bucket and storage
// key fields are intentionally absent; adapters may use a host-owned media
// bridge but cannot fetch arbitrary client-controlled URLs.
type MediaAsset struct {
	ID                string `json:"id"`
	ReleasedObjectRef string `json:"released_object_ref"`
	Digest            string `json:"digest"`
	Format            string `json:"format"`
	Bytes             int64  `json:"bytes"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Position          int    `json:"position"`
}

func (media MediaAsset) Validate() error {
	if !refPattern.MatchString(media.ID) || !strings.HasPrefix(media.ReleasedObjectRef, "upl_") || !refPattern.MatchString(media.ReleasedObjectRef) || !digestPattern.MatchString(media.Digest) || media.Format == "" || len(media.Format) > 64 || media.Bytes < 1 || media.Bytes > 100*1024*1024 || media.Width < 0 || media.Height < 0 || media.Position < 0 || media.Position > 1000 {
		return ErrInvalid
	}
	return nil
}

// Variant describes one sellable SKU within a publication snapshot.
type Variant struct {
	ID         string      `json:"id"`
	SKU        string      `json:"sku"`
	GTIN       string      `json:"gtin,omitempty"`
	Barcodes   []string    `json:"barcodes,omitempty"`
	Attributes []Attribute `json:"attributes,omitempty"`
	Dimension  Dimension   `json:"dimension"`
}

func (variant Variant) Validate() error {
	if !refPattern.MatchString(variant.ID) || !validText(variant.SKU, 200) || len(variant.GTIN) > 32 || len(variant.Barcodes) > 100 || variant.Dimension.Validate() != nil {
		return ErrInvalid
	}
	for _, barcode := range variant.Barcodes {
		if !validText(barcode, 200) {
			return ErrInvalid
		}
	}
	if validateAttributes(variant.Attributes) != nil {
		return ErrInvalid
	}
	return nil
}

// Snapshot is the immutable publication input assembled from catalog, PIM,
// price, media, compliance and mapping projections.
type Snapshot struct {
	ID                string       `json:"id"`
	Target            Target       `json:"target"`
	Version           int64        `json:"version"`
	SKU               string       `json:"sku"`
	GTIN              string       `json:"gtin,omitempty"`
	Title             string       `json:"title"`
	Description       string       `json:"description,omitempty"`
	Brand             string       `json:"brand,omitempty"`
	CategoryCode      string       `json:"category_code"`
	Attributes        []Attribute  `json:"attributes,omitempty"`
	Variants          []Variant    `json:"variants,omitempty"`
	Media             []MediaAsset `json:"media,omitempty"`
	Dimension         Dimension    `json:"dimension"`
	PriceMinor        int64        `json:"price_minor"`
	Currency          string       `json:"currency"`
	VAT               string       `json:"vat,omitempty"`
	ProductStatus     string       `json:"product_status"`
	CatalogVersion    int64        `json:"catalog_version"`
	PIMVersion        int64        `json:"pim_version"`
	PriceVersion      int64        `json:"price_version"`
	MediaVersion      int64        `json:"media_version"`
	MappingVersion    int64        `json:"mapping_version"`
	CapabilityVersion int64        `json:"capability_version"`
	ComplianceDigest  string       `json:"compliance_digest,omitempty"`
	AssembledAt       time.Time    `json:"assembled_at"`
	Digest            string       `json:"digest"`
}

func (snapshot Snapshot) Validate() error {
	if !refPattern.MatchString(snapshot.ID) || snapshot.Target.Validate() != nil || snapshot.Version < 1 || !validText(snapshot.SKU, 200) || len(snapshot.GTIN) > 32 || !validText(snapshot.Title, 500) || !validOptionalText(snapshot.Description, 10000) || !validOptionalText(snapshot.Brand, 300) || !validText(snapshot.CategoryCode, 192) || snapshot.Dimension.Validate() != nil || snapshot.PriceMinor < 0 || !currencyPattern.MatchString(snapshot.Currency) || snapshot.ProductStatus != "draft" && snapshot.ProductStatus != "active" && snapshot.ProductStatus != "archived" || snapshot.CatalogVersion < 1 || snapshot.PIMVersion < 0 || snapshot.PriceVersion < 0 || snapshot.MediaVersion < 0 || snapshot.MappingVersion < 0 || snapshot.CapabilityVersion < 0 || !isUTC(snapshot.AssembledAt) || (snapshot.ComplianceDigest != "" && !digestPattern.MatchString(snapshot.ComplianceDigest)) {
		return ErrInvalid
	}
	if validateAttributes(snapshot.Attributes) != nil || len(snapshot.Attributes) > 256 || len(snapshot.Variants) > 100 || len(snapshot.Media) > 64 {
		return ErrInvalid
	}
	for _, variant := range snapshot.Variants {
		if variant.Validate() != nil {
			return ErrInvalid
		}
	}
	seenMedia := make(map[string]struct{}, len(snapshot.Media))
	for _, media := range snapshot.Media {
		if media.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seenMedia[media.ID]; ok {
			return ErrConflict
		}
		seenMedia[media.ID] = struct{}{}
	}
	if snapshot.Digest != "" && !digestPattern.MatchString(snapshot.Digest) {
		return ErrInvalid
	}
	return nil
}

// ComputeDigest returns the stable content fingerprint of a snapshot. Digest
// itself is excluded so a caller can validate a persisted snapshot after load.
func (snapshot Snapshot) ComputeDigest() (string, error) {
	copySnapshot := snapshot
	copySnapshot.Digest = ""
	data, err := json.Marshal(copySnapshot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// Operation is the durable state machine item handled by the publication
// worker. Remote IDs are opaque and never used as local identity.
type Operation struct {
	ID                string        `json:"id"`
	Target            Target        `json:"target"`
	SnapshotID        string        `json:"snapshot_id"`
	SnapshotDigest    string        `json:"snapshot_digest"`
	Kind              OperationKind `json:"kind"`
	State             State         `json:"state"`
	IdempotencyKey    string        `json:"idempotency_key"`
	RemoteID          string        `json:"remote_id,omitempty"`
	RemoteOperationID string        `json:"remote_operation_id,omitempty"`
	Attempt           int           `json:"attempt"`
	DryRun            bool          `json:"dry_run"`
	ApprovalRef       string        `json:"approval_ref,omitempty"`
	QualityReceiptRef string        `json:"quality_receipt_ref"`
	ErrorCode         string        `json:"error_code,omitempty"`
	Version           int64         `json:"version"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

func (operation Operation) Validate() error {
	if !refPattern.MatchString(operation.ID) || operation.Target.Validate() != nil || !refPattern.MatchString(operation.SnapshotID) || !digestPattern.MatchString(operation.SnapshotDigest) || !operation.Kind.Valid() || !operation.State.Valid() || !validIdempotencyKey(operation.IdempotencyKey) || operation.Attempt < 0 || operation.Attempt > 100 || !refPattern.MatchString(operation.QualityReceiptRef) || (operation.ApprovalRef != "" && !refPattern.MatchString(operation.ApprovalRef)) || (operation.RemoteID != "" && !refPattern.MatchString(operation.RemoteID)) || (operation.RemoteOperationID != "" && !refPattern.MatchString(operation.RemoteOperationID)) || (operation.ErrorCode != "" && !codePattern.MatchString(operation.ErrorCode)) || operation.Version < 1 || !isUTC(operation.CreatedAt) || !isUTC(operation.UpdatedAt) || operation.UpdatedAt.Before(operation.CreatedAt) {
		return ErrInvalid
	}
	return nil
}

// CanTransition checks the worker lifecycle. Unknown is deliberately not
// treated as success: reconciliation must resolve it before publication.
func CanTransition(from, to State) bool {
	if !from.Valid() || !to.Valid() || from == to {
		return from == to && from.Valid()
	}
	switch from {
	case StateDraft:
		return to == StatePreflight || to == StateCancelled
	case StatePreflight:
		return to == StateQueued || to == StateRejected || to == StateNeedsAttention || to == StateCancelled
	case StateQueued:
		return to == StateSending || to == StateCancelled
	case StateSending:
		return to == StateAccepted || to == StateProcessing || to == StatePublished || to == StateRejected || to == StateUnknown || to == StateNeedsAttention
	case StateAccepted:
		return to == StateProcessing || to == StatePublished || to == StateRejected || to == StateUnknown
	case StateProcessing:
		return to == StatePublished || to == StateRejected || to == StateUnknown || to == StateNeedsAttention
	case StateUnknown:
		return to == StateProcessing || to == StatePublished || to == StateRejected || to == StateNeedsAttention || to == StateCancelled
	case StateNeedsAttention:
		return to == StateQueued || to == StateCancelled
	}
	return false
}

// RemoteObservation is a safe normalized read-after-write/reconciliation view.
type RemoteObservation struct {
	RemoteID          string           `json:"remote_id"`
	RemoteOperationID string           `json:"remote_operation_id,omitempty"`
	State             State            `json:"state"`
	Moderation        ModerationStatus `json:"moderation"`
	SnapshotDigest    string           `json:"snapshot_digest,omitempty"`
	ObservedAt        time.Time        `json:"observed_at"`
}

func (observation RemoteObservation) Validate() error {
	if !refPattern.MatchString(observation.RemoteID) || (observation.RemoteOperationID != "" && !refPattern.MatchString(observation.RemoteOperationID)) || !observation.State.Valid() || !observation.Moderation.Valid() || (observation.SnapshotDigest != "" && !digestPattern.MatchString(observation.SnapshotDigest)) || !isUTC(observation.ObservedAt) {
		return ErrInvalid
	}
	return nil
}

// DriftType is the bounded reconciliation vocabulary.
type DriftType string

const (
	DriftMissingRemote       DriftType = "missing_remote_product"
	DriftDuplicateRemote     DriftType = "duplicate_remote_product"
	DriftContentMismatch     DriftType = "content_mismatch"
	DriftAttributeMismatch   DriftType = "attribute_mismatch"
	DriftMediaMismatch       DriftType = "media_mismatch"
	DriftMappingConflict     DriftType = "mapping_conflict"
	DriftModerationRejected  DriftType = "moderation_rejected"
	DriftPublicationMismatch DriftType = "publication_status_mismatch"
	DriftUnknownWriteOutcome DriftType = "unknown_write_outcome"
)

func (drift DriftType) Valid() bool {
	switch drift {
	case DriftMissingRemote, DriftDuplicateRemote, DriftContentMismatch, DriftAttributeMismatch, DriftMediaMismatch, DriftMappingConflict, DriftModerationRejected, DriftPublicationMismatch, DriftUnknownWriteOutcome:
		return true
	default:
		return false
	}
}

// Drift is a redacted reconciliation result, suitable for audit and UI.
type Drift struct {
	Type           DriftType `json:"type"`
	SnapshotID     string    `json:"snapshot_id"`
	RemoteID       string    `json:"remote_id,omitempty"`
	ExpectedDigest string    `json:"expected_digest,omitempty"`
	ObservedDigest string    `json:"observed_digest,omitempty"`
	ObservedState  State     `json:"observed_state,omitempty"`
	DetectedAt     time.Time `json:"detected_at"`
}

func (drift Drift) Validate() error {
	if !drift.Type.Valid() || !refPattern.MatchString(drift.SnapshotID) || (drift.RemoteID != "" && !refPattern.MatchString(drift.RemoteID)) || (drift.ExpectedDigest != "" && !digestPattern.MatchString(drift.ExpectedDigest)) || (drift.ObservedDigest != "" && !digestPattern.MatchString(drift.ObservedDigest)) || (drift.ObservedState != "" && !drift.ObservedState.Valid()) || !isUTC(drift.DetectedAt) {
		return ErrInvalid
	}
	return nil
}

// Reconcile classifies remote state without mutating either side.
func Reconcile(snapshot Snapshot, observation RemoteObservation, mappingRemoteID string) ([]Drift, error) {
	if snapshot.Validate() != nil || observation.Validate() != nil {
		return nil, ErrInvalid
	}
	digest, err := snapshot.ComputeDigest()
	if err != nil {
		return nil, ErrInvalid
	}
	if snapshot.Digest != "" && snapshot.Digest != digest {
		return nil, ErrConflict
	}
	result := make([]Drift, 0, 3)
	add := func(kind DriftType) {
		result = append(result, Drift{Type: kind, SnapshotID: snapshot.ID, RemoteID: observation.RemoteID, ExpectedDigest: digest, ObservedDigest: observation.SnapshotDigest, ObservedState: observation.State, DetectedAt: observation.ObservedAt})
	}
	if mappingRemoteID != "" && mappingRemoteID != observation.RemoteID {
		add(DriftMappingConflict)
	}
	if observation.State == StateUnknown {
		add(DriftUnknownWriteOutcome)
	}
	if observation.SnapshotDigest != "" && observation.SnapshotDigest != digest {
		add(DriftContentMismatch)
	}
	if observation.Moderation == ModerationRejected {
		add(DriftModerationRejected)
	}
	if observation.State != StatePublished && observation.State != StateProcessing && observation.State != StateAccepted {
		add(DriftPublicationMismatch)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result, nil
}

func validateAttributes(attributes []Attribute) error {
	seen := make(map[string]struct{}, len(attributes))
	for _, attribute := range attributes {
		if attribute.Validate() != nil {
			return ErrInvalid
		}
		key := attribute.Code + "\x00" + attribute.Locale
		if _, ok := seen[key]; ok {
			return ErrConflict
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validText(value string, max int) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}

func validOptionalText(value string, max int) bool {
	return value == "" || validText(value, max)
}

func validIdempotencyKey(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 128 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n\t")
}

func isUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
