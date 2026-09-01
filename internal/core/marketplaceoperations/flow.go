package marketplaceoperations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

// FlowStage is the ordered cross-domain workflow. The flow stores references
// to canonical domain records; it does not duplicate an order, shipment,
// return, settlement or P&L aggregate.
type FlowStage string

const (
	StageAccount        FlowStage = "account"
	StageProduct        FlowStage = "product"
	StagePublication    FlowStage = "publication"
	StagePricing        FlowStage = "pricing"
	StageInventory      FlowStage = "inventory"
	StageOrder          FlowStage = "order"
	StageReservation    FlowStage = "reservation"
	StagePickPack       FlowStage = "pick_pack"
	StageShipment       FlowStage = "shipment"
	StageReturn         FlowStage = "return"
	StageSettlement     FlowStage = "settlement"
	StageProfitability  FlowStage = "profitability"
	StageReconciliation FlowStage = "reconciliation"
	StageComplete       FlowStage = "complete"
)

// FlowState is the state of the most recently attempted stage. Unknown is
// deliberately recoverable only through a later observation of that same
// stage. Blocked requires an operator or policy decision.
type FlowState string

const (
	FlowPending  FlowState = "pending"
	FlowUnknown  FlowState = "unknown"
	FlowBlocked  FlowState = "blocked"
	FlowComplete FlowState = "complete"
)

// Valid reports whether the state is part of the workflow contract.
func (state FlowState) Valid() bool {
	return state == FlowPending || state == FlowUnknown || state == FlowBlocked || state == FlowComplete
}

// Outcome is the normalized result of one local or remote operation.
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeRejected  Outcome = "rejected"
	OutcomeUnknown   Outcome = "unknown"
)

// Valid reports whether the outcome is normalized.
func (outcome Outcome) Valid() bool {
	return outcome == OutcomeSucceeded || outcome == OutcomeRejected || outcome == OutcomeUnknown
}

var (
	ErrInvalidFlow       = errors.New("marketplace operations: invalid flow")
	ErrInvalidTransition = errors.New("marketplace operations: invalid stage transition")
	ErrDuplicateConflict = errors.New("marketplace operations: idempotency conflict")
	ErrFlowNotFound      = errors.New("marketplace operations: flow not found")
	ErrFlowConflict      = errors.New("marketplace operations: flow conflict")
)

var flowReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var flowDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var flowStages = []FlowStage{
	StageAccount,
	StageProduct,
	StagePublication,
	StagePricing,
	StageInventory,
	StageOrder,
	StageReservation,
	StagePickPack,
	StageShipment,
	StageReturn,
	StageSettlement,
	StageProfitability,
	StageReconciliation,
	StageComplete,
}

// Valid reports whether the stage belongs to the stable workflow contract.
func (stage FlowStage) Valid() bool {
	return validFlowStage(stage)
}

// Reference identifies a canonical record owned by another bounded context.
type Reference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (reference Reference) validate() error {
	if !flowReferencePattern.MatchString(reference.Kind) || !flowReferencePattern.MatchString(reference.ID) {
		return ErrInvalidFlow
	}
	return nil
}

// Validate checks a cross-domain reference before it is retained by a flow.
func (reference Reference) Validate() error {
	return reference.validate()
}

// Flow is a tenant-scoped orchestration projection for the marketplace
// scenario. It is safe to rebuild from domain events and never becomes the
// source of truth for the referenced records.
type Flow struct {
	ID                 string      `json:"id"`
	OrganizationID     string      `json:"organization_id"`
	WorkspaceID        string      `json:"workspace_id"`
	AccountID          string      `json:"account_id"`
	Stage              FlowStage   `json:"stage"`
	State              FlowState   `json:"state"`
	Version            int64       `json:"version"`
	LastOperationID    string      `json:"last_operation_id,omitempty"`
	LastIdempotencyKey string      `json:"last_idempotency_key,omitempty"`
	LastReasonCode     string      `json:"last_reason_code,omitempty"`
	LastCommandDigest  string      `json:"-"`
	References         []Reference `json:"references,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

// Command advances one stage or records an unknown/rejected outcome. The
// application layer persists the command through an inbox/outbox transaction.
type Command struct {
	OperationID    string      `json:"operation_id"`
	IdempotencyKey string      `json:"idempotency_key"`
	Stage          FlowStage   `json:"stage"`
	Outcome        Outcome     `json:"outcome"`
	ReasonCode     string      `json:"reason_code,omitempty"`
	References     []Reference `json:"references,omitempty"`
	OccurredAt     time.Time   `json:"occurred_at"`
}

// New creates a flow before the first account operation is attempted.
func New(id, organizationID, workspaceID, accountID string, at time.Time) (Flow, error) {
	flow := Flow{ID: id, OrganizationID: organizationID, WorkspaceID: workspaceID, AccountID: accountID, Stage: StageAccount, State: FlowPending, Version: 1, CreatedAt: at.UTC(), UpdatedAt: at.UTC()}
	if flow.validate() != nil {
		return Flow{}, ErrInvalidFlow
	}
	return flow, nil
}

// Apply applies a normalized command and returns whether it was an immediate
// idempotent duplicate. A duplicate with a different payload is rejected.
func Apply(flow Flow, command Command) (Flow, bool, error) {
	if flow.validate() != nil || command.validate() != nil {
		return Flow{}, false, ErrInvalidFlow
	}
	digest := commandDigest(command)
	if flow.LastIdempotencyKey == command.IdempotencyKey {
		if flow.LastOperationID == command.OperationID && flow.LastCommandDigest == digest {
			return flow, true, nil
		}
		return Flow{}, false, ErrDuplicateConflict
	}
	if flow.Stage == StageComplete || flow.State == FlowBlocked {
		return Flow{}, false, ErrInvalidTransition
	}
	if command.Stage != flow.Stage {
		return Flow{}, false, ErrInvalidTransition
	}
	if command.OccurredAt.Before(flow.CreatedAt) {
		return Flow{}, false, ErrInvalidFlow
	}
	if err := ValidateCommandReferences(command); err != nil {
		return Flow{}, false, err
	}

	flow.LastOperationID = command.OperationID
	flow.LastIdempotencyKey = command.IdempotencyKey
	flow.LastReasonCode = command.ReasonCode
	flow.LastCommandDigest = digest
	flow.Version++
	flow.UpdatedAt = command.OccurredAt.UTC()
	flow.References = mergeReferences(flow.References, command.References)
	if len(flow.References) > 64 {
		return Flow{}, false, ErrInvalidFlow
	}
	switch command.Outcome {
	case OutcomeUnknown:
		flow.State = FlowUnknown
	case OutcomeRejected:
		flow.State = FlowBlocked
	case OutcomeSucceeded:
		flow.State = FlowPending
		flow.Stage = nextStage(flow.Stage)
		if flow.Stage == StageComplete {
			flow.State = FlowComplete
		}
	}
	return flow, false, nil
}

func (command Command) validate() error {
	if !flowReferencePattern.MatchString(command.OperationID) || !flowReferencePattern.MatchString(command.IdempotencyKey) || !command.Stage.Valid() || !command.Outcome.Valid() || command.OccurredAt.IsZero() || command.OccurredAt.Location() != time.UTC {
		return ErrInvalidFlow
	}
	if command.ReasonCode != "" && !flowReferencePattern.MatchString(command.ReasonCode) {
		return ErrInvalidFlow
	}
	if len(command.References) > 64 {
		return ErrInvalidFlow
	}
	for _, reference := range command.References {
		if reference.validate() != nil {
			return ErrInvalidFlow
		}
	}
	return nil
}

// Validate checks a workflow command before it crosses the application
// boundary.
func (command Command) Validate() error {
	return command.validate()
}

func (flow Flow) validate() error {
	if !flowReferencePattern.MatchString(flow.ID) || !flowReferencePattern.MatchString(flow.OrganizationID) || !flowReferencePattern.MatchString(flow.WorkspaceID) || !flowReferencePattern.MatchString(flow.AccountID) || !flow.Stage.Valid() || !flow.State.Valid() || flow.Version < 1 || flow.CreatedAt.IsZero() || flow.UpdatedAt.IsZero() || flow.CreatedAt.Location() != time.UTC || flow.UpdatedAt.Location() != time.UTC || flow.UpdatedAt.Before(flow.CreatedAt) || len(flow.References) > 64 {
		return ErrInvalidFlow
	}
	if flow.LastOperationID != "" && !flowReferencePattern.MatchString(flow.LastOperationID) || flow.LastIdempotencyKey != "" && !flowReferencePattern.MatchString(flow.LastIdempotencyKey) || flow.LastReasonCode != "" && !flowReferencePattern.MatchString(flow.LastReasonCode) || flow.LastCommandDigest != "" && !flowDigestPattern.MatchString(flow.LastCommandDigest) {
		return ErrInvalidFlow
	}
	if flow.Stage == StageComplete && flow.State != FlowComplete {
		return ErrInvalidFlow
	}
	seen := make(map[string]struct{}, len(flow.References))
	for _, reference := range flow.References {
		if reference.validate() != nil {
			return ErrInvalidFlow
		}
		key := reference.Kind + "\x00" + reference.ID
		if _, ok := seen[key]; ok {
			return ErrInvalidFlow
		}
		seen[key] = struct{}{}
	}
	return nil
}

func commandDigest(command Command) string {
	encoded, _ := json.Marshal(command)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Validate checks the tenant-scoped orchestration projection.
func (flow Flow) Validate() error {
	return flow.validate()
}

func validFlowStage(stage FlowStage) bool {
	for _, candidate := range flowStages {
		if candidate == stage {
			return true
		}
	}
	return false
}

func nextStage(stage FlowStage) FlowStage {
	for index, candidate := range flowStages {
		if candidate == stage && index+1 < len(flowStages) {
			return flowStages[index+1]
		}
	}
	return StageComplete
}

func mergeReferences(existing, added []Reference) []Reference {
	seen := make(map[string]Reference, len(existing)+len(added))
	for _, reference := range append(append([]Reference(nil), existing...), added...) {
		seen[reference.Kind+"\x00"+reference.ID] = reference
	}
	result := make([]Reference, 0, len(seen))
	for _, reference := range seen {
		result = append(result, reference)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].Kind+"\x00"+result[i].ID, result[j].Kind+"\x00"+result[j].ID) < 0
	})
	return result
}

// FlowPage is the bounded, cursor-based read projection used by the
// operations center. The repository returns only canonical references; it
// never returns connector payloads or credentials.
type FlowPage struct {
	Items      []Flow `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// FlowRepository persists the orchestration projection and its idempotent
// command journal. Implementations must apply the supplied tenant scope to
// every read and write transaction.
type FlowRepository interface {
	Create(context.Context, tenancy.Scope, Flow) error
	Flow(context.Context, tenancy.Scope, string) (Flow, error)
	List(context.Context, tenancy.Scope, string, int) (FlowPage, error)
	Apply(context.Context, tenancy.Scope, string, Command) (Flow, bool, error)
}
