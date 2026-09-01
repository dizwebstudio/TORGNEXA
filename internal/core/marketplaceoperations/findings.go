package marketplaceoperations

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

// FindingKind is the normalized reason why a marketplace workflow needs
// operator attention. It deliberately excludes provider-specific payloads.
type FindingKind string

const (
	FindingUnknownRemoteOutcome FindingKind = "unknown_remote_outcome"
	FindingStaleData            FindingKind = "stale_data"
	FindingMissingMapping       FindingKind = "missing_mapping"
	FindingDuplicateOrder       FindingKind = "duplicate_order"
	FindingPriceStockMismatch   FindingKind = "price_stock_mismatch"
	FindingMarketplaceHealth    FindingKind = "marketplace_health"
	FindingDeadLetter           FindingKind = "dead_letter"
	FindingPartialResponse      FindingKind = "partial_response"
	FindingStatusDrift          FindingKind = "status_drift"
)

func (kind FindingKind) Valid() bool {
	switch kind {
	case FindingUnknownRemoteOutcome, FindingStaleData, FindingMissingMapping,
		FindingDuplicateOrder, FindingPriceStockMismatch, FindingMarketplaceHealth,
		FindingDeadLetter, FindingPartialResponse, FindingStatusDrift:
		return true
	default:
		return false
	}
}

// FindingSeverity controls the operator treatment of a finding.
type FindingSeverity string

const (
	FindingInfo  FindingSeverity = "info"
	FindingWarn  FindingSeverity = "warn"
	FindingBlock FindingSeverity = "block"
)

func (severity FindingSeverity) Valid() bool {
	return severity == FindingInfo || severity == FindingWarn || severity == FindingBlock
}

// FindingStatus is derived from the append-only action journal. A finding is
// never silently deleted or rewritten when a remote state changes.
type FindingStatus string

const (
	FindingOpen     FindingStatus = "open"
	FindingResolved FindingStatus = "resolved"
)

func (status FindingStatus) Valid() bool {
	return status == FindingOpen || status == FindingResolved
}

// Finding is a tenant-scoped, provider-neutral reconciliation result. Expected
// and observed values are bounded strings or digests; raw responses and tokens
// are never valid fields.
type Finding struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organization_id"`
	WorkspaceID    string          `json:"workspace_id"`
	FlowID         string          `json:"flow_id"`
	AccountID      string          `json:"account_id"`
	Stage          FlowStage       `json:"stage"`
	Kind           FindingKind     `json:"kind"`
	EntityKind     string          `json:"entity_kind"`
	EntityID       string          `json:"entity_id"`
	Severity       FindingSeverity `json:"severity"`
	Status         FindingStatus   `json:"status"`
	ReasonCode     string          `json:"reason_code"`
	Expected       string          `json:"expected,omitempty"`
	Observed       string          `json:"observed,omitempty"`
	EvidenceDigest string          `json:"evidence_digest,omitempty"`
	DetectedAt     time.Time       `json:"detected_at"`
}

var findingTextPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var findingDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var (
	ErrInvalidFinding  = errors.New("marketplace operations: invalid finding")
	ErrFindingConflict = errors.New("marketplace operations: finding conflict")
)

// Validate checks a finding before it crosses a persistence or API boundary.
func (finding Finding) Validate() error {
	if !findingTextPattern.MatchString(finding.ID) || !findingTextPattern.MatchString(finding.OrganizationID) || !findingTextPattern.MatchString(finding.WorkspaceID) || !findingTextPattern.MatchString(finding.FlowID) || !findingTextPattern.MatchString(finding.AccountID) || !finding.Stage.Valid() || !finding.Kind.Valid() || !findingTextPattern.MatchString(finding.EntityKind) || !findingTextPattern.MatchString(finding.EntityID) || !finding.Severity.Valid() || !finding.Status.Valid() || !findingTextPattern.MatchString(finding.ReasonCode) || len(finding.Expected) > 2000 || len(finding.Observed) > 2000 || (finding.EvidenceDigest != "" && !findingDigestPattern.MatchString(finding.EvidenceDigest)) || finding.DetectedAt.IsZero() || finding.DetectedAt.Location() != time.UTC {
		return ErrInvalidFinding
	}
	if finding.Status != FindingOpen {
		return ErrInvalidFinding
	}
	return nil
}

// FindingActionKind is the only operator command admitted at the operations
// center. Retry and reconcile are durable intents; workers perform remote
// effects only after re-checking capability, approval and mapping.
type FindingActionKind string

const (
	FindingActionRetry     FindingActionKind = "retry"
	FindingActionReconcile FindingActionKind = "reconcile"
	FindingActionResolve   FindingActionKind = "resolve"
)

func (kind FindingActionKind) Valid() bool {
	return kind == FindingActionRetry || kind == FindingActionReconcile || kind == FindingActionResolve
}

// FindingAction is append-only evidence of an operator decision.
type FindingAction struct {
	ID             string            `json:"id"`
	FindingID      string            `json:"finding_id"`
	Action         FindingActionKind `json:"action"`
	IdempotencyKey string            `json:"idempotency_key"`
	ActorID        string            `json:"actor_id"`
	OccurredAt     time.Time         `json:"occurred_at"`
}

func (action FindingAction) Validate() error {
	if !findingTextPattern.MatchString(action.ID) || !findingTextPattern.MatchString(action.FindingID) || !action.Action.Valid() || !findingTextPattern.MatchString(action.IdempotencyKey) || !findingTextPattern.MatchString(action.ActorID) || action.OccurredAt.IsZero() || action.OccurredAt.Location() != time.UTC {
		return ErrInvalidFinding
	}
	return nil
}

// FindingQuery bounds an operations-center read. Flow and status filters are
// optional but always remain tenant-scoped in repository implementations.
type FindingQuery struct {
	Cursor string
	Limit  int
	FlowID string
	Status FindingStatus
}

func (query FindingQuery) Validate() error {
	if query.Limit < 1 || query.Limit > 100 || len(query.Cursor) > 512 || (query.FlowID != "" && !findingTextPattern.MatchString(query.FlowID)) || (query.Status != "" && !query.Status.Valid()) {
		return ErrInvalidFinding
	}
	return nil
}

// FindingPage is the cursor-based read model for operations center clients.
type FindingPage struct {
	Items      []Finding `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// FindingRepository is the persistence boundary for append-only findings and
// manual action journal.
type FindingRepository interface {
	RecordFinding(context.Context, tenancy.Scope, Finding) error
	Finding(context.Context, tenancy.Scope, string) (Finding, error)
	ListFindings(context.Context, tenancy.Scope, FindingQuery) (FindingPage, error)
	ApplyFindingAction(context.Context, tenancy.Scope, string, FindingAction) (FindingAction, bool, error)
}
