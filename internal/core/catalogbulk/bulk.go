// Package catalogbulk defines the provider-neutral multi-channel catalog bulk
// workspace. It stores drafts and evidence, while Product/PIM/Offer remain the
// canonical sources of truth.
package catalogbulk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
)

var (
	ErrInvalid         = errors.New("catalog bulk: invalid value")
	ErrConflict        = errors.New("catalog bulk: conflict")
	ErrTooLarge        = errors.New("catalog bulk: selection exceeds limit")
	ErrStale           = errors.New("catalog bulk: preview is stale")
	ErrNotQualified    = errors.New("catalog bulk: channel is not qualified")
	ErrApproval        = errors.New("catalog bulk: approval is required")
	ErrKillSwitch      = errors.New("catalog bulk: mass writes are stopped")
	refPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
	digestPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	currencyPattern    = regexp.MustCompile(`^[A-Z]{3}$`)
	filterPattern      = regexp.MustCompile(`^[a-z][a-z0-9._:-]{0,127}$`)
	mediaFormatPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9.+-]+$`)
)

const (
	// MaxSKUs is the maximum immutable selection size for one preview.
	MaxSKUs = 1000
	// MaxChannels bounds one workspace operation and keeps the partition fanout explicit.
	MaxChannels = 8
	// MaxRows bounds a preview at MaxSKUs multiplied by the channel fanout.
	MaxRows = MaxSKUs * MaxChannels
)

// CapabilityState describes the effective channel write posture.
type CapabilityState string

const (
	CapabilityReadOnly            CapabilityState = "read_only"
	CapabilityPartial             CapabilityState = "partially_supported"
	CapabilityReady               CapabilityState = "ready"
	CapabilityQualified           CapabilityState = "qualified"
	CapabilityQualificationNeeded CapabilityState = "qualification_required"
	CapabilityUnavailable         CapabilityState = "not_available"
)

func (s CapabilityState) Valid() bool {
	return s == CapabilityReadOnly || s == CapabilityPartial || s == CapabilityReady || s == CapabilityQualified || s == CapabilityQualificationNeeded || s == CapabilityUnavailable
}

// BulkState is the lifecycle of a durable catalog bulk operation.
type BulkState string

const (
	StateDraft            BulkState = "draft"
	StatePreviewed        BulkState = "previewed"
	StateAwaitingApproval BulkState = "awaiting_approval"
	StateQueued           BulkState = "queued"
	StateRunning          BulkState = "running"
	StatePartial          BulkState = "partial"
	StateCompleted        BulkState = "completed"
	StateFailed           BulkState = "failed"
	StateCancelled        BulkState = "cancelled"
	StateUnknown          BulkState = "unknown"
)

func (s BulkState) Valid() bool {
	switch s {
	case StateDraft, StatePreviewed, StateAwaitingApproval, StateQueued, StateRunning, StatePartial, StateCompleted, StateFailed, StateCancelled, StateUnknown:
		return true
	default:
		return false
	}
}

// KillSwitch is tenant-scoped emergency state for catalog bulk remote writes.
// Enabling it stops new apply intents and leaves all existing evidence intact.
type KillSwitch struct {
	Enabled   bool      `json:"enabled"`
	Reason    string    `json:"reason"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks the explicit operator reason and append-only version.
func (k KillSwitch) Validate() error {
	if k.Version < 1 || len(k.Reason) > 500 || strings.TrimSpace(k.Reason) != k.Reason || (k.Enabled && k.Reason == "") || !isUTC(k.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

// RowState describes the outcome of one SKU/channel partition.
type RowState string

const (
	RowReady           RowState = "ready"
	RowBlocked         RowState = "blocked"
	RowQueued          RowState = "queued"
	RowApplied         RowState = "applied"
	RowRejected        RowState = "rejected"
	RowConflict        RowState = "conflict"
	RowUnknown         RowState = "unknown"
	RowSkipped         RowState = "skipped"
	RowManualAttention RowState = "manual_attention"
)

func (s RowState) Valid() bool {
	switch s {
	case RowReady, RowBlocked, RowQueued, RowApplied, RowRejected, RowConflict, RowUnknown, RowSkipped, RowManualAttention:
		return true
	default:
		return false
	}
}

// ChannelTarget is a normalized projection target. The connector manifest is
// not sufficient to make a target writable: state must be qualified and the
// field capability must be present.
type ChannelTarget struct {
	ChannelID           string          `json:"channel_id"`
	AccountID           string          `json:"account_id"`
	Label               string          `json:"label"`
	State               CapabilityState `json:"state"`
	Capabilities        []string        `json:"capabilities,omitempty"`
	TaxonomyFingerprint string          `json:"taxonomy_fingerprint"`
	TaxonomyVersion     int64           `json:"taxonomy_version"`
	MappingVersion      int64           `json:"mapping_version"`
	FreshUntil          time.Time       `json:"fresh_until"`
	ObservedAt          time.Time       `json:"observed_at"`
}

func (t ChannelTarget) Validate() error {
	if !refPattern.MatchString(t.ChannelID) || !refPattern.MatchString(t.AccountID) || strings.TrimSpace(t.Label) != t.Label || t.Label == "" || len(t.Label) > 200 || !t.State.Valid() || !digestPattern.MatchString(t.TaxonomyFingerprint) || t.TaxonomyVersion < 1 || t.MappingVersion < 1 || !isUTC(t.FreshUntil) || !isUTC(t.ObservedAt) || t.FreshUntil.Before(t.ObservedAt) || len(t.Capabilities) > 64 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, capability := range t.Capabilities {
		if !filterPattern.MatchString(capability) {
			return ErrInvalid
		}
		if _, exists := seen[capability]; exists {
			return ErrConflict
		}
		seen[capability] = struct{}{}
	}
	return nil
}

// SelectionSnapshot is an immutable filter result. Product IDs and SKUs are
// copied into the snapshot so a later filter re-run cannot silently change it.
type SelectionSnapshot struct {
	FilterDigest    string          `json:"filter_digest"`
	Filter          string          `json:"filter"`
	ProductIDs      []string        `json:"product_ids,omitempty"`
	SKUs            []string        `json:"skus"`
	Targets         []ChannelTarget `json:"targets"`
	SnapshotVersion int64           `json:"snapshot_version"`
	CreatedAt       time.Time       `json:"created_at"`
	ExpiresAt       time.Time       `json:"expires_at"`
}

func (s SelectionSnapshot) Validate(now time.Time) error {
	if !digestPattern.MatchString(s.FilterDigest) || !filterPattern.MatchString(s.Filter) || len(s.SKUs) == 0 || len(s.SKUs) > MaxSKUs || len(s.Targets) == 0 || len(s.Targets) > MaxChannels || s.SnapshotVersion < 1 || !isUTC(s.CreatedAt) || !isUTC(s.ExpiresAt) || !s.ExpiresAt.After(s.CreatedAt) || !isUTC(now) || !now.Before(s.ExpiresAt) {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, sku := range s.SKUs {
		if !validText(sku, 200) {
			return ErrInvalid
		}
		if _, exists := seen[sku]; exists {
			return ErrConflict
		}
		seen[sku] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, target := range s.Targets {
		if target.Validate() != nil {
			return ErrInvalid
		}
		key := target.ChannelID + "\x00" + target.AccountID
		if _, exists := seen[key]; exists {
			return ErrConflict
		}
		seen[key] = struct{}{}
	}
	return nil
}

// MediaEdit is digest-only media input. The original PIM asset remains intact
// when a channel projection removes or replaces it.
type MediaEdit struct {
	AssetID     string `json:"asset_id"`
	AssetDigest string `json:"asset_digest"`
	Slot        string `json:"slot"`
	Format      string `json:"format"`
	Bytes       int64  `json:"bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Position    int    `json:"position"`
	Released    bool   `json:"released"`
	Safe        bool   `json:"safe"`
}

func (m MediaEdit) Validate() error {
	if !refPattern.MatchString(m.AssetID) || !digestPattern.MatchString(m.AssetDigest) || !filterPattern.MatchString(m.Slot) || !mediaFormatPattern.MatchString(strings.ToLower(m.Format)) || m.Bytes < 1 || m.Bytes > 100*1024*1024 || m.Width < 1 || m.Height < 1 || m.Position < 0 || m.Position > 1000 || !m.Released || !m.Safe {
		return ErrInvalid
	}
	return nil
}

// Change is a typed mass edit. Text and attributes use value/from_field;
// media uses the digest-only asset; price and stock use their typed fields.
type Change struct {
	Kind       string     `json:"kind"`
	Field      string     `json:"field"`
	Value      string     `json:"value,omitempty"`
	FromField  string     `json:"from_field,omitempty"`
	Media      *MediaEdit `json:"media,omitempty"`
	Currency   string     `json:"currency,omitempty"`
	PriceMinor *int64     `json:"price_minor_units,omitempty"`
	Stock      *int64     `json:"stock,omitempty"`
}

var changeKinds = map[string]struct{}{
	"set": {}, "replace": {}, "append": {}, "remove": {}, "normalize": {}, "copy": {},
	"add_media": {}, "replace_media": {}, "remove_media": {}, "reorder_media": {},
	"set_price": {}, "set_stock": {},
}

func (c Change) Validate() error {
	if _, ok := changeKinds[c.Kind]; !ok || !validField(c.Field) || len(c.Value) > 16000 || (c.FromField != "" && !validField(c.FromField)) || len(c.Currency) > 3 {
		return ErrInvalid
	}
	if c.Kind == "copy" && c.FromField == "" || c.Kind == "set" || c.Kind == "replace" || c.Kind == "append" {
		if c.Kind == "copy" && c.FromField == "" {
			return ErrInvalid
		}
		if c.Kind != "copy" && c.Value == "" && c.Field != "content.description" {
			return ErrInvalid
		}
	}
	if strings.HasPrefix(c.Field, "media.") {
		if c.Kind == "add_media" || c.Kind == "replace_media" || c.Kind == "reorder_media" {
			if c.Media == nil || c.Media.Validate() != nil {
				return ErrInvalid
			}
		}
	}
	if c.Kind == "set_price" {
		if !currencyPattern.MatchString(c.Currency) || c.PriceMinor == nil || *c.PriceMinor < 0 {
			return ErrInvalid
		}
	}
	if c.Kind == "set_stock" && (c.Stock == nil || *c.Stock < 0) {
		return ErrInvalid
	}
	return nil
}

// Projection is the channel-specific view of one canonical SKU.
type Projection struct {
	SKU             string                          `json:"sku"`
	ProductID       string                          `json:"product_id"`
	OfferID         string                          `json:"offer_id,omitempty"`
	ChannelID       string                          `json:"channel_id"`
	AccountID       string                          `json:"account_id"`
	Draft           marketplacelisting.ListingDraft `json:"draft"`
	Currency        string                          `json:"currency"`
	PriceMinorUnits int64                           `json:"price_minor_units"`
	Stock           int64                           `json:"stock"`
	Version         int64                           `json:"version"`
	PublishedDigest string                          `json:"published_digest,omitempty"`
}

func (p Projection) Validate() error {
	if !validText(p.SKU, 200) || !refPattern.MatchString(p.ProductID) || (p.OfferID != "" && !refPattern.MatchString(p.OfferID)) || !refPattern.MatchString(p.ChannelID) || !refPattern.MatchString(p.AccountID) || p.Draft.Validate() != nil || p.Draft.SKU != p.SKU || p.Draft.ProductID != p.ProductID || p.Draft.OrganizationID == "" || p.Currency == "" || !currencyPattern.MatchString(p.Currency) || p.PriceMinorUnits < 0 || p.Stock < 0 || p.Version < 1 || (p.PublishedDigest != "" && !digestPattern.MatchString(p.PublishedDigest)) {
		return ErrInvalid
	}
	return nil
}

// Diagnostic is deliberately the same normalized shape used by listing and
// quality gates, so UI/API/publication workers agree on blockers.
type Diagnostic = marketplacelisting.Diagnostic

// Row is one SKU/channel result. One successful row never masks another row.
type Row struct {
	ID              string          `json:"id"`
	SKU             string          `json:"sku"`
	ProductID       string          `json:"product_id"`
	ChannelID       string          `json:"channel_id"`
	AccountID       string          `json:"account_id"`
	CapabilityState CapabilityState `json:"capability_state"`
	Before          Projection      `json:"before"`
	After           Projection      `json:"after"`
	BeforeDigest    string          `json:"before_digest"`
	AfterDigest     string          `json:"after_digest"`
	ChangedFields   []string        `json:"changed_fields,omitempty"`
	Diagnostics     []Diagnostic    `json:"diagnostics,omitempty"`
	Eligible        bool            `json:"eligible"`
	State           RowState        `json:"state"`
	ExpectedVersion int64           `json:"expected_version"`
}

func (r Row) Validate() error {
	if !refPattern.MatchString(r.ID) || !validText(r.SKU, 200) || !refPattern.MatchString(r.ProductID) || !refPattern.MatchString(r.ChannelID) || !refPattern.MatchString(r.AccountID) || !r.CapabilityState.Valid() || r.Before.Validate() != nil || r.After.Validate() != nil || !digestPattern.MatchString(r.BeforeDigest) || !digestPattern.MatchString(r.AfterDigest) || !r.State.Valid() || r.ExpectedVersion < 1 {
		return ErrInvalid
	}
	for _, field := range r.ChangedFields {
		if !validField(field) {
			return ErrInvalid
		}
	}
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// Preview is immutable evidence for one multi-channel operation.
type Preview struct {
	ID             string            `json:"id"`
	OrganizationID string            `json:"organization_id"`
	WorkspaceID    string            `json:"workspace_id"`
	Selection      SelectionSnapshot `json:"selection"`
	Changes        []Change          `json:"changes"`
	InputDigest    string            `json:"input_digest"`
	AffectedSKU    int               `json:"affected_sku_count"`
	AffectedRows   int               `json:"affected_row_count"`
	EligibleRows   int               `json:"eligible_row_count"`
	BlockedRows    int               `json:"blocked_row_count"`
	State          BulkState         `json:"state"`
	Rows           []Row             `json:"rows"`
	CreatedAt      time.Time         `json:"created_at"`
}

func (p Preview) Validate(now time.Time) error {
	if !refPattern.MatchString(p.ID) || !refPattern.MatchString(p.OrganizationID) || !refPattern.MatchString(p.WorkspaceID) {
		return fmt.Errorf("catalog bulk preview: identity: %w", ErrInvalid)
	}
	if err := p.Selection.Validate(now); err != nil {
		return fmt.Errorf("catalog bulk preview: selection: %w", err)
	}
	if len(p.Changes) == 0 || !digestPattern.MatchString(p.InputDigest) || p.AffectedSKU != len(p.Selection.SKUs) || p.AffectedRows != len(p.Rows) || p.AffectedRows < 1 || p.AffectedRows > MaxRows || p.EligibleRows < 0 || p.BlockedRows < 0 || p.EligibleRows+p.BlockedRows != p.AffectedRows || !p.State.Valid() || p.State != StatePreviewed || !isUTC(p.CreatedAt) {
		return fmt.Errorf("catalog bulk preview: envelope: %w", ErrInvalid)
	}
	for _, change := range p.Changes {
		if change.Validate() != nil {
			return fmt.Errorf("catalog bulk preview: change: %w", ErrInvalid)
		}
	}
	for _, row := range p.Rows {
		if row.Validate() != nil {
			return fmt.Errorf("catalog bulk preview: row: %w", ErrInvalid)
		}
	}
	return nil
}

// RowResult is the append-only apply/reconciliation outcome for one row.
type RowResult struct {
	RowID               string   `json:"row_id"`
	SKU                 string   `json:"sku"`
	ChannelID           string   `json:"channel_id"`
	AccountID           string   `json:"account_id"`
	State               RowState `json:"state"`
	ErrorCode           string   `json:"error_code,omitempty"`
	RemoteReceiptDigest string   `json:"remote_receipt_digest,omitempty"`
	ReconciliationRef   string   `json:"reconciliation_ref,omitempty"`
}

func (r RowResult) Validate() error {
	if !refPattern.MatchString(r.RowID) || !validText(r.SKU, 200) || !refPattern.MatchString(r.ChannelID) || !refPattern.MatchString(r.AccountID) || !r.State.Valid() || len(r.ErrorCode) > 128 || (r.RemoteReceiptDigest != "" && !digestPattern.MatchString(r.RemoteReceiptDigest)) || len(r.ReconciliationRef) > 192 {
		return ErrInvalid
	}
	return nil
}

// Run is an immutable durable apply intent. Worker transitions are recorded as
// new evidence by the worker; this record never rewrites the PIM snapshot.
type Run struct {
	ID             string      `json:"id"`
	PreviewID      string      `json:"preview_id"`
	OrganizationID string      `json:"organization_id"`
	WorkspaceID    string      `json:"workspace_id"`
	ActorRef       string      `json:"actor_ref"`
	IdempotencyKey string      `json:"idempotency_key"`
	ApprovalRef    string      `json:"approval_ref"`
	State          BulkState   `json:"state"`
	InputDigest    string      `json:"input_digest"`
	Partitions     []string    `json:"partitions"`
	Results        []RowResult `json:"results"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

func (r Run) Validate() error {
	if !refPattern.MatchString(r.ID) || !refPattern.MatchString(r.PreviewID) || !refPattern.MatchString(r.OrganizationID) || !refPattern.MatchString(r.WorkspaceID) || !refPattern.MatchString(r.ActorRef) || len(r.IdempotencyKey) < 8 || len(r.IdempotencyKey) > 128 || !refPattern.MatchString(r.ApprovalRef) || !r.State.Valid() || !digestPattern.MatchString(r.InputDigest) || len(r.Partitions) == 0 || len(r.Partitions) > MaxRows || len(r.Results) > MaxRows || !isUTC(r.CreatedAt) || !isUTC(r.UpdatedAt) || r.UpdatedAt.Before(r.CreatedAt) {
		return ErrInvalid
	}
	for _, partition := range r.Partitions {
		if !refPattern.MatchString(partition) {
			return ErrInvalid
		}
	}
	for _, result := range r.Results {
		if result.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// RemoteObservation is normalized connector output; raw provider responses
// are intentionally not part of the public contract.
type RemoteObservation struct {
	RowID          string    `json:"row_id"`
	RemoteID       string    `json:"remote_id"`
	ObservedDigest string    `json:"observed_digest"`
	Status         string    `json:"status"`
	ObservedAt     time.Time `json:"observed_at"`
}

func (o RemoteObservation) Validate() error {
	if !refPattern.MatchString(o.RowID) || !refPattern.MatchString(o.RemoteID) || !digestPattern.MatchString(o.ObservedDigest) || !filterPattern.MatchString(o.Status) || !isUTC(o.ObservedAt) {
		return ErrInvalid
	}
	return nil
}

// Reconciliation is the safe read-after-write decision.
type Reconciliation struct {
	RowID          string   `json:"row_id"`
	ExpectedDigest string   `json:"expected_digest"`
	ObservedDigest string   `json:"observed_digest"`
	Decision       string   `json:"decision"`
	Drifts         []string `json:"drifts,omitempty"`
}

// BuildPreview creates a deterministic cross-channel before/after preview.
func BuildPreview(id, organizationID, workspaceID string, selection SelectionSnapshot, projections []Projection, changes []Change, now time.Time) (Preview, error) {
	if !refPattern.MatchString(id) || !refPattern.MatchString(organizationID) || !refPattern.MatchString(workspaceID) {
		return Preview{}, fmt.Errorf("catalog bulk preview: identity: %w", ErrInvalid)
	}
	if err := selection.Validate(now); err != nil {
		return Preview{}, fmt.Errorf("catalog bulk preview: selection: %w", err)
	}
	if len(projections) == 0 || len(projections) > MaxRows || len(changes) == 0 || !isUTC(now) {
		return Preview{}, fmt.Errorf("catalog bulk preview: input bounds: %w", ErrInvalid)
	}
	for _, change := range changes {
		if err := change.Validate(); err != nil {
			return Preview{}, fmt.Errorf("catalog bulk preview: change: %w", err)
		}
	}
	skuSet := make(map[string]struct{}, len(selection.SKUs))
	for _, sku := range selection.SKUs {
		skuSet[sku] = struct{}{}
	}
	targets := make(map[string]ChannelTarget, len(selection.Targets))
	for _, target := range selection.Targets {
		targets[target.ChannelID+"\x00"+target.AccountID] = target
	}
	rowsInput := append([]Projection(nil), projections...)
	sort.Slice(rowsInput, func(i, j int) bool {
		left := rowsInput[i].ChannelID + "\x00" + rowsInput[i].AccountID + "\x00" + rowsInput[i].SKU
		right := rowsInput[j].ChannelID + "\x00" + rowsInput[j].AccountID + "\x00" + rowsInput[j].SKU
		return left < right
	})
	seen := map[string]struct{}{}
	rows := make([]Row, 0, len(rowsInput))
	for _, before := range rowsInput {
		if err := before.Validate(); err != nil {
			return Preview{}, fmt.Errorf("catalog bulk preview: projection: %w", err)
		}
		if _, ok := skuSet[before.SKU]; !ok {
			return Preview{}, ErrConflict
		}
		key := before.ChannelID + "\x00" + before.AccountID + "\x00" + before.SKU
		if _, ok := seen[key]; ok {
			return Preview{}, ErrConflict
		}
		seen[key] = struct{}{}
		target, ok := targets[before.ChannelID+"\x00"+before.AccountID]
		if !ok {
			return Preview{}, ErrConflict
		}
		after, err := ApplyChanges(before, changes)
		if err != nil {
			return Preview{}, err
		}
		beforeDigest, err := projectionDigest(before)
		if err != nil {
			return Preview{}, err
		}
		afterDigest, err := projectionDigest(after)
		if err != nil {
			return Preview{}, err
		}
		diagnostics := make([]Diagnostic, 0, 4)
		if target.ChannelID == "demo" {
			diagnostics = marketplacelisting.ValidateDraft(demoTaxonomyForProjection(target, now), after.Draft, now)
		} else if after.Draft.Validate() != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_projection", Severity: marketplacelisting.SeverityBlock, FieldPath: "projection", Message: "Проекция карточки содержит недопустимые значения", Expected: "валидная проекция", Observed: "invalid", Remediation: "Исправьте карточку в PIM и повторите preview"})
		}
		if now.After(target.FreshUntil) || target.TaxonomyFingerprint != after.Draft.TaxonomyFingerprint {
			diagnostics = append(diagnostics, Diagnostic{Code: "stale_mapping", Severity: marketplacelisting.SeverityBlock, FieldPath: "channel.mapping", Message: "Схема или mapping канала устарели", Expected: target.TaxonomyFingerprint, Observed: after.Draft.TaxonomyFingerprint, Remediation: "Обновите taxonomy и повторите preview"})
		}
		if target.State != CapabilityQualified {
			diagnostics = append(diagnostics, Diagnostic{Code: "channel_not_qualified", Severity: marketplacelisting.SeverityBlock, FieldPath: "channel.capability", Message: "Массовая запись канала не квалифицирована", Expected: string(CapabilityQualified), Observed: string(target.State), Remediation: "Оставьте канал read-only или пройдите connector qualification"})
		}
		for _, change := range changes {
			required := requiredCapability(change.Field)
			if required != "" && !contains(target.Capabilities, required) {
				diagnostics = append(diagnostics, Diagnostic{Code: "unsupported_capability", Severity: marketplacelisting.SeverityBlock, FieldPath: change.Field, Message: "Канал не поддерживает эту массовую операцию", Expected: required, Observed: strings.Join(target.Capabilities, ","), Remediation: "Выберите другой канал или оставьте строку без записи"})
			}
		}
		sort.SliceStable(diagnostics, func(i, j int) bool {
			return diagnostics[i].FieldPath+diagnostics[i].Code < diagnostics[j].FieldPath+diagnostics[j].Code
		})
		changedFields := changedFields(before, after, changes)
		eligible := len(changedFields) > 0 && !hasBlock(diagnostics)
		state := RowReady
		if !eligible {
			state = RowBlocked
		}
		rows = append(rows, Row{ID: stableID("cbr_", organizationID, workspaceID, key), SKU: before.SKU, ProductID: before.ProductID, ChannelID: before.ChannelID, AccountID: before.AccountID, CapabilityState: target.State, Before: before, After: after, BeforeDigest: beforeDigest, AfterDigest: afterDigest, ChangedFields: changedFields, Diagnostics: diagnostics, Eligible: eligible, State: state, ExpectedVersion: before.Version})
	}
	if len(rows) == 0 || len(rows) > MaxRows {
		return Preview{}, ErrTooLarge
	}
	input := struct {
		Selection SelectionSnapshot `json:"selection"`
		Rows      []Row             `json:"rows"`
		Changes   []Change          `json:"changes"`
	}{selection, rows, changes}
	inputDigest, err := digest(input)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{ID: id, OrganizationID: organizationID, WorkspaceID: workspaceID, Selection: selection, Changes: changes, InputDigest: inputDigest, AffectedSKU: len(selection.SKUs), AffectedRows: len(rows), State: StatePreviewed, Rows: rows, CreatedAt: now}
	for _, row := range rows {
		if row.Eligible {
			preview.EligibleRows++
		} else {
			preview.BlockedRows++
		}
	}
	if preview.Validate(now) != nil {
		return Preview{}, fmt.Errorf("catalog bulk: preview validation: %w", ErrInvalid)
	}
	return preview, nil
}

// NewRun partitions only the qualified, valid rows by channel/account. Blocked
// rows remain explicit skipped results and never disappear from the outcome.
func NewRun(id, idempotencyKey, approvalRef, actorRef string, preview Preview, now time.Time) (Run, error) {
	if preview.Validate(now) != nil || !refPattern.MatchString(id) || len(idempotencyKey) < 8 || !refPattern.MatchString(approvalRef) || !refPattern.MatchString(actorRef) || preview.EligibleRows == 0 || !isUTC(now) {
		return Run{}, ErrInvalid
	}
	partitionsSet := map[string]struct{}{}
	results := make([]RowResult, 0, len(preview.Rows))
	for _, row := range preview.Rows {
		if row.Eligible {
			partitionsSet[row.ChannelID+"/"+row.AccountID] = struct{}{}
			results = append(results, RowResult{RowID: row.ID, SKU: row.SKU, ChannelID: row.ChannelID, AccountID: row.AccountID, State: RowQueued})
		} else {
			results = append(results, RowResult{RowID: row.ID, SKU: row.SKU, ChannelID: row.ChannelID, AccountID: row.AccountID, State: RowSkipped, ErrorCode: "preview_blocked"})
		}
	}
	partitions := make([]string, 0, len(partitionsSet))
	for partition := range partitionsSet {
		partitions = append(partitions, partition)
	}
	sort.Strings(partitions)
	run := Run{ID: id, PreviewID: preview.ID, OrganizationID: preview.OrganizationID, WorkspaceID: preview.WorkspaceID, ActorRef: actorRef, IdempotencyKey: idempotencyKey, ApprovalRef: approvalRef, State: StateQueued, InputDigest: preview.InputDigest, Partitions: partitions, Results: results, CreatedAt: now, UpdatedAt: now}
	if run.Validate() != nil {
		return Run{}, ErrInvalid
	}
	return run, nil
}

// Reconcile compares normalized remote state with the expected row digest.
func Reconcile(row Row, observation RemoteObservation) (Reconciliation, error) {
	if row.Validate() != nil || observation.Validate() != nil || row.ID != observation.RowID {
		return Reconciliation{}, ErrInvalid
	}
	result := Reconciliation{RowID: row.ID, ExpectedDigest: row.AfterDigest, ObservedDigest: observation.ObservedDigest, Decision: "reconciled"}
	if observation.ObservedDigest != row.AfterDigest {
		result.Decision = "needs_attention"
		result.Drifts = []string{"projection_mismatch"}
	}
	if observation.Status == "unknown" {
		result.Decision = "needs_attention"
		result.Drifts = append(result.Drifts, "unknown_remote_outcome")
	}
	return result, nil
}

func ApplyChanges(before Projection, changes []Change) (Projection, error) {
	if err := before.Validate(); err != nil {
		return Projection{}, fmt.Errorf("catalog bulk apply: before projection: %w", err)
	}
	after := before
	after.Draft.Attributes = cloneAttributes(before.Draft.Attributes)
	after.Draft.Content.Bullets = append([]string(nil), before.Draft.Content.Bullets...)
	after.Draft.Media = append([]marketplacelisting.MediaRef(nil), before.Draft.Media...)
	after.Draft.Variants = append([]marketplacelisting.Variant(nil), before.Draft.Variants...)
	for _, change := range changes {
		if err := change.Validate(); err != nil {
			return Projection{}, fmt.Errorf("catalog bulk apply: change: %w", err)
		}
		switch {
		case change.Kind == "set_price":
			after.Currency, after.PriceMinorUnits = change.Currency, *change.PriceMinor
		case change.Kind == "set_stock":
			after.Stock = *change.Stock
		case strings.HasPrefix(change.Field, "media."):
			if err := applyMedia(&after.Draft.Media, change); err != nil {
				return Projection{}, err
			}
		case change.Field == "category_code":
			if change.Kind == "set" || change.Kind == "replace" {
				after.Draft.CategoryCode = change.Value
			}
		case change.Field == "content.title":
			after.Draft.Content.Title = applyText(after.Draft.Content.Title, change)
		case change.Field == "content.description":
			after.Draft.Content.Description = applyText(after.Draft.Content.Description, change)
		case change.Field == "content.brand":
			after.Draft.Content.Brand = applyText(after.Draft.Content.Brand, change)
		case strings.HasPrefix(change.Field, "attributes."):
			code := strings.TrimPrefix(change.Field, "attributes.")
			current := after.Draft.Attributes[code]
			switch change.Kind {
			case "set", "replace":
				current.Value = change.Value
			case "append":
				if current.Value == "" {
					current.Value = change.Value
				} else {
					current.Value += "," + change.Value
				}
			case "remove":
				delete(after.Draft.Attributes, code)
			case "normalize":
				current.Value = strings.ToLower(strings.TrimSpace(current.Value))
			case "copy":
				if source, ok := attributeSource(after.Draft.Attributes, change.FromField); ok {
					current = source
				}
			}
			if change.Kind != "remove" {
				after.Draft.Attributes[code] = current
			}
		case strings.HasPrefix(change.Field, "variants."):
			if err := applyVariant(&after.Draft.Variants, change); err != nil {
				return Projection{}, err
			}
		default:
			return Projection{}, ErrInvalid
		}
	}
	if err := after.Validate(); err != nil {
		return Projection{}, fmt.Errorf("catalog bulk apply: after projection: %w", err)
	}
	return after, nil
}

func applyMedia(media *[]marketplacelisting.MediaRef, change Change) error {
	if change.Kind == "remove_media" {
		id := strings.TrimSpace(change.Value)
		for i, item := range *media {
			if item.ID == id {
				*media = append((*media)[:i], (*media)[i+1:]...)
				return nil
			}
		}
		return ErrConflict
	}
	if change.Media == nil {
		return ErrInvalid
	}
	item := marketplacelisting.MediaRef{ID: change.Media.AssetID, Slot: change.Media.Slot, ReleasedObjectRef: "upl_" + change.Media.AssetID, Digest: change.Media.AssetDigest, Format: change.Media.Format, Bytes: change.Media.Bytes, Width: change.Media.Width, Height: change.Media.Height, Position: change.Media.Position, Released: change.Media.Released, Safe: change.Media.Safe}
	if change.Kind == "replace_media" {
		for i, current := range *media {
			if current.Slot == change.Media.Slot && current.Position == change.Media.Position {
				(*media)[i] = item
				return nil
			}
		}
	}
	*media = append(*media, item)
	sort.SliceStable(*media, func(i, j int) bool { return (*media)[i].Position < (*media)[j].Position })
	return nil
}

func applyVariant(variants *[]marketplacelisting.Variant, change Change) error {
	if change.Kind == "remove" {
		for i, variant := range *variants {
			if variant.SKU == change.Value {
				*variants = append((*variants)[:i], (*variants)[i+1:]...)
				return nil
			}
		}
		return ErrConflict
	}
	if change.Kind != "set" && change.Kind != "replace" {
		return ErrInvalid
	}
	var variant marketplacelisting.Variant
	if json.Unmarshal([]byte(change.Value), &variant) != nil || variant.Validate() != nil {
		return ErrInvalid
	}
	for i, current := range *variants {
		if current.SKU == variant.SKU {
			(*variants)[i] = variant
			return nil
		}
	}
	*variants = append(*variants, variant)
	return nil
}

func applyText(current string, change Change) string {
	switch change.Kind {
	case "set", "replace":
		if change.Kind == "replace" && change.FromField != "" {
			return strings.ReplaceAll(current, change.FromField, change.Value)
		}
		return change.Value
	case "append":
		if current == "" {
			return change.Value
		}
		return current + " " + change.Value
	case "remove":
		return strings.ReplaceAll(current, change.Value, "")
	case "normalize":
		return strings.TrimSpace(current)
	case "copy":
		return current
	default:
		return current
	}
}

func changedFields(before, after Projection, changes []Change) []string {
	result := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Kind == "set_price" && before.PriceMinorUnits != after.PriceMinorUnits || change.Kind == "set_stock" && before.Stock != after.Stock || change.Field != "" && before.Draft.Content.Title != after.Draft.Content.Title && change.Field == "content.title" || change.Field == "content.description" && before.Draft.Content.Description != after.Draft.Content.Description || change.Field == "content.brand" && before.Draft.Content.Brand != after.Draft.Content.Brand || change.Field == "category_code" && before.Draft.CategoryCode != after.Draft.CategoryCode || strings.HasPrefix(change.Field, "attributes.") || strings.HasPrefix(change.Field, "media.") || strings.HasPrefix(change.Field, "variants.") {
			result = append(result, change.Field)
		}
	}
	sort.Strings(result)
	return uniqueStrings(result)
}

func validField(field string) bool {
	if field == "category_code" || field == "content.title" || field == "content.description" || field == "content.brand" || field == "price" || field == "stock" || strings.HasPrefix(field, "media.") || strings.HasPrefix(field, "variants.") {
		return true
	}
	return strings.HasPrefix(field, "attributes.") && filterPattern.MatchString(strings.TrimPrefix(field, "attributes."))
}

func requiredCapability(field string) string {
	switch {
	case field == "price":
		return "prices.write"
	case field == "stock":
		return "inventory.write"
	case strings.HasPrefix(field, "media."):
		return "marketplace.listings.media.write"
	case strings.HasPrefix(field, "variants."), strings.HasPrefix(field, "attributes."), field == "category_code":
		return "marketplace.listings.attributes.write"
	case strings.HasPrefix(field, "content."):
		return "marketplace.listings.content.write"
	default:
		return "products.write"
	}
}

func projectionDigest(value Projection) (string, error) { return digest(value) }

func digest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func stableID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:16])
}
func isUTC(t time.Time) bool { return !t.IsZero() && t.Location() == time.UTC }
func validText(value string, max int) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func hasBlock(values []Diagnostic) bool {
	for _, value := range values {
		if value.Severity == marketplacelisting.SeverityBlock {
			return true
		}
	}
	return false
}
func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
func cloneAttributes(values map[string]marketplacelisting.AttributeValue) map[string]marketplacelisting.AttributeValue {
	result := make(map[string]marketplacelisting.AttributeValue, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func attributeSource(values map[string]marketplacelisting.AttributeValue, field string) (marketplacelisting.AttributeValue, bool) {
	return values[strings.TrimPrefix(field, "attributes.")], values[strings.TrimPrefix(field, "attributes.")].Value != ""
}

// demoTaxonomyForProjection is only a deterministic fallback for synthetic
// previews. Real channel taxonomy is supplied by the connector boundary and
// must be stored in the preview by the API.
func demoTaxonomyForProjection(target ChannelTarget, now time.Time) marketplacelisting.Taxonomy {
	return marketplacelisting.DemoTaxonomy(target.ChannelID, "ru-RU", "RU", now)
}
