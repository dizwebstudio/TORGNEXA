// Package reconciliation implements Task-014 provider-neutral reconciliation.
// It compares bounded canonical/remote snapshots, persists drift evidence and
// dispatches only policy-authorized remediation actions. It intentionally does
// not perform transport-specific reads/writes itself; Task-013 remains the
// propagation engine and connector/domain adapters remain responsible for IO.
package reconciliation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

var (
	ErrInvalid           = errors.New("reconciliation: invalid record")
	ErrNotFound          = errors.New("reconciliation: not found")
	ErrConflict          = errors.New("reconciliation: optimistic conflict")
	ErrRunClosed         = errors.New("reconciliation: run is closed")
	ErrActionUnsafe      = errors.New("reconciliation: action is unsafe")
	ErrActionUnavailable = errors.New("reconciliation: action executor unavailable")
)

var (
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	revisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=-]{0,255}$`)
	codePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

const (
	MaxPageSize    = 1000
	MaxCursorBytes = 1024
)

type Mode string

const (
	ModeIncremental   Mode = "incremental"
	ModeScheduledFull Mode = "scheduled_full"
	ModeOnDemand      Mode = "on_demand"
)

func (m Mode) Valid() bool {
	return m == ModeIncremental || m == ModeScheduledFull || m == ModeOnDemand
}

type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunInterrupted RunStatus = "interrupted"
	RunCompleted   RunStatus = "completed"
)

func (s RunStatus) Valid() bool { return s == RunRunning || s == RunInterrupted || s == RunCompleted }

type DriftKind string

const (
	DriftContent          DriftKind = "content_drift"
	DriftMissingMapping   DriftKind = "missing_mapping"
	DriftOrphanMapping    DriftKind = "orphan_mapping"
	DriftDuplicateMapping DriftKind = "duplicate_mapping"
	DriftStatusMismatch   DriftKind = "status_mismatch"
	DriftStaleConnector   DriftKind = "stale_connector"
)

func (k DriftKind) Valid() bool {
	switch k {
	case DriftContent, DriftMissingMapping, DriftOrphanMapping, DriftDuplicateMapping, DriftStatusMismatch, DriftStaleConnector:
		return true
	}
	return false
}

type DriftStatus string

const (
	DriftOpen            DriftStatus = "open"
	DriftAutoFixed       DriftStatus = "auto_fixed"
	DriftNotified        DriftStatus = "notified"
	DriftApprovalPending DriftStatus = "approval_pending"
	DriftIgnored         DriftStatus = "ignored"
)

func (s DriftStatus) Valid() bool {
	return s == DriftOpen || s == DriftAutoFixed || s == DriftNotified || s == DriftApprovalPending || s == DriftIgnored
}

type ActionKind string

const (
	ActionNone     ActionKind = "none"
	ActionAutoFix  ActionKind = "auto_fix"
	ActionNotify   ActionKind = "notify"
	ActionApproval ActionKind = "approval"
	ActionIgnore   ActionKind = "ignore"
)

func (a ActionKind) Valid() bool {
	return a == ActionNone || a == ActionAutoFix || a == ActionNotify || a == ActionApproval || a == ActionIgnore
}

type ActionResult string

const (
	ActionSucceeded ActionResult = "succeeded"
	ActionFailed    ActionResult = "failed"
)

func (r ActionResult) Valid() bool { return r == ActionSucceeded || r == ActionFailed }

type RepairDirection string

const (
	RepairLocalToRemote RepairDirection = "local_to_remote"
	RepairRemoteToLocal RepairDirection = "remote_to_local"
	RepairCreateMapping RepairDirection = "create_mapping"
)

func (d RepairDirection) Valid() bool {
	return d == RepairLocalToRemote || d == RepairRemoteToLocal || d == RepairCreateMapping
}

// ActionPolicy is snapshotted by the caller for each run. Auto-fix is still
// subject to runtime safety checks derived from the Task-013 sync policy and
// detected drift shape; requesting auto-fix cannot force an unsafe write.
type ActionPolicy struct {
	Content          ActionKind
	MissingMapping   ActionKind
	OrphanMapping    ActionKind
	DuplicateMapping ActionKind
	StatusMismatch   ActionKind
	StaleConnector   ActionKind
}

func (p ActionPolicy) action(k DriftKind) ActionKind {
	var a ActionKind
	switch k {
	case DriftContent:
		a = p.Content
	case DriftMissingMapping:
		a = p.MissingMapping
	case DriftOrphanMapping:
		a = p.OrphanMapping
	case DriftDuplicateMapping:
		a = p.DuplicateMapping
	case DriftStatusMismatch:
		a = p.StatusMismatch
	case DriftStaleConnector:
		a = p.StaleConnector
	}
	if a == "" {
		return ActionNone
	}
	return a
}
func (p ActionPolicy) Validate() error {
	for _, a := range []ActionKind{p.Content, p.MissingMapping, p.OrphanMapping, p.DuplicateMapping, p.StatusMismatch, p.StaleConnector} {
		if a != "" && !a.Valid() {
			return ErrInvalid
		}
	}
	// Never permit automatic deletion/guessing for ambiguous mapping or health drift.
	for _, a := range []ActionKind{p.OrphanMapping, p.DuplicateMapping, p.StaleConnector} {
		if a == ActionAutoFix {
			return ErrActionUnsafe
		}
	}
	return nil
}

type Run struct {
	ID, PolicyID             string
	Mode                     Mode
	TriggerRef               string
	Status                   RunStatus
	Cursor                   string
	ScannedCount, DriftCount int64
	Version                  int64
	StartedAt, UpdatedAt     time.Time
	CompletedAt              *time.Time
}

func (r Run) Validate() error {
	if !validID(r.ID) || !validID(r.PolicyID) || !r.Mode.Valid() || !r.Status.Valid() || !safeOptional(r.TriggerRef, 128) || !safeCursor(r.Cursor) || r.ScannedCount < 0 || r.DriftCount < 0 || r.Version < 1 || !utc(r.StartedAt) || !utc(r.UpdatedAt) || r.UpdatedAt.Before(r.StartedAt) {
		return ErrInvalid
	}
	if r.Status == RunCompleted {
		if r.CompletedAt == nil || !utc(*r.CompletedAt) || r.CompletedAt.Before(r.StartedAt) {
			return ErrInvalid
		}
	} else if r.CompletedAt != nil {
		return ErrInvalid
	}
	return nil
}

type Subject struct {
	LocalEntityID, RemoteID               string
	LocalPresent, RemotePresent           bool
	MappingLocalCount, MappingRemoteCount int
	LocalFingerprint, RemoteFingerprint   string
	LocalStatus, RemoteStatus             string
	LocalVersion                          int64
	RemoteRevision                        string
	ObservedAt                            time.Time
	CanAutoMap                            bool
}

func (s Subject) Validate() error {
	if !s.LocalPresent && !s.RemotePresent {
		return ErrInvalid
	}
	if s.LocalPresent && (!validID(s.LocalEntityID) || s.LocalVersion < 1 || !digestPattern.MatchString(s.LocalFingerprint)) {
		return ErrInvalid
	}
	if !s.LocalPresent && (s.LocalEntityID != "" || s.LocalVersion != 0 || s.LocalFingerprint != "") {
		return ErrInvalid
	}
	if s.RemotePresent && (!safeRemoteID(s.RemoteID) || !revisionPattern.MatchString(s.RemoteRevision) || !digestPattern.MatchString(s.RemoteFingerprint)) {
		return ErrInvalid
	}
	if !s.RemotePresent && (s.RemoteID != "" || s.RemoteRevision != "" || s.RemoteFingerprint != "") {
		return ErrInvalid
	}
	if s.MappingLocalCount < 0 || s.MappingRemoteCount < 0 || s.MappingLocalCount > 1000 || s.MappingRemoteCount > 1000 || !safeOptional(s.LocalStatus, 64) || !safeOptional(s.RemoteStatus, 64) || !utc(s.ObservedAt) {
		return ErrInvalid
	}
	if s.CanAutoMap && (!s.LocalPresent || !s.RemotePresent || s.MappingLocalCount != 0 || s.MappingRemoteCount != 0) {
		return ErrInvalid
	}
	return nil
}

type ScanRequest struct {
	Policy syncengine.Policy
	Mode   Mode
	Cursor string
	Limit  int
}

func (r ScanRequest) Validate() error {
	if r.Policy.Validate() != nil || !r.Mode.Valid() || !safeCursor(r.Cursor) || r.Limit < 1 || r.Limit > MaxPageSize {
		return ErrInvalid
	}
	return nil
}

type ScanPage struct {
	Subjects         []Subject
	NextCursor       string
	HasMore          bool
	RemoteObservedAt time.Time
}

func (p ScanPage) Validate(limit int) error {
	if limit < 1 || limit > MaxPageSize || len(p.Subjects) > limit || !safeCursor(p.NextCursor) || !utc(p.RemoteObservedAt) || (p.HasMore && p.NextCursor == "") {
		return ErrInvalid
	}
	for i := range p.Subjects {
		if p.Subjects[i].Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

type Source interface {
	Scan(context.Context, tenancy.Scope, ScanRequest) (ScanPage, error)
}

type Drift struct {
	ID, RunID, PolicyID                   string
	Kind                                  DriftKind
	LocalEntityID, RemoteID               string
	LocalFingerprint, RemoteFingerprint   string
	LocalStatus, RemoteStatus             string
	LocalVersion                          int64
	RemoteRevision                        string
	MappingLocalCount, MappingRemoteCount int
	DetectedAt                            time.Time
	Status                                DriftStatus
	RecommendedAction                     ActionKind
	Version                               int64
	ResolvedAt                            *time.Time
}

func (d Drift) Validate() error {
	if !validID(d.ID) || !validID(d.RunID) || !validID(d.PolicyID) || !d.Kind.Valid() || !d.Status.Valid() || !d.RecommendedAction.Valid() || d.Version < 1 || !utc(d.DetectedAt) || d.MappingLocalCount < 0 || d.MappingRemoteCount < 0 {
		return ErrInvalid
	}
	if d.LocalEntityID != "" && !validID(d.LocalEntityID) {
		return ErrInvalid
	}
	if d.RemoteID != "" && !safeRemoteID(d.RemoteID) {
		return ErrInvalid
	}
	if d.LocalFingerprint != "" && !digestPattern.MatchString(d.LocalFingerprint) {
		return ErrInvalid
	}
	if d.RemoteFingerprint != "" && !digestPattern.MatchString(d.RemoteFingerprint) {
		return ErrInvalid
	}
	if !safeOptional(d.LocalStatus, 64) || !safeOptional(d.RemoteStatus, 64) || d.LocalVersion < 0 || (d.RemoteRevision != "" && !revisionPattern.MatchString(d.RemoteRevision)) {
		return ErrInvalid
	}
	terminal := d.Status == DriftAutoFixed || d.Status == DriftNotified || d.Status == DriftApprovalPending || d.Status == DriftIgnored
	if terminal {
		if d.ResolvedAt == nil || !utc(*d.ResolvedAt) || d.ResolvedAt.Before(d.DetectedAt) {
			return ErrInvalid
		}
	} else if d.ResolvedAt != nil {
		return ErrInvalid
	}
	return nil
}

type ActionRecord struct {
	ID, DriftID    string
	Action         ActionKind
	IdempotencyKey string
	Result         ActionResult
	ErrorCode      string
	CreatedAt      time.Time
}

func (a ActionRecord) Validate() error {
	if !validID(a.ID) || !validID(a.DriftID) || a.Action == ActionNone || !a.Action.Valid() || !validID(a.IdempotencyKey) || !a.Result.Valid() || !safeErrorCode(a.ErrorCode) || !utc(a.CreatedAt) {
		return ErrInvalid
	}
	if a.Result == ActionSucceeded && a.ErrorCode != "" {
		return ErrInvalid
	}
	if a.Result == ActionFailed && a.ErrorCode == "" {
		return ErrInvalid
	}
	return nil
}

type Repository interface {
	CreateRun(context.Context, tenancy.Scope, Run) (Run, error)
	Run(context.Context, tenancy.Scope, string) (Run, error)
	UpdateRun(context.Context, tenancy.Scope, Run, int64) (Run, error)
	RecordDrift(context.Context, tenancy.Scope, Drift) (Drift, bool, error)
	Drift(context.Context, tenancy.Scope, string) (Drift, error)
	UpdateDrift(context.Context, tenancy.Scope, Drift, int64) (Drift, error)
	ListDrifts(context.Context, tenancy.Scope, string, int) ([]Drift, error)
	RecordAction(context.Context, tenancy.Scope, ActionRecord) error
	ListActions(context.Context, tenancy.Scope, string, int) ([]ActionRecord, error)
}

type RepairRequest struct {
	Drift          Drift
	Direction      RepairDirection
	IdempotencyKey string
}
type NotifyRequest struct {
	Drift          Drift
	IdempotencyKey string
}
type ApprovalRequest struct {
	Drift          Drift
	IdempotencyKey string
}
type ActionExecutor interface {
	AutoFix(context.Context, tenancy.Scope, RepairRequest) error
	Notify(context.Context, tenancy.Scope, NotifyRequest) error
	RequestApproval(context.Context, tenancy.Scope, ApprovalRequest) error
}

type IDGenerator interface{ NewID(string) (string, error) }
type randomIDs struct{}

func (randomIDs) NewID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

type clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Engine struct {
	syncRepo syncengine.Repository
	repo     Repository
	executor ActionExecutor
	ids      IDGenerator
	clock    clock
}

func New(syncRepo syncengine.Repository, repo Repository, executor ActionExecutor) (*Engine, error) {
	return newEngine(syncRepo, repo, executor, randomIDs{}, systemClock{})
}
func newEngine(syncRepo syncengine.Repository, repo Repository, executor ActionExecutor, ids IDGenerator, clk clock) (*Engine, error) {
	if syncRepo == nil || repo == nil || ids == nil || clk == nil {
		return nil, ErrInvalid
	}
	return &Engine{syncRepo: syncRepo, repo: repo, executor: executor, ids: ids, clock: clk}, nil
}

type RunRequest struct {
	PolicyID   string
	Mode       Mode
	TriggerRef string
	Limit      int
	StaleAfter time.Duration
	Actions    ActionPolicy
}

func (r RunRequest) Validate() error {
	if !validID(r.PolicyID) || !r.Mode.Valid() || !safeOptional(r.TriggerRef, 128) || r.Limit < 1 || r.Limit > MaxPageSize || r.StaleAfter < time.Minute || r.StaleAfter > 30*24*time.Hour || r.Actions.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

func (e *Engine) Start(ctx context.Context, scope tenancy.Scope, req RunRequest, source Source) (Run, error) {
	if ctx == nil || !scope.Valid() || source == nil || req.Validate() != nil {
		return Run{}, ErrInvalid
	}
	if _, err := e.loadPolicy(ctx, scope, req.PolicyID); err != nil {
		return Run{}, err
	}
	id, err := e.ids.NewID("rec_")
	if err != nil {
		return Run{}, err
	}
	now := e.now()
	run := Run{ID: id, PolicyID: req.PolicyID, Mode: req.Mode, TriggerRef: req.TriggerRef, Status: RunRunning, Version: 1, StartedAt: now, UpdatedAt: now}
	run, err = e.repo.CreateRun(ctx, scope, run)
	if err != nil {
		return Run{}, err
	}
	return e.execute(ctx, scope, run, req.StaleAfter, req.Actions, req.Limit, source)
}
func (e *Engine) Resume(ctx context.Context, scope tenancy.Scope, runID string, staleAfter time.Duration, actions ActionPolicy, limit int, source Source) (Run, error) {
	if ctx == nil || !scope.Valid() || !validID(runID) || source == nil || staleAfter < time.Minute || staleAfter > 30*24*time.Hour || limit < 1 || limit > MaxPageSize || actions.Validate() != nil {
		return Run{}, ErrInvalid
	}
	run, err := e.repo.Run(ctx, scope, runID)
	if err != nil {
		return Run{}, err
	}
	if run.Status == RunCompleted {
		return Run{}, ErrRunClosed
	}
	return e.execute(ctx, scope, run, staleAfter, actions, limit, source)
}

func (e *Engine) execute(ctx context.Context, scope tenancy.Scope, run Run, staleAfter time.Duration, actions ActionPolicy, limit int, source Source) (Run, error) {
	policy, err := e.loadPolicy(ctx, scope, run.PolicyID)
	if err != nil {
		return Run{}, err
	}
	// A resumed interrupted run returns to running before external IO.
	if run.Status == RunInterrupted {
		old := run.Version
		run.Status = RunRunning
		run.UpdatedAt = e.now()
		run.CompletedAt = nil
		run, err = e.repo.UpdateRun(ctx, scope, run, old)
		if err != nil {
			return Run{}, err
		}
	}
	for {
		page, scanErr := source.Scan(ctx, scope, ScanRequest{Policy: policy, Mode: run.Mode, Cursor: run.Cursor, Limit: limit})
		if scanErr != nil {
			return e.interrupt(ctx, scope, run, scanErr)
		}
		if page.Validate(limit) != nil {
			return e.interrupt(ctx, scope, run, ErrInvalid)
		}
		// One run-level health drift per cursor/page is deterministic and replay-safe.
		if e.now().Sub(page.RemoteObservedAt) > staleAfter {
			d := e.newDrift(run, DriftStaleConnector, Subject{ObservedAt: page.RemoteObservedAt}, actions.action(DriftStaleConnector))
			inserted, actionErr := e.persistAndAct(ctx, scope, policy, d, Subject{}, actions.action(DriftStaleConnector))
			if actionErr != nil {
				return e.interrupt(ctx, scope, run, actionErr)
			}
			if inserted {
				run.DriftCount++
			}
		}
		for _, s := range page.Subjects {
			kinds := detect(s)
			for _, k := range kinds {
				requested := actions.action(k)
				d := e.newDrift(run, k, s, requested)
				inserted, actionErr := e.persistAndAct(ctx, scope, policy, d, s, requested)
				if actionErr != nil {
					return e.interrupt(ctx, scope, run, actionErr)
				}
				if inserted {
					run.DriftCount++
				}
			}
		}
		run.ScannedCount += int64(len(page.Subjects))
		run.Cursor = page.NextCursor
		old := run.Version
		run.UpdatedAt = e.now()
		run.Status = RunRunning
		run, err = e.repo.UpdateRun(ctx, scope, run, old)
		if err != nil {
			return Run{}, err
		}
		if !page.HasMore {
			old = run.Version
			now := e.now()
			run.Status = RunCompleted
			run.CompletedAt = &now
			run.UpdatedAt = now
			return e.repo.UpdateRun(ctx, scope, run, old)
		}
	}
}

func (e *Engine) interrupt(ctx context.Context, scope tenancy.Scope, run Run, cause error) (Run, error) {
	old := run.Version
	run.Status = RunInterrupted
	run.UpdatedAt = e.now()
	updated, err := e.repo.UpdateRun(ctx, scope, run, old)
	if err != nil {
		return Run{}, errors.Join(cause, err)
	}
	return updated, cause
}

func (e *Engine) ApplyAction(ctx context.Context, scope tenancy.Scope, driftID string, action ActionKind) (Drift, error) {
	if ctx == nil || !scope.Valid() || !validID(driftID) || action == ActionNone || !action.Valid() {
		return Drift{}, ErrInvalid
	}
	d, err := e.repo.Drift(ctx, scope, driftID)
	if err != nil {
		return Drift{}, err
	}
	if d.Status != DriftOpen {
		return Drift{}, ErrRunClosed
	}
	policy, err := e.loadPolicy(ctx, scope, d.PolicyID)
	if err != nil {
		return Drift{}, err
	}
	subject := subjectFromDrift(d)
	return e.performAction(ctx, scope, policy, d, subject, action)
}

func (e *Engine) persistAndAct(ctx context.Context, scope tenancy.Scope, policy syncengine.Policy, d Drift, s Subject, requested ActionKind) (bool, error) {
	stored, inserted, err := e.repo.RecordDrift(ctx, scope, d)
	if err != nil {
		return false, err
	}
	if requested == ActionNone || stored.Status != DriftOpen {
		return inserted, nil
	}
	// A crash may happen after durable drift insertion (or even after an external
	// side effect) but before action receipt/status persistence. Replaying the page
	// therefore re-attempts an open drift with the same deterministic idempotency key.
	_, err = e.performAction(ctx, scope, policy, stored, s, requested)
	return inserted, err
}

func (e *Engine) performAction(ctx context.Context, scope tenancy.Scope, policy syncengine.Policy, d Drift, s Subject, action ActionKind) (Drift, error) {
	if action == ActionIgnore {
		aid, err := e.ids.NewID("rca_")
		if err != nil {
			return Drift{}, err
		}
		rec := ActionRecord{ID: aid, DriftID: d.ID, Action: action, IdempotencyKey: actionKey(d.ID, action), Result: ActionSucceeded, CreatedAt: e.now()}
		if err := e.repo.RecordAction(ctx, scope, rec); err != nil {
			return Drift{}, err
		}
		return e.transition(ctx, scope, d, DriftIgnored)
	}
	if e.executor == nil {
		return Drift{}, ErrActionUnavailable
	}
	key := actionKey(d.ID, action)
	errCode := ""
	var callErr error
	target := DriftOpen
	switch action {
	case ActionAutoFix:
		direction, ok := safeRepair(policy, d.Kind, s)
		if !ok {
			return Drift{}, ErrActionUnsafe
		}
		callErr = e.executor.AutoFix(ctx, scope, RepairRequest{Drift: d, Direction: direction, IdempotencyKey: key})
		target = DriftAutoFixed
	case ActionNotify:
		callErr = e.executor.Notify(ctx, scope, NotifyRequest{Drift: d, IdempotencyKey: key})
		target = DriftNotified
	case ActionApproval:
		callErr = e.executor.RequestApproval(ctx, scope, ApprovalRequest{Drift: d, IdempotencyKey: key})
		target = DriftApprovalPending
	default:
		return Drift{}, ErrInvalid
	}
	result := ActionSucceeded
	if callErr != nil {
		result = ActionFailed
		errCode = "executor_failed"
	}
	aid, err := e.ids.NewID("rca_")
	if err != nil {
		return Drift{}, err
	}
	rec := ActionRecord{ID: aid, DriftID: d.ID, Action: action, IdempotencyKey: key, Result: result, ErrorCode: errCode, CreatedAt: e.now()}
	if err := e.repo.RecordAction(ctx, scope, rec); err != nil {
		return Drift{}, err
	}
	if callErr != nil {
		return Drift{}, callErr
	}
	return e.transition(ctx, scope, d, target)
}
func (e *Engine) transition(ctx context.Context, scope tenancy.Scope, d Drift, status DriftStatus) (Drift, error) {
	old := d.Version
	now := e.now()
	d.Status = status
	d.ResolvedAt = &now
	return e.repo.UpdateDrift(ctx, scope, d, old)
}

func detect(s Subject) []DriftKind {
	out := make([]DriftKind, 0, 4)
	if s.MappingLocalCount > 1 || s.MappingRemoteCount > 1 {
		out = append(out, DriftDuplicateMapping)
	}
	if s.LocalPresent && s.RemotePresent && s.MappingLocalCount == 0 && s.MappingRemoteCount == 0 {
		out = append(out, DriftMissingMapping)
	}
	if (s.MappingLocalCount > 0 || s.MappingRemoteCount > 0) && (!s.LocalPresent || !s.RemotePresent) {
		out = append(out, DriftOrphanMapping)
	}
	unique := s.LocalPresent && s.RemotePresent && s.MappingLocalCount == 1 && s.MappingRemoteCount == 1
	if unique && s.LocalFingerprint != s.RemoteFingerprint {
		out = append(out, DriftContent)
	}
	if unique && s.LocalStatus != "" && s.RemoteStatus != "" && s.LocalStatus != s.RemoteStatus {
		out = append(out, DriftStatusMismatch)
	}
	return out
}

func safeRepair(policy syncengine.Policy, kind DriftKind, s Subject) (RepairDirection, bool) {
	switch kind {
	case DriftMissingMapping:
		if s.CanAutoMap && s.LocalPresent && s.RemotePresent && s.MappingLocalCount == 0 && s.MappingRemoteCount == 0 {
			return RepairCreateMapping, true
		}
	case DriftContent, DriftStatusMismatch:
		if policy.SourceOfTruth == syncengine.SourceLocal && policy.Direction.AllowsOutbound() {
			return RepairLocalToRemote, true
		}
		if policy.SourceOfTruth == syncengine.SourceRemote && policy.Direction.AllowsInbound() {
			return RepairRemoteToLocal, true
		}
	}
	return "", false
}

func (e *Engine) newDrift(run Run, kind DriftKind, s Subject, requested ActionKind) Drift {
	d := Drift{RunID: run.ID, PolicyID: run.PolicyID, Kind: kind, LocalEntityID: s.LocalEntityID, RemoteID: s.RemoteID, LocalFingerprint: s.LocalFingerprint, RemoteFingerprint: s.RemoteFingerprint, LocalStatus: s.LocalStatus, RemoteStatus: s.RemoteStatus, LocalVersion: s.LocalVersion, RemoteRevision: s.RemoteRevision, MappingLocalCount: s.MappingLocalCount, MappingRemoteCount: s.MappingRemoteCount, DetectedAt: e.now(), Status: DriftOpen, RecommendedAction: requested, Version: 1}
	d.ID = driftID(run.ID, kind, s)
	return d
}
func subjectFromDrift(d Drift) Subject {
	return Subject{LocalEntityID: d.LocalEntityID, RemoteID: d.RemoteID, LocalPresent: d.LocalEntityID != "", RemotePresent: d.RemoteID != "", MappingLocalCount: d.MappingLocalCount, MappingRemoteCount: d.MappingRemoteCount, LocalFingerprint: d.LocalFingerprint, RemoteFingerprint: d.RemoteFingerprint, LocalStatus: d.LocalStatus, RemoteStatus: d.RemoteStatus, LocalVersion: d.LocalVersion, RemoteRevision: d.RemoteRevision, ObservedAt: d.DetectedAt, CanAutoMap: d.Kind == DriftMissingMapping && d.MappingLocalCount == 0 && d.MappingRemoteCount == 0}
}
func driftID(runID string, k DriftKind, s Subject) string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d", runID, k, s.LocalEntityID, s.RemoteID, s.LocalFingerprint, s.RemoteFingerprint, s.LocalStatus+"\x00"+s.RemoteStatus, s.RemoteRevision, s.MappingLocalCount, s.MappingRemoteCount)
	sum := sha256.Sum256([]byte(raw))
	return "rcd_" + hex.EncodeToString(sum[:16])
}
func actionKey(driftID string, a ActionKind) string {
	sum := sha256.Sum256([]byte(driftID + "\x00" + string(a)))
	return "reconcile:" + hex.EncodeToString(sum[:16])
}
func (e *Engine) loadPolicy(ctx context.Context, scope tenancy.Scope, id string) (syncengine.Policy, error) {
	p, err := e.syncRepo.Policy(ctx, scope, id)
	if err != nil {
		return syncengine.Policy{}, err
	}
	if p.Validate() != nil || !p.Enabled {
		return syncengine.Policy{}, ErrInvalid
	}
	return p, nil
}
func (e *Engine) now() time.Time { return e.clock.Now().UTC() }

func validID(v string) bool    { return idPattern.MatchString(v) }
func safeCursor(v string) bool { return v == "" || safeOptional(v, MaxCursorBytes) }
func safeOptional(v string, max int) bool {
	if v == "" {
		return true
	}
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) || len(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func safeRemoteID(v string) bool  { return v != "" && safeOptional(v, 512) }
func safeErrorCode(v string) bool { return v == "" || codePattern.MatchString(v) }
func utc(t time.Time) bool        { return !t.IsZero() && t.Location() == time.UTC }
