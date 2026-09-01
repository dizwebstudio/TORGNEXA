package marketplaceoperations

import "time"

// FindingObservation is the bounded input used by reconciliation adapters to
// classify an operational problem. It contains only already-redacted values;
// provider payloads and credentials cannot be passed through this type.
type FindingObservation struct {
	ID             string
	OrganizationID string
	WorkspaceID    string
	FlowID         string
	AccountID      string
	Stage          FlowStage
	EntityKind     string
	EntityID       string
	Expected       string
	Observed       string
	EvidenceDigest string
	DetectedAt     time.Time
	Severity       FindingSeverity

	UnknownRemoteOutcome bool
	DeadLetter           bool
	MarketplaceHealth    bool
	MissingMapping       bool
	DuplicateOrder       bool
	PriceStockMismatch   bool
	StaleData            bool
	PartialResponse      bool
	StatusDrift          bool
}

// Classify returns one stable finding kind. The order is intentional: an
// unknown result and a dead letter are more urgent than secondary symptoms
// such as stale data or a status mismatch.
func (observation FindingObservation) Classify() FindingKind {
	switch {
	case observation.UnknownRemoteOutcome:
		return FindingUnknownRemoteOutcome
	case observation.DeadLetter:
		return FindingDeadLetter
	case observation.MarketplaceHealth:
		return FindingMarketplaceHealth
	case observation.MissingMapping:
		return FindingMissingMapping
	case observation.DuplicateOrder:
		return FindingDuplicateOrder
	case observation.PriceStockMismatch:
		return FindingPriceStockMismatch
	case observation.StaleData:
		return FindingStaleData
	case observation.PartialResponse:
		return FindingPartialResponse
	case observation.StatusDrift:
		return FindingStatusDrift
	default:
		return ""
	}
}

// BuildFinding converts a safe observation to an append-only open finding.
// The caller persists it through FindingRepository; repeated IDs are
// conflicts, never updates.
func BuildFinding(observation FindingObservation) (Finding, error) {
	kind := observation.Classify()
	if kind == "" {
		return Finding{}, ErrInvalidFinding
	}
	if observation.Severity == "" {
		observation.Severity = FindingWarn
	}
	reason := string(kind)
	finding := Finding{
		ID: observation.ID, OrganizationID: observation.OrganizationID, WorkspaceID: observation.WorkspaceID,
		FlowID: observation.FlowID, AccountID: observation.AccountID, Stage: observation.Stage,
		Kind: kind, EntityKind: observation.EntityKind, EntityID: observation.EntityID,
		Severity: observation.Severity, Status: FindingOpen, ReasonCode: reason,
		Expected: observation.Expected, Observed: observation.Observed,
		EvidenceDigest: observation.EvidenceDigest, DetectedAt: observation.DetectedAt,
	}
	if finding.Validate() != nil {
		return Finding{}, ErrInvalidFinding
	}
	return finding, nil
}
