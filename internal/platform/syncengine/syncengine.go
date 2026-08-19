// Package syncengine defines the provider-neutral durable bidirectional sync
// orchestration boundary. Domain-specific adapters translate canonical entities
// to/from RemoteMutation and LocalMutation; this package owns direction,
// authority, mapping, checkpoint, idempotency and loop-prevention semantics.
package syncengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

var (
	ErrInvalidRecord      = errors.New("syncengine: invalid record")
	ErrPolicyNotFound     = errors.New("syncengine: policy not found")
	ErrPolicyConflict     = errors.New("syncengine: policy version conflict")
	ErrDirectionDisabled  = errors.New("syncengine: direction disabled")
	ErrReceiptCollision   = errors.New("syncengine: receipt collision")
	ErrStateConflict      = errors.New("syncengine: state version conflict")
	ErrCheckpointConflict = errors.New("syncengine: checkpoint version conflict")
	ErrConflict           = errors.New("syncengine: concurrent local and remote change")
	ErrRemoteConflict     = errors.New("syncengine: remote optimistic conflict")
)

var (
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	entityPattern   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	revisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=-]{0,255}$`)
	cursorPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=-]*$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const MaxPayloadBytes = 1 << 20

type Direction string

const (
	DirectionInbound       Direction = "inbound"
	DirectionOutbound      Direction = "outbound"
	DirectionBidirectional Direction = "bidirectional"
)

func (d Direction) Valid() bool {
	return d == DirectionInbound || d == DirectionOutbound || d == DirectionBidirectional
}
func (d Direction) AllowsInbound() bool { return d == DirectionInbound || d == DirectionBidirectional }
func (d Direction) AllowsOutbound() bool {
	return d == DirectionOutbound || d == DirectionBidirectional
}

type SourceOfTruth string

const (
	SourceLocal  SourceOfTruth = "local"
	SourceRemote SourceOfTruth = "remote"
	SourceManual SourceOfTruth = "manual"
)

func (s SourceOfTruth) Valid() bool {
	return s == SourceLocal || s == SourceRemote || s == SourceManual
}

type Operation string

const (
	OperationUpsert Operation = "upsert"
	OperationDelete Operation = "delete"
)

func (o Operation) Valid() bool { return o == OperationUpsert || o == OperationDelete }

type Outcome string

const (
	OutcomeApplied         Outcome = "applied"
	OutcomeDuplicate       Outcome = "duplicate"
	OutcomeLoopSuppressed  Outcome = "loop_suppressed"
	OutcomeStaleSuppressed Outcome = "stale_suppressed"
	OutcomeLocalWins       Outcome = "conflict_local_wins"
)

func (o Outcome) Valid() bool {
	switch o {
	case OutcomeApplied, OutcomeDuplicate, OutcomeLoopSuppressed, OutcomeStaleSuppressed, OutcomeLocalWins:
		return true
	default:
		return false
	}
}

// Policy selects one connector account + canonical entity type. Direction and
// SourceOfTruth are intentionally independent: source-of-truth is consulted
// only when both sides changed since the last successful synchronization.
type Policy struct {
	ID                 string
	OrganizationID     string
	WorkspaceID        string
	ConnectorAccountID string
	EntityType         string
	Direction          Direction
	SourceOfTruth      SourceOfTruth
	Enabled            bool
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (p Policy) Validate() error {
	accountOK := idPattern.MatchString(p.ConnectorAccountID)
	if !idPattern.MatchString(p.ID) || !idPattern.MatchString(p.OrganizationID) || !idPattern.MatchString(p.WorkspaceID) ||
		!accountOK || !entityPattern.MatchString(p.EntityType) || !p.Direction.Valid() ||
		!p.SourceOfTruth.Valid() || p.Version < 1 || !utc(p.CreatedAt) || !utc(p.UpdatedAt) || p.UpdatedAt.Before(p.CreatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type PolicyCreate struct {
	ID                 string
	ConnectorAccountID string
	EntityType         string
	Direction          Direction
	SourceOfTruth      SourceOfTruth
	Enabled            bool
}

func (c PolicyCreate) Validate() error {
	accountOK := idPattern.MatchString(c.ConnectorAccountID)
	if !idPattern.MatchString(c.ID) || !accountOK || !entityPattern.MatchString(c.EntityType) || !c.Direction.Valid() || !c.SourceOfTruth.Valid() {
		return ErrInvalidRecord
	}
	return nil
}

type PolicyUpdate struct {
	ID              string
	Direction       Direction
	SourceOfTruth   SourceOfTruth
	Enabled         bool
	ExpectedVersion int64
}

func (u PolicyUpdate) Validate() error {
	if !idPattern.MatchString(u.ID) || !u.Direction.Valid() || !u.SourceOfTruth.Valid() || u.ExpectedVersion < 1 {
		return ErrInvalidRecord
	}
	return nil
}

type Checkpoint struct {
	PolicyID  string
	Cursor    string
	Version   int64
	UpdatedAt time.Time
}

func (c Checkpoint) Validate() error {
	if !idPattern.MatchString(c.PolicyID) || c.Version < 1 || !utc(c.UpdatedAt) || (c.Cursor != "" && (!cursorPattern.MatchString(c.Cursor) || !safeText(c.Cursor, 1024))) {
		return ErrInvalidRecord
	}
	return nil
}

type EntityState struct {
	PolicyID              string
	LocalEntityID         string
	RemoteID              string
	LastLocalVersion      int64
	LastRemoteRevision    string
	LastSyncedFingerprint string
	LastLocalEventID      string
	LastRemoteChangeID    string
	Version               int64
	UpdatedAt             time.Time
}

func (s EntityState) Validate() error {
	if !idPattern.MatchString(s.PolicyID) || !idPattern.MatchString(s.LocalEntityID) || !safeRemoteID(s.RemoteID) || s.LastLocalVersion < 1 ||
		!revisionPattern.MatchString(s.LastRemoteRevision) || !digestPattern.MatchString(s.LastSyncedFingerprint) ||
		!idPattern.MatchString(s.LastLocalEventID) || !optionalID(s.LastRemoteChangeID) || s.Version < 1 || !utc(s.UpdatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Receipt struct {
	PolicyID    string
	ChangeID    string
	Fingerprint string
	Outcome     Outcome
	CreatedAt   time.Time
}

func (r Receipt) Validate() error {
	if !idPattern.MatchString(r.PolicyID) || !idPattern.MatchString(r.ChangeID) || !digestPattern.MatchString(r.Fingerprint) || !r.Outcome.Valid() || !utc(r.CreatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

// Repository persists sync control-plane and replay state. Implementations must
// be tenant scoped. Record*Receipt is insert-only: same id+fingerprint is a
// duplicate, same id+different fingerprint is ErrReceiptCollision.
type Repository interface {
	CreatePolicy(context.Context, tenancy.Scope, PolicyCreate) (Policy, error)
	UpdatePolicy(context.Context, tenancy.Scope, PolicyUpdate) (Policy, error)
	Policy(context.Context, tenancy.Scope, string) (Policy, error)
	Checkpoint(context.Context, tenancy.Scope, string) (Checkpoint, error)
	AdvanceCheckpoint(context.Context, tenancy.Scope, string, int64, string, time.Time) (Checkpoint, error)
	EntityState(context.Context, tenancy.Scope, string, string) (EntityState, error)
	SaveEntityState(context.Context, tenancy.Scope, EntityState, int64) (EntityState, error)
	LocalReceipt(context.Context, tenancy.Scope, string, string) (Receipt, error)
	RecordLocalReceipt(context.Context, tenancy.Scope, Receipt) error
	RemoteReceipt(context.Context, tenancy.Scope, string, string) (Receipt, error)
	RecordRemoteReceipt(context.Context, tenancy.Scope, Receipt) error
}

// ErrNotFound is used for optional repository state/receipt lookups.
var ErrNotFound = errors.New("syncengine: not found")

// LocalMutation is the canonical local change obtained from a domain event.
// Payload is provider-neutral canonical JSON, not a provider request body.
type LocalMutation struct {
	EventID       string
	EntityType    string
	LocalEntityID string
	LocalVersion  int64
	Operation     Operation
	Payload       json.RawMessage
	Source        string
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
}

func (m LocalMutation) Validate() error {
	if !idPattern.MatchString(m.EventID) || !entityPattern.MatchString(m.EntityType) || !idPattern.MatchString(m.LocalEntityID) || m.LocalVersion < 1 ||
		!m.Operation.Valid() || !validPayload(m.Payload) || !safeSource(m.Source) || !optionalID(m.CorrelationID) || !optionalID(m.CausationID) || !utc(m.OccurredAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Origin struct {
	Source        string
	EventID       string
	CorrelationID string
	CausationID   string
}

func (o Origin) Validate() error {
	if o.Source == "" && o.EventID == "" && o.CorrelationID == "" && o.CausationID == "" {
		return nil
	}
	if (o.Source != "" && !safeSource(o.Source)) || !optionalID(o.EventID) || !optionalID(o.CorrelationID) || !optionalID(o.CausationID) {
		return ErrInvalidRecord
	}
	return nil
}

type RemoteMutation struct {
	ChangeID   string
	EntityType string
	RemoteID   string
	Revision   string
	Operation  Operation
	Payload    json.RawMessage
	OccurredAt time.Time
	Origin     Origin
}

func (m RemoteMutation) Validate() error {
	if !idPattern.MatchString(m.ChangeID) || !entityPattern.MatchString(m.EntityType) || !safeRemoteID(m.RemoteID) || !revisionPattern.MatchString(m.Revision) ||
		!m.Operation.Valid() || !validPayload(m.Payload) || !utc(m.OccurredAt) || m.Origin.Validate() != nil {
		return ErrInvalidRecord
	}
	return nil
}

type RemotePage struct {
	Changes    []RemoteMutation
	NextCursor string
	HasMore    bool
}

func (p RemotePage) Validate(limit int) error {
	if limit < 1 || limit > 1000 || len(p.Changes) > limit || (p.NextCursor != "" && (!cursorPattern.MatchString(p.NextCursor) || !safeText(p.NextCursor, 1024))) {
		return ErrInvalidRecord
	}
	if p.HasMore && p.NextCursor == "" {
		return ErrInvalidRecord
	}
	for i := range p.Changes {
		if err := p.Changes[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

type PullRequest struct {
	ConnectorAccountID string
	EntityType         string
	Cursor             string
	Limit              int
}

type RemoteReader interface {
	Pull(context.Context, tenancy.Scope, PullRequest) (RemotePage, error)
}

type PropagationMetadata struct {
	IdempotencyKey string
	Source         string
	CorrelationID  string
	CausationID    string
	OriginEventID  string
}

func (m PropagationMetadata) Validate() error {
	if !idPattern.MatchString(m.IdempotencyKey) || !safeSource(m.Source) || !idPattern.MatchString(m.CorrelationID) || !idPattern.MatchString(m.CausationID) || !idPattern.MatchString(m.OriginEventID) {
		return ErrInvalidRecord
	}
	return nil
}

type RemoteApplyRequest struct {
	ConnectorAccountID     string
	EntityType             string
	LocalEntityID          string
	RemoteID               string
	Operation              Operation
	Payload                json.RawMessage
	ExpectedRemoteRevision string
	Force                  bool
	Metadata               PropagationMetadata
}

func (r RemoteApplyRequest) Validate() error {
	accountOK := idPattern.MatchString(r.ConnectorAccountID)
	if !accountOK || !entityPattern.MatchString(r.EntityType) || !idPattern.MatchString(r.LocalEntityID) ||
		(r.RemoteID != "" && !safeRemoteID(r.RemoteID)) || !r.Operation.Valid() || !validPayload(r.Payload) ||
		(r.ExpectedRemoteRevision != "" && !revisionPattern.MatchString(r.ExpectedRemoteRevision)) || r.Metadata.Validate() != nil {
		return ErrInvalidRecord
	}
	return nil
}

type RemoteApplyResult struct {
	RemoteID string
	Revision string
}

func (r RemoteApplyResult) Validate() error {
	if !safeRemoteID(r.RemoteID) || !revisionPattern.MatchString(r.Revision) {
		return ErrInvalidRecord
	}
	return nil
}

type RemoteWriter interface {
	ApplyLocal(context.Context, tenancy.Scope, RemoteApplyRequest) (RemoteApplyResult, error)
}

// RemoteConflict carries a bounded current revision only; raw remote error text
// must never be persisted or returned as sync evidence.
type RemoteConflict struct{ CurrentRevision string }

func (e *RemoteConflict) Error() string { return ErrRemoteConflict.Error() }
func (e *RemoteConflict) Unwrap() error { return ErrRemoteConflict }
func (e *RemoteConflict) Validate() error {
	if e == nil || !revisionPattern.MatchString(e.CurrentRevision) {
		return ErrInvalidRecord
	}
	return nil
}

type LocalSnapshot struct {
	LocalEntityID string
	Version       int64
	Fingerprint   string
}

func (s LocalSnapshot) Validate() error {
	if !idPattern.MatchString(s.LocalEntityID) || s.Version < 1 || !digestPattern.MatchString(s.Fingerprint) {
		return ErrInvalidRecord
	}
	return nil
}

type LocalApplyRequest struct {
	EntityType           string
	LocalEntityID        string // empty means create and return a canonical id
	RemoteID             string
	Operation            Operation
	Payload              json.RawMessage
	ExpectedLocalVersion int64 // 0 only for create
	Overwrite            bool
	EventID              string
	Source               string
	CorrelationID        string
	CausationID          string
	IdempotencyKey       string
	OccurredAt           time.Time
}

func (r LocalApplyRequest) Validate() error {
	if !entityPattern.MatchString(r.EntityType) || (r.LocalEntityID != "" && !idPattern.MatchString(r.LocalEntityID)) || !safeRemoteID(r.RemoteID) ||
		!r.Operation.Valid() || !validPayload(r.Payload) || r.ExpectedLocalVersion < 0 || !idPattern.MatchString(r.EventID) || !safeSource(r.Source) ||
		!idPattern.MatchString(r.CorrelationID) || !idPattern.MatchString(r.CausationID) || !idPattern.MatchString(r.IdempotencyKey) || !utc(r.OccurredAt) {
		return ErrInvalidRecord
	}
	if r.LocalEntityID == "" && r.ExpectedLocalVersion != 0 {
		return ErrInvalidRecord
	}
	if r.LocalEntityID != "" && r.ExpectedLocalVersion < 1 {
		return ErrInvalidRecord
	}
	return nil
}

type LocalApplyResult struct {
	LocalEntityID string
	Version       int64
	Fingerprint   string
}

func (r LocalApplyResult) Validate() error {
	if !idPattern.MatchString(r.LocalEntityID) || r.Version < 1 || !digestPattern.MatchString(r.Fingerprint) {
		return ErrInvalidRecord
	}
	return nil
}

type LocalEndpoint interface {
	Snapshot(context.Context, tenancy.Scope, string, string) (LocalSnapshot, error)
	ApplyRemote(context.Context, tenancy.Scope, LocalApplyRequest) (LocalApplyResult, error)
}

type Result struct {
	Outcome        Outcome
	PolicyID       string
	LocalEntityID  string
	RemoteID       string
	LocalVersion   int64
	RemoteRevision string
	CorrelationID  string
	CausationID    string
}

func (r Result) Validate() error {
	if !r.Outcome.Valid() || !idPattern.MatchString(r.PolicyID) || (r.LocalEntityID != "" && !idPattern.MatchString(r.LocalEntityID)) ||
		(r.RemoteID != "" && !safeRemoteID(r.RemoteID)) || r.LocalVersion < 0 || (r.RemoteRevision != "" && !revisionPattern.MatchString(r.RemoteRevision)) ||
		(r.CorrelationID != "" && !idPattern.MatchString(r.CorrelationID)) || (r.CausationID != "" && !idPattern.MatchString(r.CausationID)) {
		return ErrInvalidRecord
	}
	return nil
}

type clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Engine struct {
	repository Repository
	mappings   connectors.MappingRepository
	clock      clock
}

func New(repository Repository, mappings connectors.MappingRepository) (*Engine, error) {
	return newEngine(repository, mappings, systemClock{})
}
func newEngine(repository Repository, mappings connectors.MappingRepository, clk clock) (*Engine, error) {
	if repository == nil || mappings == nil || clk == nil {
		return nil, errors.New("syncengine: repository, mappings and clock are required")
	}
	return &Engine{repository: repository, mappings: mappings, clock: clk}, nil
}

// PushLocal propagates one canonical local mutation to a connector. The remote
// adapter must honor Metadata.IdempotencyKey, so a crash after the remote side
// effect but before local receipt persistence can safely retry.
func (e *Engine) PushLocal(ctx context.Context, scope tenancy.Scope, policyID string, mutation LocalMutation, remote RemoteWriter) (Result, error) {
	if err := e.ready(ctx, scope, remote != nil); err != nil {
		return Result{}, err
	}
	if err := mutation.Validate(); err != nil {
		return Result{}, err
	}
	policy, err := e.loadPolicy(ctx, scope, policyID)
	if err != nil {
		return Result{}, err
	}
	if !policy.Direction.AllowsOutbound() {
		return Result{}, ErrDirectionDisabled
	}
	if policy.EntityType != mutation.EntityType {
		return Result{}, ErrInvalidRecord
	}

	payloadFingerprint, err := PayloadFingerprint(mutation.Payload)
	if err != nil {
		return Result{}, err
	}
	receiptFingerprint, err := LocalMutationFingerprint(mutation)
	if err != nil {
		return Result{}, err
	}
	if receipt, err := e.repository.LocalReceipt(ctx, scope, policy.ID, mutation.EventID); err == nil {
		if receipt.Fingerprint != receiptFingerprint {
			return Result{}, ErrReceiptCollision
		}
		return Result{Outcome: OutcomeDuplicate, PolicyID: policy.ID, LocalEntityID: mutation.LocalEntityID, CorrelationID: correlationForLocal(mutation), CausationID: mutation.EventID}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}

	// Same-policy local events created by PullRemote are never reflected back to
	// that provider. Other policies may still propagate the event cross-channel.
	if mutation.Source == policySource(policy.ID) {
		receipt := Receipt{PolicyID: policy.ID, ChangeID: mutation.EventID, Fingerprint: receiptFingerprint, Outcome: OutcomeLoopSuppressed, CreatedAt: e.now()}
		if err := e.repository.RecordLocalReceipt(ctx, scope, receipt); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeLoopSuppressed, PolicyID: policy.ID, LocalEntityID: mutation.LocalEntityID, CorrelationID: correlationForLocal(mutation), CausationID: mutation.EventID}, nil
	}

	mapping, mappingErr := e.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.ConnectorAccountID, policy.EntityType, mutation.LocalEntityID)
	mappingMissing := errors.Is(mappingErr, connectors.ErrMappingNotFound)
	if mappingErr != nil && !mappingMissing {
		return Result{}, mappingErr
	}
	var state EntityState
	stateFound := false
	if mappingErr == nil {
		state, err = e.repository.EntityState(ctx, scope, policy.ID, mapping.LocalEntityID)
		if err == nil {
			stateFound = true
		} else if !errors.Is(err, ErrNotFound) {
			return Result{}, err
		}
	}
	if stateFound && mutation.LocalVersion <= state.LastLocalVersion {
		receipt := Receipt{PolicyID: policy.ID, ChangeID: mutation.EventID, Fingerprint: receiptFingerprint, Outcome: OutcomeStaleSuppressed, CreatedAt: e.now()}
		if err := e.repository.RecordLocalReceipt(ctx, scope, receipt); err != nil {
			return Result{}, err
		}
		return resultFromState(OutcomeStaleSuppressed, policy.ID, state, correlationForLocal(mutation), mutation.EventID), nil
	}

	correlation := correlationForLocal(mutation)
	metadata := PropagationMetadata{IdempotencyKey: deterministicID("synout", policy.ID, mutation.EventID), Source: policySource(policy.ID), CorrelationID: correlation, CausationID: mutation.EventID, OriginEventID: mutation.EventID}
	request := RemoteApplyRequest{ConnectorAccountID: policy.ConnectorAccountID, EntityType: policy.EntityType, LocalEntityID: mutation.LocalEntityID, Operation: mutation.Operation, Payload: append(json.RawMessage(nil), mutation.Payload...), Metadata: metadata}
	if mappingErr == nil {
		request.RemoteID = mapping.RemoteID
	}
	if stateFound {
		request.ExpectedRemoteRevision = state.LastRemoteRevision
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}

	applied, err := remote.ApplyLocal(ctx, scope, request)
	if err != nil {
		var conflict *RemoteConflict
		if !errors.As(err, &conflict) || conflict.Validate() != nil {
			return Result{}, err
		}
		switch policy.SourceOfTruth {
		case SourceLocal:
			request.Force = true
			request.ExpectedRemoteRevision = conflict.CurrentRevision
			applied, err = remote.ApplyLocal(ctx, scope, request)
			if err != nil {
				return Result{}, err
			}
		case SourceRemote, SourceManual:
			return Result{}, fmt.Errorf("%w: outbound remote revision changed", ErrConflict)
		}
	}
	if err := applied.Validate(); err != nil {
		return Result{}, err
	}

	mapping, err = e.ensureMappingByLocal(ctx, scope, policy, mutation.LocalEntityID, applied.RemoteID, mapping, mappingErr)
	if err != nil {
		return Result{}, err
	}
	expectedStateVersion := int64(0)
	if stateFound {
		expectedStateVersion = state.Version
	}
	next := EntityState{PolicyID: policy.ID, LocalEntityID: mutation.LocalEntityID, RemoteID: applied.RemoteID, LastLocalVersion: mutation.LocalVersion, LastRemoteRevision: applied.Revision, LastSyncedFingerprint: payloadFingerprint, LastLocalEventID: mutation.EventID, LastRemoteChangeID: state.LastRemoteChangeID, UpdatedAt: e.now()}
	next, err = e.repository.SaveEntityState(ctx, scope, next, expectedStateVersion)
	if err != nil {
		return Result{}, err
	}
	receipt := Receipt{PolicyID: policy.ID, ChangeID: mutation.EventID, Fingerprint: receiptFingerprint, Outcome: OutcomeApplied, CreatedAt: e.now()}
	if err := e.repository.RecordLocalReceipt(ctx, scope, receipt); err != nil {
		return Result{}, err
	}
	return resultFromState(OutcomeApplied, policy.ID, next, correlation, mutation.EventID), nil
}

// PullRemote pulls one page and advances the durable cursor only after every
// change in that page is resolved. If the process crashes before checkpoint
// commit, the page is replayed and remote receipts + idempotent LocalEndpoint
// application make it safe.
func (e *Engine) PullRemote(ctx context.Context, scope tenancy.Scope, policyID string, reader RemoteReader, local LocalEndpoint, limit int) ([]Result, error) {
	if err := e.ready(ctx, scope, reader != nil && local != nil); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidRecord
	}
	policy, err := e.loadPolicy(ctx, scope, policyID)
	if err != nil {
		return nil, err
	}
	if !policy.Direction.AllowsInbound() {
		return nil, ErrDirectionDisabled
	}
	checkpoint, err := e.repository.Checkpoint(ctx, scope, policy.ID)
	if err != nil {
		return nil, err
	}
	page, err := reader.Pull(ctx, scope, PullRequest{ConnectorAccountID: policy.ConnectorAccountID, EntityType: policy.EntityType, Cursor: checkpoint.Cursor, Limit: limit})
	if err != nil {
		return nil, err
	}
	if err := page.Validate(limit); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(page.Changes))
	for _, change := range page.Changes {
		result, err := e.applyRemote(ctx, scope, policy, change, local)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	if page.NextCursor != checkpoint.Cursor || len(page.Changes) > 0 {
		if _, err := e.repository.AdvanceCheckpoint(ctx, scope, policy.ID, checkpoint.Version, page.NextCursor, e.now()); err != nil {
			return results, err
		}
	}
	return results, nil
}

func (e *Engine) applyRemote(ctx context.Context, scope tenancy.Scope, policy Policy, change RemoteMutation, local LocalEndpoint) (Result, error) {
	if err := change.Validate(); err != nil || change.EntityType != policy.EntityType {
		return Result{}, ErrInvalidRecord
	}
	payloadFingerprint, err := PayloadFingerprint(change.Payload)
	if err != nil {
		return Result{}, err
	}
	receiptFingerprint, err := RemoteMutationFingerprint(change)
	if err != nil {
		return Result{}, err
	}
	if receipt, err := e.repository.RemoteReceipt(ctx, scope, policy.ID, change.ChangeID); err == nil {
		if receipt.Fingerprint != receiptFingerprint {
			return Result{}, ErrReceiptCollision
		}
		return Result{Outcome: OutcomeDuplicate, PolicyID: policy.ID, RemoteID: change.RemoteID, CorrelationID: correlationForRemote(policy.ID, change), CausationID: change.ChangeID}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}

	mapping, mappingErr := e.mappings.MappingByRemote(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.ConnectorAccountID, policy.EntityType, change.RemoteID)
	mappingMissing := errors.Is(mappingErr, connectors.ErrMappingNotFound)
	if mappingErr != nil && !mappingMissing {
		return Result{}, mappingErr
	}
	var state EntityState
	stateFound := false
	if mappingErr == nil {
		state, err = e.repository.EntityState(ctx, scope, policy.ID, mapping.LocalEntityID)
		if err == nil {
			stateFound = true
		} else if !errors.Is(err, ErrNotFound) {
			return Result{}, err
		}
	}

	// Explicit origin marker is the strongest loop signal.
	if change.Origin.Source == policySource(policy.ID) {
		if mappingErr == nil && stateFound {
			state.LastRemoteRevision = change.Revision
			state.LastRemoteChangeID = change.ChangeID
			state.UpdatedAt = e.now()
			if _, err := e.repository.SaveEntityState(ctx, scope, state, state.Version); err != nil {
				return Result{}, err
			}
		}
		if err := e.repository.RecordRemoteReceipt(ctx, scope, Receipt{PolicyID: policy.ID, ChangeID: change.ChangeID, Fingerprint: receiptFingerprint, Outcome: OutcomeLoopSuppressed, CreatedAt: e.now()}); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeLoopSuppressed, PolicyID: policy.ID, RemoteID: change.RemoteID, CorrelationID: correlationForRemote(policy.ID, change), CausationID: change.ChangeID}, nil
	}
	// Fingerprint echo detection covers providers that cannot preserve origin
	// metadata but return the exact state we last pushed.
	if stateFound && payloadFingerprint == state.LastSyncedFingerprint {
		state.LastRemoteRevision = change.Revision
		state.LastRemoteChangeID = change.ChangeID
		state.UpdatedAt = e.now()
		updated, err := e.repository.SaveEntityState(ctx, scope, state, state.Version)
		if err != nil {
			return Result{}, err
		}
		if err := e.repository.RecordRemoteReceipt(ctx, scope, Receipt{PolicyID: policy.ID, ChangeID: change.ChangeID, Fingerprint: receiptFingerprint, Outcome: OutcomeLoopSuppressed, CreatedAt: e.now()}); err != nil {
			return Result{}, err
		}
		return resultFromState(OutcomeLoopSuppressed, policy.ID, updated, correlationForRemote(policy.ID, change), change.ChangeID), nil
	}

	var snapshot LocalSnapshot
	localChanged := false
	if mappingErr == nil {
		snapshot, err = local.Snapshot(ctx, scope, policy.EntityType, mapping.LocalEntityID)
		if err != nil {
			return Result{}, err
		}
		if err := snapshot.Validate(); err != nil || snapshot.LocalEntityID != mapping.LocalEntityID {
			return Result{}, ErrInvalidRecord
		}
		if stateFound {
			localChanged = snapshot.Version > state.LastLocalVersion
		}
	}
	remoteChanged := !stateFound || change.Revision != state.LastRemoteRevision
	overwrite := false
	if stateFound && localChanged && remoteChanged {
		switch policy.SourceOfTruth {
		case SourceLocal:
			if err := e.repository.RecordRemoteReceipt(ctx, scope, Receipt{PolicyID: policy.ID, ChangeID: change.ChangeID, Fingerprint: receiptFingerprint, Outcome: OutcomeLocalWins, CreatedAt: e.now()}); err != nil {
				return Result{}, err
			}
			return resultFromState(OutcomeLocalWins, policy.ID, state, correlationForRemote(policy.ID, change), change.ChangeID), nil
		case SourceManual:
			return Result{}, fmt.Errorf("%w: inbound local version and remote revision both changed", ErrConflict)
		case SourceRemote:
			overwrite = true
		}
	}

	correlation := correlationForRemote(policy.ID, change)
	request := LocalApplyRequest{EntityType: policy.EntityType, RemoteID: change.RemoteID, Operation: change.Operation, Payload: append(json.RawMessage(nil), change.Payload...), Overwrite: overwrite,
		EventID: deterministicID("evtsync", policy.ID, change.ChangeID), Source: policySource(policy.ID), CorrelationID: correlation, CausationID: change.ChangeID,
		IdempotencyKey: deterministicID("synin", policy.ID, change.ChangeID), OccurredAt: change.OccurredAt}
	if mappingErr == nil {
		request.LocalEntityID = mapping.LocalEntityID
		request.ExpectedLocalVersion = snapshot.Version
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	applied, err := local.ApplyRemote(ctx, scope, request)
	if err != nil {
		return Result{}, err
	}
	if err := applied.Validate(); err != nil {
		return Result{}, err
	}
	mapping, err = e.ensureMappingByRemote(ctx, scope, policy, applied.LocalEntityID, change.RemoteID, mapping, mappingErr)
	if err != nil {
		return Result{}, err
	}

	expectedStateVersion := int64(0)
	if stateFound {
		expectedStateVersion = state.Version
	}
	next := EntityState{PolicyID: policy.ID, LocalEntityID: applied.LocalEntityID, RemoteID: change.RemoteID, LastLocalVersion: applied.Version, LastRemoteRevision: change.Revision,
		LastSyncedFingerprint: payloadFingerprint, LastLocalEventID: request.EventID, LastRemoteChangeID: change.ChangeID, UpdatedAt: e.now()}
	next, err = e.repository.SaveEntityState(ctx, scope, next, expectedStateVersion)
	if err != nil {
		return Result{}, err
	}
	if err := e.repository.RecordRemoteReceipt(ctx, scope, Receipt{PolicyID: policy.ID, ChangeID: change.ChangeID, Fingerprint: receiptFingerprint, Outcome: OutcomeApplied, CreatedAt: e.now()}); err != nil {
		return Result{}, err
	}
	return resultFromState(OutcomeApplied, policy.ID, next, correlation, change.ChangeID), nil
}

func (e *Engine) ready(ctx context.Context, scope tenancy.Scope, dependencyOK bool) error {
	if ctx == nil {
		return errors.New("syncengine: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil || e.repository == nil || e.mappings == nil || e.clock == nil || !dependencyOK {
		return errors.New("syncengine: engine dependency is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}
func (e *Engine) loadPolicy(ctx context.Context, scope tenancy.Scope, id string) (Policy, error) {
	if !idPattern.MatchString(id) {
		return Policy{}, ErrInvalidRecord
	}
	policy, err := e.repository.Policy(ctx, scope, id)
	if err != nil {
		return Policy{}, err
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	if policy.OrganizationID != scope.OrganizationID().String() || policy.WorkspaceID != scope.WorkspaceID().String() || policy.ID != id {
		return Policy{}, tenancy.ErrInvalidScope
	}
	if !policy.Enabled {
		return Policy{}, ErrDirectionDisabled
	}
	return policy, nil
}
func (e *Engine) now() time.Time { return e.clock.Now().UTC() }

func (e *Engine) ensureMappingByLocal(ctx context.Context, scope tenancy.Scope, policy Policy, localID, remoteID string, current connectors.EntityMapping, lookupErr error) (connectors.EntityMapping, error) {
	if lookupErr == nil {
		if current.RemoteID != remoteID {
			return connectors.EntityMapping{}, connectors.ErrMappingConflict
		}
		return current, nil
	}
	created, err := e.mappings.UpsertMapping(ctx, connectors.MappingUpsert{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorAccountID: policy.ConnectorAccountID, EntityType: policy.EntityType, LocalEntityID: localID, RemoteID: remoteID, ExpectedVersion: 0})
	if err == nil {
		return created, nil
	}
	mappingConflict := errors.Is(err, connectors.ErrMappingConflict)
	if !mappingConflict {
		return connectors.EntityMapping{}, err
	}
	resolved, lookup := e.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.ConnectorAccountID, policy.EntityType, localID)
	if lookup != nil || resolved.RemoteID != remoteID {
		return connectors.EntityMapping{}, connectors.ErrMappingConflict
	}
	return resolved, nil
}
func (e *Engine) ensureMappingByRemote(ctx context.Context, scope tenancy.Scope, policy Policy, localID, remoteID string, current connectors.EntityMapping, lookupErr error) (connectors.EntityMapping, error) {
	if lookupErr == nil {
		if current.LocalEntityID != localID {
			return connectors.EntityMapping{}, connectors.ErrMappingConflict
		}
		return current, nil
	}
	return e.ensureMappingByLocal(ctx, scope, policy, localID, remoteID, current, lookupErr)
}

func resultFromState(outcome Outcome, policyID string, state EntityState, correlation, causation string) Result {
	return Result{Outcome: outcome, PolicyID: policyID, LocalEntityID: state.LocalEntityID, RemoteID: state.RemoteID, LocalVersion: state.LastLocalVersion, RemoteRevision: state.LastRemoteRevision, CorrelationID: correlation, CausationID: causation}
}

func correlationForLocal(m LocalMutation) string {
	if m.CorrelationID != "" {
		return m.CorrelationID
	}
	return m.EventID
}
func correlationForRemote(policyID string, m RemoteMutation) string {
	if optionalID(m.Origin.CorrelationID) && m.Origin.CorrelationID != "" {
		return m.Origin.CorrelationID
	}
	return deterministicID("corrsync", policyID, m.ChangeID)
}
func policySource(policyID string) string {
	sum := sha256.Sum256([]byte(policyID))
	return "sync." + hex.EncodeToString(sum[:12])
}
func deterministicID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{byte(len(part) >> 8), byte(len(part))})
		_, _ = h.Write([]byte(part))
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil)[:12])
}

// LocalMutationFingerprint binds a receipt to the full immutable local change,
// not only payload bytes, so event-id reuse with a changed version/operation or
// causation metadata is a collision rather than a duplicate.
func LocalMutationFingerprint(m LocalMutation) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	payload, err := PayloadFingerprint(m.Payload)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		EventID, EntityType, LocalEntityID                                 string
		LocalVersion                                                       int64
		Operation                                                          Operation
		PayloadFingerprint, Source, CorrelationID, CausationID, OccurredAt string
	}{m.EventID, m.EntityType, m.LocalEntityID, m.LocalVersion, m.Operation, payload, m.Source, m.CorrelationID, m.CausationID, m.OccurredAt.Format(time.RFC3339Nano)})
	if err != nil {
		return "", ErrInvalidRecord
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// RemoteMutationFingerprint binds remote change-id replay to revision,
// operation, origin metadata and canonical payload content.
func RemoteMutationFingerprint(m RemoteMutation) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	payload, err := PayloadFingerprint(m.Payload)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		ChangeID, EntityType, RemoteID, Revision string
		Operation                                Operation
		PayloadFingerprint, OccurredAt           string
		Origin                                   Origin
	}{m.ChangeID, m.EntityType, m.RemoteID, m.Revision, m.Operation, payload, m.OccurredAt.Format(time.RFC3339Nano), m.Origin})
	if err != nil {
		return "", ErrInvalidRecord
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// PayloadFingerprint canonicalizes an already bounded JSON object before SHA-256
// so harmless object-key ordering differences do not create false conflicts.
func PayloadFingerprint(payload json.RawMessage) (string, error) {
	if !validPayload(payload) {
		return "", ErrInvalidRecord
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", ErrInvalidRecord
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidRecord
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func validPayload(value json.RawMessage) bool {
	return len(value) > 0 && len(value) <= MaxPayloadBytes && eventbus.ValidateData(value) == nil
}
func optionalID(v string) bool   { return v == "" || idPattern.MatchString(v) }
func safeRemoteID(v string) bool { return v != "" && safeText(v, 512) }
func safeText(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func safeSource(v string) bool {
	if v == "" || len(v) > 128 || v != strings.ToLower(v) {
		return false
	}
	for i, c := range []byte(v) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '.' || c == '_' || c == '-')) {
			continue
		}
		return false
	}
	return true
}
func utc(v time.Time) bool { return !v.IsZero() && v.Location() == time.UTC }
