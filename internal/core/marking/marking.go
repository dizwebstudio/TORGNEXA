// Package marking contains the provider-neutral domain for traceability codes.
//
// A MarkingCode deliberately contains only a fingerprint. Plain code values
// may cross the edge only through the short-lived RawCodeStore port and are
// never part of domain events, audit summaries or ordinary API responses.
package marking

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid       = errors.New("marking: invalid value")
	ErrNotFound      = errors.New("marking: not found")
	ErrConflict      = errors.New("marking: conflict")
	ErrInvalidState  = errors.New("marking: invalid state transition")
	ErrCycle         = errors.New("marking: package cycle")
	ErrRawCode      = errors.New("marking: raw code is not allowed here")
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var gtinPattern = regexp.MustCompile(`^[0-9]{8,14}$`)
var skuPattern = regexp.MustCompile(`^[^[:cntrl:][:space:]][^[:cntrl:]]{0,199}$`)

// Scope identifies the tenant that owns a marking aggregate.
type Scope struct{ organizationID, workspaceID string }

// ParseScope validates a tenant scope.
func ParseScope(organizationID, workspaceID string) (Scope, error) {
	if !refPattern.MatchString(organizationID) || !refPattern.MatchString(workspaceID) {
		return Scope{}, ErrInvalid
	}
	return Scope{organizationID: organizationID, workspaceID: workspaceID}, nil
}

func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool            { return refPattern.MatchString(s.organizationID) && refPattern.MatchString(s.workspaceID) }

// ProductGroup is a traceability category used to select the applicable
// operation matrix. The list is intentionally generic; provider catalogs own
// jurisdiction-specific category identifiers.
type ProductGroup string

const (
	ProductGroupClothing   ProductGroup = "clothing"
	ProductGroupShoes       ProductGroup = "shoes"
	ProductGroupTobacco     ProductGroup = "tobacco"
	ProductGroupWater       ProductGroup = "water"
	ProductGroupMedicine    ProductGroup = "medicine"
	ProductGroupFood        ProductGroup = "food"
	ProductGroupCosmetics   ProductGroup = "cosmetics"
	ProductGroupOther       ProductGroup = "other"
)

func (g ProductGroup) Valid() bool {
	switch g {
	case ProductGroupClothing, ProductGroupShoes, ProductGroupTobacco, ProductGroupWater, ProductGroupMedicine, ProductGroupFood, ProductGroupCosmetics, ProductGroupOther:
		return true
	default:
		return false
	}
}

// CodeStatus is the local state of a marking code. Remote statuses are stored
// as observations and never silently overwrite this state.
type CodeStatus string

const (
	CodeRequested      CodeStatus = "requested"
	CodeReserved       CodeStatus = "reserved"
	CodeAvailable      CodeStatus = "available"
	CodePrinted        CodeStatus = "printed"
	CodeApplied        CodeStatus = "applied"
	CodeAggregated     CodeStatus = "aggregated"
	CodeIntroduced     CodeStatus = "introduced"
	CodeInCirculation  CodeStatus = "in_circulation"
	CodeSold           CodeStatus = "sold"
	CodeWithdrawn      CodeStatus = "withdrawn"
	CodeWrittenOff     CodeStatus = "written_off"
	CodeReturned       CodeStatus = "returned"
	CodeRejected       CodeStatus = "rejected"
	CodeUnknown        CodeStatus = "unknown"
)

func (s CodeStatus) Valid() bool {
	switch s {
	case CodeRequested, CodeReserved, CodeAvailable, CodePrinted, CodeApplied, CodeAggregated, CodeIntroduced, CodeInCirculation, CodeSold, CodeWithdrawn, CodeWrittenOff, CodeReturned, CodeRejected, CodeUnknown:
		return true
	default:
		return false
	}
}

// CanTransition reports whether a local code lifecycle transition is legal.
func CanTransition(from, to CodeStatus) bool {
	if from == to {
		return true
	}
	allowed := map[CodeStatus][]CodeStatus{
		CodeRequested:     {CodeReserved, CodeAvailable, CodeUnknown, CodeRejected},
		CodeReserved:      {CodeAvailable, CodePrinted, CodeUnknown, CodeRejected},
		CodeAvailable:     {CodeReserved, CodePrinted, CodeApplied, CodeUnknown, CodeRejected},
		CodePrinted:       {CodeApplied, CodeAvailable, CodeUnknown},
		CodeApplied:       {CodeAggregated, CodeIntroduced, CodeUnknown},
		CodeAggregated:    {CodeIntroduced, CodeUnknown},
		CodeIntroduced:    {CodeInCirculation, CodeWithdrawn, CodeUnknown},
		CodeInCirculation: {CodeSold, CodeWithdrawn, CodeWrittenOff, CodeReturned, CodeUnknown},
		CodeSold:          {CodeReturned, CodeUnknown},
		CodeWithdrawn:     {CodeReturned, CodeUnknown},
		CodeWrittenOff:    {CodeReturned, CodeUnknown},
		CodeReturned:      {CodeInCirculation, CodeWithdrawn, CodeUnknown},
		CodeUnknown:       {CodeRequested, CodeReserved, CodeAvailable, CodePrinted, CodeApplied, CodeAggregated, CodeIntroduced, CodeInCirculation, CodeSold, CodeWithdrawn, CodeWrittenOff, CodeReturned, CodeRejected},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// CodeFingerprint hashes a scanned/raw code for durable correlation. The raw
// value is not returned and must be discarded by the caller after this call.
func CodeFingerprint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
		return "", ErrRawCode
	}
	for _, r := range value {
		if r == 0 || r == 0x7f || r == '\r' || r == '\n' || r == '\t' {
			return "", ErrRawCode
		}
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:]), nil
}

func validRef(value string) bool       { return refPattern.MatchString(value) }
func validDigest(value string) bool    { return digestPattern.MatchString(value) }
func validGTIN(value string) bool      { return gtinPattern.MatchString(value) }
func validSKU(value string) bool       { return skuPattern.MatchString(value) && strings.TrimSpace(value) == value }
func validTime(value time.Time) bool   { return !value.IsZero() && value.Location() == time.UTC }
func validQuantity(value int64) bool   { return value > 0 && value <= 1000000000 }

// CodeBatch describes a request/response batch without retaining its raw
// codes. RawArtifactRef is an expiring reference owned by the secret/artifact
// boundary, never a blob or a code list.
type CodeBatch struct {
	ID             string       `json:"id"`
	OrganizationID string       `json:"organization_id"`
	WorkspaceID    string       `json:"workspace_id"`
	ProductGroup   ProductGroup `json:"product_group"`
	GTIN           string       `json:"gtin"`
	SKU            string       `json:"sku"`
	Requested      int64        `json:"requested"`
	Received       int64        `json:"received"`
	Reserved       int64        `json:"reserved"`
	Status         CodeStatus   `json:"status"`
	RawArtifactRef string       `json:"raw_artifact_ref,omitempty"`
	ExpiresAt      time.Time    `json:"expires_at,omitempty"`
	Version        int64        `json:"version"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

func (b CodeBatch) Validate() error {
	if !validRef(b.ID) || !validRef(b.OrganizationID) || !validRef(b.WorkspaceID) || !b.ProductGroup.Valid() || !validGTIN(b.GTIN) || !validSKU(b.SKU) || !validQuantity(b.Requested) || b.Received < 0 || b.Received > b.Requested || b.Reserved < 0 || b.Reserved > b.Received || !b.Status.Valid() || b.Version < 1 || !validTime(b.CreatedAt) || !validTime(b.UpdatedAt) || b.UpdatedAt.Before(b.CreatedAt) {
		return ErrInvalid
	}
	if b.RawArtifactRef != "" && !validRef(b.RawArtifactRef) {
		return ErrInvalid
	}
	if !b.ExpiresAt.IsZero() && !validTime(b.ExpiresAt) {
		return ErrInvalid
	}
	return nil
}

// MarkingCode is the durable safe representation of one code.
type MarkingCode struct {
	Fingerprint    string     `json:"fingerprint"`
	BatchID        string     `json:"batch_id"`
	GTIN           string     `json:"gtin"`
	SKU            string     `json:"sku"`
	Status         CodeStatus `json:"status"`
	PackageID      string     `json:"package_id,omitempty"`
	RemoteStatus   string     `json:"remote_status,omitempty"`
	LastObservedAt *time.Time `json:"last_observed_at,omitempty"`
	Version        int64      `json:"version"`
}

func (c MarkingCode) Validate() error {
	if !validDigest(c.Fingerprint) || !validRef(c.BatchID) || !validGTIN(c.GTIN) || !validSKU(c.SKU) || !c.Status.Valid() || c.Version < 1 {
		return ErrInvalid
	}
	if c.PackageID != "" && !validRef(c.PackageID) || c.RemoteStatus != "" && !validRef(c.RemoteStatus) {
		return ErrInvalid
	}
	if c.LastObservedAt != nil && !validTime(*c.LastObservedAt) {
		return ErrInvalid
	}
	return nil
}

// OperationKind names an auditable local intent and its remote capability.
type OperationKind string

const (
	OperationRequestCodes    OperationKind = "marking.codes.request"
	OperationReserveCodes   OperationKind = "marking.codes.reserve"
	OperationPrintLabels    OperationKind = "marking.labels.print"
	OperationScanCode       OperationKind = "marking.codes.scan"
	OperationAggregate      OperationKind = "marking.aggregation.write"
	OperationIntroduce      OperationKind = "marking.circulation.introduce"
	OperationWithdraw       OperationKind = "marking.circulation.withdraw"
	OperationTransfer       OperationKind = "marking.transfer.write"
	OperationCreateUPD      OperationKind = "marking.upd.create"
	OperationSignUPD        OperationKind = "marking.upd.sign"
	OperationSendEDO        OperationKind = "marking.edo.send"
	OperationReconcile      OperationKind = "marking.reconciliation.run"
)

func (k OperationKind) Valid() bool {
	switch k {
	case OperationRequestCodes, OperationReserveCodes, OperationPrintLabels, OperationScanCode, OperationAggregate, OperationIntroduce, OperationWithdraw, OperationTransfer, OperationCreateUPD, OperationSignUPD, OperationSendEDO, OperationReconcile:
		return true
	default:
		return false
	}
}

// OperationState preserves unknown remote outcomes for reconciliation instead
// of retrying an operation that may already have been accepted.
type OperationState string

const (
	OperationQueued    OperationState = "queued"
	OperationRunning   OperationState = "running"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
	OperationUnknown   OperationState = "unknown"
	OperationCancelled OperationState = "cancelled"
)

func (s OperationState) Valid() bool {
	switch s {
	case OperationQueued, OperationRunning, OperationSucceeded, OperationFailed, OperationUnknown, OperationCancelled:
		return true
	default:
		return false
	}
}

type Operation struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	WorkspaceID    string         `json:"workspace_id"`
	Kind           OperationKind  `json:"kind"`
	State          OperationState `json:"state"`
	IdempotencyKey string         `json:"idempotency_key"`
	DryRun         bool           `json:"dry_run"`
	ApprovalRef    string         `json:"approval_ref,omitempty"`
	ArtifactRef    string         `json:"artifact_ref,omitempty"`
	RemoteID       string         `json:"remote_id,omitempty"`
	ErrorCode      string         `json:"error_code,omitempty"`
	Attempt        int            `json:"attempt"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (o Operation) Validate() error {
	if !validRef(o.ID) || !validRef(o.OrganizationID) || !validRef(o.WorkspaceID) || !o.Kind.Valid() || !o.State.Valid() || !validRef(o.IdempotencyKey) || o.Attempt < 1 || !validTime(o.CreatedAt) || !validTime(o.UpdatedAt) || o.UpdatedAt.Before(o.CreatedAt) {
		return ErrInvalid
	}
	for _, value := range []string{o.ApprovalRef, o.ArtifactRef, o.RemoteID, o.ErrorCode} {
		if value != "" && !validRef(value) {
			return ErrInvalid
		}
	}
	return nil
}

type PackageKind string

const (
	PackageUnit    PackageKind = "unit"
	PackageKit     PackageKind = "kit"
	PackageBox     PackageKind = "box"
	PackagePallet  PackageKind = "pallet"
)

func (k PackageKind) Valid() bool { return k == PackageUnit || k == PackageKit || k == PackageBox || k == PackagePallet }

type PackageLink struct {
	ParentID string `json:"parent_id"`
	ChildID  string `json:"child_id"`
	Quantity int64  `json:"quantity"`
}

type MarkingPackage struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	WorkspaceID    string         `json:"workspace_id"`
	Kind           PackageKind    `json:"kind"`
	CodeFingerprint string        `json:"code_fingerprint"`
	ParentID       string         `json:"parent_id,omitempty"`
	Status         string         `json:"status"`
	ShipmentRef    string         `json:"shipment_ref,omitempty"`
	OrderRef       string         `json:"order_ref,omitempty"`
	UPDRef         string         `json:"upd_ref,omitempty"`
	Version        int64          `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (p MarkingPackage) Validate() error {
	if !validRef(p.ID) || !validRef(p.OrganizationID) || !validRef(p.WorkspaceID) || !p.Kind.Valid() || !validDigest(p.CodeFingerprint) || !validRef(p.Status) || p.Version < 1 || !validTime(p.CreatedAt) || !validTime(p.UpdatedAt) || p.UpdatedAt.Before(p.CreatedAt) {
		return ErrInvalid
	}
	for _, value := range []string{p.ParentID, p.ShipmentRef, p.OrderRef, p.UPDRef} {
		if value != "" && !validRef(value) {
			return ErrInvalid
		}
	}
	return nil
}

// ValidatePackageTree checks parent/child links and rejects self-links and
// cycles before an aggregate can be closed.
func ValidatePackageTree(packages []MarkingPackage, links []PackageLink) error {
	byID := make(map[string]MarkingPackage, len(packages))
	for _, item := range packages {
		if err := item.Validate(); err != nil {
			return err
		}
		if _, exists := byID[item.ID]; exists {
			return ErrConflict
		}
		byID[item.ID] = item
	}
	children := make(map[string][]string, len(packages))
	for _, link := range links {
		if !validRef(link.ParentID) || !validRef(link.ChildID) || link.ParentID == link.ChildID || !validQuantity(link.Quantity) {
			return ErrInvalid
		}
		parent, pok := byID[link.ParentID]
		child, cok := byID[link.ChildID]
		if !pok || !cok || parent.Kind == PackageUnit || child.Kind == PackagePallet {
			return ErrInvalid
		}
		children[link.ParentID] = append(children[link.ParentID], link.ChildID)
	}
	for id := range byID {
		seen := map[string]bool{}
		var visit func(string) error
		visit = func(current string) error {
			if seen[current] {
				return ErrCycle
			}
			seen[current] = true
			for _, child := range children[current] {
				if err := visit(child); err != nil {
					return err
				}
			}
			delete(seen, current)
			return nil
		}
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

type PrintJobState string

const (
	PrintQueued    PrintJobState = "queued"
	PrintRunning   PrintJobState = "running"
	PrintCompleted PrintJobState = "completed"
	PrintFailed    PrintJobState = "failed"
	PrintUnknown   PrintJobState = "unknown"
)

func (s PrintJobState) Valid() bool { return s == PrintQueued || s == PrintRunning || s == PrintCompleted || s == PrintFailed || s == PrintUnknown }

type PrintJob struct {
	ID             string        `json:"id"`
	TemplateRef    string        `json:"template_ref"`
	TemplateVersion int64        `json:"template_version"`
	PrinterRef     string        `json:"printer_ref"`
	CodeCount      int64         `json:"code_count"`
	State          PrintJobState `json:"state"`
	Attempt        int           `json:"attempt"`
	IdempotencyKey string        `json:"idempotency_key"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func (p PrintJob) Validate() error {
	if !validRef(p.ID) || !validRef(p.TemplateRef) || p.TemplateVersion < 1 || !validRef(p.PrinterRef) || !validQuantity(p.CodeCount) || !p.State.Valid() || p.Attempt < 1 || !validRef(p.IdempotencyKey) || !validTime(p.CreatedAt) || !validTime(p.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type ScanResult string

const (
	ScanAccepted ScanResult = "accepted"
	ScanRejected ScanResult = "rejected"
	ScanDuplicate ScanResult = "duplicate"
	ScanOverflow ScanResult = "overflow"
)

func (r ScanResult) Valid() bool { return r == ScanAccepted || r == ScanRejected || r == ScanDuplicate || r == ScanOverflow }

type Scan struct {
	ID             string     `json:"id"`
	Fingerprint    string     `json:"fingerprint"`
	SKU            string     `json:"sku"`
	GTIN           string     `json:"gtin"`
	WMSAction      string     `json:"wms_action"`
	Result         ScanResult `json:"result"`
	ReasonCode     string     `json:"reason_code,omitempty"`
	ActorID        string     `json:"actor_id"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

func (s Scan) Validate() error {
	if !validRef(s.ID) || !validDigest(s.Fingerprint) || !validSKU(s.SKU) || !validGTIN(s.GTIN) || !validRef(s.WMSAction) || !s.Result.Valid() || !validRef(s.ActorID) || !validTime(s.OccurredAt) {
		return ErrInvalid
	}
	if s.ReasonCode != "" && !validRef(s.ReasonCode) {
		return ErrInvalid
	}
	return nil
}

type DocumentState string

const (
	DocumentDraft       DocumentState = "draft"
	DocumentReady       DocumentState = "ready"
	DocumentSigning     DocumentState = "signing"
	DocumentSent        DocumentState = "sent"
	DocumentConfirmed   DocumentState = "confirmed"
	DocumentRejected    DocumentState = "rejected"
	DocumentCorrection  DocumentState = "correction_required"
	DocumentUnknown     DocumentState = "unknown"
)

func (s DocumentState) Valid() bool {
	switch s {
	case DocumentDraft, DocumentReady, DocumentSigning, DocumentSent, DocumentConfirmed, DocumentRejected, DocumentCorrection, DocumentUnknown:
		return true
	default:
		return false
	}
}

type Document struct {
	ID             string        `json:"id"`
	FormatVersion  string        `json:"format_version"`
	Kind           string        `json:"kind"`
	CounterpartyRef string       `json:"counterparty_ref"`
	State          DocumentState `json:"state"`
	ArtifactRef    string        `json:"artifact_ref"`
	SignatureRef   string        `json:"signature_ref,omitempty"`
	MChDRef        string        `json:"mchd_ref,omitempty"`
	RemoteID       string        `json:"remote_id,omitempty"`
	Version        int64         `json:"version"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func (d Document) Validate() error {
	if !validRef(d.ID) || d.FormatVersion != "5.03" || !validRef(d.Kind) || !validRef(d.CounterpartyRef) || !d.State.Valid() || !validRef(d.ArtifactRef) || d.Version < 1 || !validTime(d.CreatedAt) || !validTime(d.UpdatedAt) {
		return ErrInvalid
	}
	for _, value := range []string{d.SignatureRef, d.MChDRef, d.RemoteID} {
		if value != "" && !validRef(value) {
			return ErrInvalid
		}
	}
	return nil
}

type RemoteObservation struct {
	ID             string    `json:"id"`
	EntityType     string    `json:"entity_type"`
	EntityRef      string    `json:"entity_ref"`
	RemoteStatus   string    `json:"remote_status"`
	RemoteRequestID string   `json:"remote_request_id,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

func (o RemoteObservation) Validate() error {
	if !validRef(o.ID) || !validRef(o.EntityType) || !validRef(o.EntityRef) || !validRef(o.RemoteStatus) || !validTime(o.ObservedAt) {
		return ErrInvalid
	}
	if o.RemoteRequestID != "" && !validRef(o.RemoteRequestID) {
		return ErrInvalid
	}
	return nil
}

type DriftType string

const (
	DriftStatus       DriftType = "status"
	DriftQuantity     DriftType = "quantity"
	DriftComposition  DriftType = "package_composition"
	DriftUnknownWrite DriftType = "unknown_write_result"
	DriftMissing      DriftType = "missing_remote_observation"
)

func (d DriftType) Valid() bool { return d == DriftStatus || d == DriftQuantity || d == DriftComposition || d == DriftUnknownWrite || d == DriftMissing }

type Drift struct {
	ID         string    `json:"id"`
	EntityType string    `json:"entity_type"`
	EntityRef  string    `json:"entity_ref"`
	Kind       DriftType `json:"kind"`
	Expected   string    `json:"expected"`
	Observed   string    `json:"observed"`
	Resolved   bool      `json:"resolved"`
	ObservedAt time.Time `json:"observed_at"`
}

func (d Drift) Validate() error {
	if !validRef(d.ID) || !validRef(d.EntityType) || !validRef(d.EntityRef) || !d.Kind.Valid() || !validRef(d.Expected) || !validRef(d.Observed) || !validTime(d.ObservedAt) {
		return ErrInvalid
	}
	return nil
}

// SortFingerprints returns a deterministic set suitable for idempotency and
// package composition digests.
func SortFingerprints(values []string) ([]string, error) {
	result := append([]string(nil), values...)
	for _, value := range result {
		if !validDigest(value) {
			return nil, ErrInvalid
		}
	}
	sort.Strings(result)
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, ErrConflict
		}
	}
	return result, nil
}

// RawCodeHandle points to a short-lived protected artifact. It is safe to
// persist the reference and expiry, never its content.
type RawCodeHandle struct {
	ArtifactRef string    `json:"artifact_ref"`
	Digest      string    `json:"digest"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (h RawCodeHandle) Validate(now time.Time) error {
	if !validRef(h.ArtifactRef) || !validDigest(h.Digest) || !validTime(h.ExpiresAt) || !validTime(now) || !h.ExpiresAt.After(now) {
		return ErrInvalid
	}
	return nil
}

// RawCodeStore is intentionally callback-shaped: implementations must limit
// the lifetime and never expose plaintext to persistence, events or logs.
type RawCodeStore interface {
	Put([]byte, time.Duration) (RawCodeHandle, error)
	Use(RawCodeHandle, func([]byte) error) error
	Delete(RawCodeHandle) error
}
