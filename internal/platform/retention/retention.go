// Package retention coordinates privacy retention, subject-request and tenant-deletion workflows.
package retention

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/privacy"
)

var (
	ErrInvalid      = errors.New("retention: invalid value")
	ErrNotFound     = errors.New("retention: not found")
	ErrConflict     = errors.New("retention: conflict")
	ErrLegalHold    = errors.New("retention: blocked by legal hold")
	ErrUnsupported  = errors.New("retention: target does not support action")
	ErrNoTargets    = errors.New("retention: no eligible targets")
	ErrUnsafeResult = errors.New("retention: unsafe target result")
	ErrManualReview = errors.New("retention: manual review required")
)

type RequestType string

const (
	RequestAccess      RequestType = "access"
	RequestExport      RequestType = "export"
	RequestCorrection  RequestType = "correction"
	RequestDeletion    RequestType = "deletion"
	RequestRestriction RequestType = "restriction"
)

func (v RequestType) Valid() bool {
	switch v {
	case RequestAccess, RequestExport, RequestCorrection, RequestDeletion, RequestRestriction:
		return true
	}
	return false
}

type WorkflowKind string

const (
	WorkflowSubjectRequest  WorkflowKind = "subject_request"
	WorkflowRetentionExpiry WorkflowKind = "retention_expiry"
	WorkflowTenantDeletion  WorkflowKind = "tenant_deletion"
)

func (v WorkflowKind) Valid() bool {
	return v == WorkflowSubjectRequest || v == WorkflowRetentionExpiry || v == WorkflowTenantDeletion
}

type Action string

const (
	ActionExport            Action = "export"
	ActionCorrect           Action = "correct"
	ActionDelete            Action = "delete"
	ActionAnonymize         Action = "anonymize"
	ActionRestrict          Action = "restrict"
	ActionArchiveThenDelete Action = "archive_then_delete"
	ActionTenantDelete      Action = "tenant_delete"
	ActionManualReview      Action = "manual_review"
)

func (v Action) Valid() bool {
	switch v {
	case ActionExport, ActionCorrect, ActionDelete, ActionAnonymize, ActionRestrict, ActionArchiveThenDelete, ActionTenantDelete, ActionManualReview:
		return true
	}
	return false
}
func (v Action) Destructive() bool {
	return v == ActionDelete || v == ActionAnonymize || v == ActionArchiveThenDelete || v == ActionTenantDelete
}

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
)

func (v Status) Valid() bool {
	return v == StatusPending || v == StatusRunning || v == StatusBlocked || v == StatusCompleted
}

type TargetStatus string

const (
	TargetPending   TargetStatus = "pending"
	TargetRunning   TargetStatus = "running"
	TargetCompleted TargetStatus = "completed"
)

func (v TargetStatus) Valid() bool {
	return v == TargetPending || v == TargetRunning || v == TargetCompleted
}

type StoreClass string

const (
	StoreAuthoritative StoreClass = "authoritative"
	StoreDerived       StoreClass = "derived"
	StoreObject        StoreClass = "object"
)

func (v StoreClass) Valid() bool {
	return v == StoreAuthoritative || v == StoreDerived || v == StoreObject
}

type SubjectRef struct {
	Kind     string
	OpaqueID string
}

func (r SubjectRef) Valid() bool { return validToken(r.Kind, 64) && validOpaque(r.OpaqueID, 256) }

type Request struct {
	ID                    string
	OrganizationID        tenancy.OrganizationID
	WorkspaceID           tenancy.WorkspaceID
	Type                  RequestType
	Subject               SubjectRef
	CorrectionArtifactRef string
	Status                Status
	Version               uint64
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Job struct {
	ID             string
	OrganizationID tenancy.OrganizationID
	WorkspaceID    tenancy.WorkspaceID
	Kind           WorkflowKind
	RequestID      string
	Subject        SubjectRef
	PurposeKey     string
	DataClass      privacy.DataClass
	Disposition    privacy.Disposition
	Action         Action
	HoldPermitted  bool
	Status         Status
	Version        uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Target struct {
	JobID       string
	Store       string
	Class       StoreClass
	Action      Action
	Cursor      string
	Status      TargetStatus
	Processed   uint64
	LastDigest  string
	ArtifactRef string
	Version     uint64
	UpdatedAt   time.Time
}

type Evidence struct {
	JobID        string
	Store        string
	Action       Action
	CursorBefore string
	CursorAfter  string
	Processed    uint64
	Digest       string
	ArtifactRef  string
	Done         bool
	RecordedAt   time.Time
}

type HoldSelectorKind string

const (
	HoldTenant       HoldSelectorKind = "tenant"
	HoldSubject      HoldSelectorKind = "subject"
	HoldPurposeClass HoldSelectorKind = "purpose_class"
)

type LegalHold struct {
	ID             string
	OrganizationID tenancy.OrganizationID
	WorkspaceID    tenancy.WorkspaceID
	SelectorKind   HoldSelectorKind
	Subject        SubjectRef
	PurposeKey     string
	DataClass      privacy.DataClass
	ReasonRef      string
	ExpiresAt      *time.Time
	ReleasedAt     *time.Time
	Version        uint64
	CreatedAt      time.Time
}

func (h LegalHold) Active(now time.Time) bool {
	return h.ReleasedAt == nil && (h.ExpiresAt == nil || h.ExpiresAt.After(now))
}

type Repository interface {
	CreateWorkflow(context.Context, tenancy.Scope, *Request, Job, []Target) error
	Job(context.Context, tenancy.Scope, string) (Job, error)
	Targets(context.Context, tenancy.Scope, string) ([]Target, error)
	UpdateJob(context.Context, tenancy.Scope, Job, uint64) error
	CommitTargetPage(context.Context, tenancy.Scope, Target, uint64, Evidence) error
	UpdateRequest(context.Context, tenancy.Scope, Request, uint64) error
	Request(context.Context, tenancy.Scope, string) (Request, error)
	PlaceHold(context.Context, tenancy.Scope, LegalHold) error
	ActiveHolds(context.Context, tenancy.Scope, time.Time) ([]LegalHold, error)
	ReleaseHold(context.Context, tenancy.Scope, string, uint64, time.Time) error
}

type Step struct {
	JobID                 string
	Action                Action
	Subject               SubjectRef
	PurposeKey            string
	DataClass             privacy.DataClass
	CorrectionArtifactRef string
	Cursor                string
	Limit                 int
}

type StepResult struct {
	NextCursor  string
	Processed   uint64
	Digest      string
	ArtifactRef string
	Done        bool
}

type Store interface {
	Name() string
	Class() StoreClass
	Supports(Action) bool
	Step(context.Context, tenancy.Scope, Step) (StepResult, error)
}

type Auditor interface {
	Capture(context.Context, tenancy.Scope, audit.Entry) (audit.Record, error)
}

type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	repository Repository
	stores     map[string]Store
	ordered    []string
	auditor    Auditor
	clock      Clock
}

func NewService(repository Repository, stores []Store, auditor Auditor) (*Service, error) {
	return newService(repository, stores, auditor, systemClock{})
}
func newService(repository Repository, stores []Store, auditor Auditor, clock Clock) (*Service, error) {
	if repository == nil || auditor == nil || clock == nil {
		return nil, ErrInvalid
	}
	m := make(map[string]Store, len(stores))
	ordered := make([]string, 0, len(stores))
	for _, s := range stores {
		if s == nil || !validToken(s.Name(), 96) || !s.Class().Valid() {
			return nil, ErrInvalid
		}
		if _, ok := m[s.Name()]; ok {
			return nil, ErrConflict
		}
		m[s.Name()] = s
		ordered = append(ordered, s.Name())
	}
	sort.Strings(ordered)
	return &Service{repository: repository, stores: m, ordered: ordered, auditor: auditor, clock: clock}, nil
}

type SubjectRequestSpec struct {
	RequestID, JobID      string
	Type                  RequestType
	Subject               SubjectRef
	CorrectionArtifactRef string
}

func (s *Service) CreateSubjectRequest(ctx context.Context, scope tenancy.Scope, spec SubjectRequestSpec) (Job, error) {
	if err := validateCall(ctx, scope); err != nil {
		return Job{}, err
	}
	if !validID(spec.RequestID) || !validID(spec.JobID) || !spec.Type.Valid() || !spec.Subject.Valid() {
		return Job{}, ErrInvalid
	}
	if spec.Type == RequestCorrection {
		if !validOpaque(spec.CorrectionArtifactRef, 256) {
			return Job{}, ErrInvalid
		}
	} else if spec.CorrectionArtifactRef != "" {
		return Job{}, ErrInvalid
	}
	action := actionForRequest(spec.Type)
	targets, err := s.targets(spec.JobID, action)
	if err != nil {
		return Job{}, err
	}
	now := s.clock.Now().UTC()
	req := Request{ID: spec.RequestID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Type: spec.Type, Subject: spec.Subject, CorrectionArtifactRef: spec.CorrectionArtifactRef, Status: StatusPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	job := Job{ID: spec.JobID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Kind: WorkflowSubjectRequest, RequestID: req.ID, Subject: req.Subject, Action: action, HoldPermitted: action.Destructive(), Status: StatusPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.repository.CreateWorkflow(ctx, scope, &req, job, targets); err != nil {
		return Job{}, err
	}
	if err := s.audit(ctx, scope, job, "privacy.workflow.created"); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Service) CreateRetentionExpiry(ctx context.Context, scope tenancy.Scope, jobID string, policy privacy.RetentionPolicy) (Job, error) {
	if err := validateCall(ctx, scope); err != nil {
		return Job{}, err
	}
	if !validID(jobID) || privacy.ValidateRetention(scope, policy) != nil || policy.Status != privacy.StatusActive {
		return Job{}, ErrInvalid
	}
	action, ok := actionForDisposition(policy.Disposition)
	if !ok {
		return Job{}, ErrUnsupported
	}
	targets, err := s.targets(jobID, action)
	if err != nil {
		return Job{}, err
	}
	now := s.clock.Now().UTC()
	job := Job{ID: jobID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Kind: WorkflowRetentionExpiry, PurposeKey: policy.PurposeKey, DataClass: policy.DataClass, Disposition: policy.Disposition, Action: action, HoldPermitted: policy.LegalHoldOK, Status: statusForAction(action), Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.repository.CreateWorkflow(ctx, scope, nil, job, targets); err != nil {
		return Job{}, err
	}
	if err := s.audit(ctx, scope, job, "privacy.workflow.created"); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Service) CreateTenantDeletion(ctx context.Context, scope tenancy.Scope, jobID string) (Job, error) {
	if err := validateCall(ctx, scope); err != nil {
		return Job{}, err
	}
	if !validID(jobID) {
		return Job{}, ErrInvalid
	}
	targets, err := s.targets(jobID, ActionTenantDelete)
	if err != nil {
		return Job{}, err
	}
	now := s.clock.Now().UTC()
	job := Job{ID: jobID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Kind: WorkflowTenantDeletion, Action: ActionTenantDelete, HoldPermitted: true, Status: StatusPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.repository.CreateWorkflow(ctx, scope, nil, job, targets); err != nil {
		return Job{}, err
	}
	if err := s.audit(ctx, scope, job, "privacy.workflow.created"); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Service) PlaceLegalHold(ctx context.Context, scope tenancy.Scope, hold LegalHold) error {
	if err := validateCall(ctx, scope); err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	hold.OrganizationID, hold.WorkspaceID = scope.OrganizationID(), scope.WorkspaceID()
	if hold.Version == 0 {
		hold.Version = 1
	}
	if hold.CreatedAt.IsZero() {
		hold.CreatedAt = now
	}
	if err := validateHold(hold); err != nil {
		return err
	}
	if err := s.repository.PlaceHold(ctx, scope, hold); err != nil {
		return err
	}
	_, err := s.auditor.Capture(ctx, scope, audit.Entry{ActorID: "privacy-system", Source: "privacy", Action: "privacy.legal_hold.placed", ResourceType: "privacy_legal_hold", ResourceID: hold.ID, CorrelationID: hold.ID, Risk: audit.RiskLegallySignificant, Summary: audit.Summary{"selector_kind": string(hold.SelectorKind), "reason_ref": hold.ReasonRef}})
	return err
}

func (s *Service) ReleaseLegalHold(ctx context.Context, scope tenancy.Scope, holdID string, expectedVersion uint64) error {
	if err := validateCall(ctx, scope); err != nil {
		return err
	}
	if !validID(holdID) || expectedVersion < 1 {
		return ErrInvalid
	}
	now := s.clock.Now().UTC()
	if err := s.repository.ReleaseHold(ctx, scope, holdID, expectedVersion, now); err != nil {
		return err
	}
	_, err := s.auditor.Capture(ctx, scope, audit.Entry{ActorID: "privacy-system", Source: "privacy", Action: "privacy.legal_hold.released", ResourceType: "privacy_legal_hold", ResourceID: holdID, CorrelationID: holdID, Risk: audit.RiskLegallySignificant})
	return err
}

// Advance executes at most maxSteps pages. A failed store call never advances its cursor,
// so retry resumes from the last durably recorded target state.
func (s *Service) Advance(ctx context.Context, scope tenancy.Scope, jobID string, maxSteps int) (Job, error) {
	if err := validateCall(ctx, scope); err != nil {
		return Job{}, err
	}
	if !validID(jobID) || maxSteps < 1 || maxSteps > 1000 {
		return Job{}, ErrInvalid
	}
	job, err := s.repository.Job(ctx, scope, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.Status == StatusCompleted {
		return job, nil
	}
	if job.Action == ActionManualReview {
		return job, ErrManualReview
	}
	if job.Action.Destructive() && job.HoldPermitted {
		blocked, err := s.blockedByHold(ctx, scope, job)
		if err != nil {
			return Job{}, err
		}
		if blocked {
			if job.Status != StatusBlocked {
				if err := s.setJobStatus(ctx, scope, &job, StatusBlocked); err != nil {
					return Job{}, err
				}
				_ = s.audit(ctx, scope, job, "privacy.workflow.blocked")
			}
			return job, ErrLegalHold
		}
	}
	if job.Status != StatusRunning {
		if err := s.setJobStatus(ctx, scope, &job, StatusRunning); err != nil {
			return Job{}, err
		}
	}
	correctionRef := ""
	if job.RequestID != "" {
		req, e := s.repository.Request(ctx, scope, job.RequestID)
		if e != nil {
			return Job{}, e
		}
		correctionRef = req.CorrectionArtifactRef
	}
	steps := 0
	for steps < maxSteps {
		targets, err := s.repository.Targets(ctx, scope, job.ID)
		if err != nil {
			return Job{}, err
		}
		var current *Target
		for i := range targets {
			if targets[i].Status != TargetCompleted {
				current = &targets[i]
				break
			}
		}
		if current == nil {
			if err := s.setJobStatus(ctx, scope, &job, StatusCompleted); err != nil {
				return Job{}, err
			}
			if job.RequestID != "" {
				req, e := s.repository.Request(ctx, scope, job.RequestID)
				if e != nil {
					return Job{}, e
				}
				if req.Status != StatusCompleted {
					old := req.Version
					req.Status = StatusCompleted
					req.Version++
					req.UpdatedAt = s.clock.Now().UTC()
					if e := s.repository.UpdateRequest(ctx, scope, req, old); e != nil {
						return Job{}, e
					}
				}
			}
			if err := s.audit(ctx, scope, job, "privacy.workflow.completed"); err != nil {
				return Job{}, err
			}
			return job, nil
		}
		adapter, ok := s.stores[current.Store]
		if !ok || !adapter.Supports(job.Action) {
			return Job{}, ErrUnsupported
		}
		before := *current
		result, e := adapter.Step(ctx, scope, Step{JobID: job.ID, Action: job.Action, Subject: job.Subject, PurposeKey: job.PurposeKey, DataClass: job.DataClass, CorrectionArtifactRef: correctionRef, Cursor: before.Cursor, Limit: 500})
		if e != nil {
			return job, e
		}
		if err := validateResult(before.Cursor, result); err != nil {
			return Job{}, err
		}
		if result.Done && (job.Action == ActionExport || job.Action == ActionArchiveThenDelete) && result.ArtifactRef == "" {
			return Job{}, ErrUnsafeResult
		}
		after := before
		after.Cursor = result.NextCursor
		after.Processed += result.Processed
		after.LastDigest = result.Digest
		after.ArtifactRef = result.ArtifactRef
		after.Status = TargetRunning
		if result.Done {
			after.Status = TargetCompleted
		}
		old := after.Version
		after.Version++
		after.UpdatedAt = s.clock.Now().UTC()
		evidence := Evidence{JobID: job.ID, Store: after.Store, Action: job.Action, CursorBefore: before.Cursor, CursorAfter: after.Cursor, Processed: result.Processed, Digest: result.Digest, ArtifactRef: result.ArtifactRef, Done: result.Done, RecordedAt: after.UpdatedAt}
		if err := s.repository.CommitTargetPage(ctx, scope, after, old, evidence); err != nil {
			return Job{}, err
		}
		steps++
	}
	latest, err := s.repository.Job(ctx, scope, job.ID)
	if err != nil {
		return Job{}, err
	}
	return latest, nil
}

func (s *Service) targets(jobID string, action Action) ([]Target, error) {
	if action == ActionManualReview {
		return []Target{}, nil
	}
	if action == ActionTenantDelete {
		for _, name := range s.ordered {
			if !s.stores[name].Supports(action) {
				return nil, ErrUnsupported
			}
		}
	}
	out := make([]Target, 0, len(s.ordered))
	now := s.clock.Now().UTC()
	for _, name := range s.ordered {
		st := s.stores[name]
		if st.Supports(action) {
			out = append(out, Target{JobID: jobID, Store: name, Class: st.Class(), Action: action, Status: TargetPending, Version: 1, UpdatedAt: now})
		}
	}
	if len(out) == 0 {
		return nil, ErrNoTargets
	}
	// A destructive workflow must cover at least one authoritative store; derived-only deletion is unsafe.
	if action.Destructive() {
		ok := false
		for _, t := range out {
			if t.Class == StoreAuthoritative {
				ok = true
				break
			}
		}
		if !ok {
			return nil, ErrNoTargets
		}
	}
	return out, nil
}

func (s *Service) setJobStatus(ctx context.Context, scope tenancy.Scope, job *Job, status Status) error {
	old := job.Version
	job.Status = status
	job.Version++
	job.UpdatedAt = s.clock.Now().UTC()
	return s.repository.UpdateJob(ctx, scope, *job, old)
}

func (s *Service) blockedByHold(ctx context.Context, scope tenancy.Scope, job Job) (bool, error) {
	holds, err := s.repository.ActiveHolds(ctx, scope, s.clock.Now().UTC())
	if err != nil {
		return false, err
	}
	for _, h := range holds {
		if holdMatches(h, job) {
			return true, nil
		}
	}
	return false, nil
}
func holdMatches(h LegalHold, job Job) bool {
	switch h.SelectorKind {
	case HoldTenant:
		return true
	case HoldSubject:
		return job.Subject.Valid() && h.Subject == job.Subject
	case HoldPurposeClass:
		return job.PurposeKey != "" && h.PurposeKey == job.PurposeKey && h.DataClass == job.DataClass
	default:
		return false
	}
}

func (s *Service) audit(ctx context.Context, scope tenancy.Scope, job Job, action string) error {
	_, err := s.auditor.Capture(ctx, scope, audit.Entry{ActorID: "privacy-system", Source: "privacy", Action: action, ResourceType: "privacy_job", ResourceID: job.ID, CorrelationID: job.ID, Risk: audit.RiskLegallySignificant, Summary: audit.Summary{"workflow": string(job.Kind), "action": string(job.Action), "status": string(job.Status)}})
	return err
}

// ValidateRequest validates a persisted subject request without exposing its payload.
func ValidateRequest(scope tenancy.Scope, r Request) error {
	if !scope.Valid() || !validID(r.ID) || r.OrganizationID != scope.OrganizationID() || r.WorkspaceID != scope.WorkspaceID() || !r.Type.Valid() || !r.Subject.Valid() || !r.Status.Valid() || r.Version < 1 || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return ErrInvalid
	}
	if r.Type == RequestCorrection {
		if !validOpaque(r.CorrectionArtifactRef, 256) {
			return ErrInvalid
		}
	} else if r.CorrectionArtifactRef != "" {
		return ErrInvalid
	}
	return nil
}

// ValidateJob validates durable execution metadata.
func ValidateJob(scope tenancy.Scope, j Job) error {
	if !scope.Valid() || !validID(j.ID) || j.OrganizationID != scope.OrganizationID() || j.WorkspaceID != scope.WorkspaceID() || !j.Kind.Valid() || !j.Action.Valid() || !j.Status.Valid() || j.Version < 1 || j.CreatedAt.IsZero() || j.UpdatedAt.IsZero() || j.UpdatedAt.Before(j.CreatedAt) {
		return ErrInvalid
	}
	switch j.Kind {
	case WorkflowSubjectRequest:
		if !validID(j.RequestID) || !j.Subject.Valid() || j.PurposeKey != "" || j.DataClass.Valid() || j.Disposition.Valid() {
			return ErrInvalid
		}
	case WorkflowRetentionExpiry:
		if j.RequestID != "" || j.Subject.Valid() || !validToken(j.PurposeKey, 63) || !j.DataClass.Valid() || !j.Disposition.Valid() {
			return ErrInvalid
		}
	case WorkflowTenantDeletion:
		if j.RequestID != "" || j.Subject.Valid() || j.PurposeKey != "" || j.DataClass.Valid() || j.Disposition.Valid() || j.Action != ActionTenantDelete {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// ValidateTarget validates one resumable store checkpoint.
func ValidateTarget(t Target) error {
	if !validID(t.JobID) || !validToken(t.Store, 96) || !t.Class.Valid() || !t.Action.Valid() || !t.Status.Valid() || !validOptionalOpaque(t.Cursor, 512) || !validOptionalOpaque(t.ArtifactRef, 512) || t.Version < 1 || t.UpdatedAt.IsZero() {
		return ErrInvalid
	}
	if t.LastDigest != "" {
		if len(t.LastDigest) != 64 {
			return ErrInvalid
		}
		if _, err := hex.DecodeString(t.LastDigest); err != nil {
			return ErrInvalid
		}
	}
	return nil
}

// ValidateEvidence validates append-only execution evidence.
func ValidateEvidence(e Evidence) error {
	if !validID(e.JobID) || !validToken(e.Store, 96) || !e.Action.Valid() || !validOptionalOpaque(e.CursorBefore, 512) || !validOptionalOpaque(e.CursorAfter, 512) || !validOptionalOpaque(e.ArtifactRef, 512) || e.RecordedAt.IsZero() {
		return ErrInvalid
	}
	if e.Digest != "" {
		if len(e.Digest) != 64 {
			return ErrInvalid
		}
		if _, err := hex.DecodeString(e.Digest); err != nil {
			return ErrInvalid
		}
	}
	return nil
}

// ValidateLegalHold validates a tenant-scoped hold record.
func ValidateLegalHold(scope tenancy.Scope, h LegalHold) error {
	if !scope.Valid() || h.OrganizationID != scope.OrganizationID() || h.WorkspaceID != scope.WorkspaceID() {
		return ErrInvalid
	}
	return validateHold(h)
}

func actionForRequest(v RequestType) Action {
	switch v {
	case RequestAccess, RequestExport:
		return ActionExport
	case RequestCorrection:
		return ActionCorrect
	case RequestDeletion:
		return ActionDelete
	case RequestRestriction:
		return ActionRestrict
	}
	return ""
}
func statusForAction(action Action) Status {
	if action == ActionManualReview {
		return StatusBlocked
	}
	return StatusPending
}

func actionForDisposition(v privacy.Disposition) (Action, bool) {
	switch v {
	case privacy.DispositionDelete:
		return ActionDelete, true
	case privacy.DispositionAnonymize:
		return ActionAnonymize, true
	case privacy.DispositionArchiveThenDelete:
		return ActionArchiveThenDelete, true
	case privacy.DispositionManualReview:
		return ActionManualReview, true
	default:
		return "", false
	}
}

func validateCall(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}
func validateHold(h LegalHold) error {
	if !validID(h.ID) || !h.OrganizationID.Valid() || !h.WorkspaceID.Valid() || !validOpaque(h.ReasonRef, 256) || h.Version < 1 || h.CreatedAt.IsZero() {
		return ErrInvalid
	}
	if h.ExpiresAt != nil && !h.ExpiresAt.After(h.CreatedAt) {
		return ErrInvalid
	}
	switch h.SelectorKind {
	case HoldTenant:
		if h.Subject.Valid() || h.PurposeKey != "" || h.DataClass.Valid() {
			return ErrInvalid
		}
	case HoldSubject:
		if !h.Subject.Valid() || h.PurposeKey != "" || h.DataClass.Valid() {
			return ErrInvalid
		}
	case HoldPurposeClass:
		if !validToken(h.PurposeKey, 63) || !h.DataClass.Valid() || h.Subject.Valid() {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func validateResult(cursor string, r StepResult) error {
	if len(r.Digest) != 64 {
		return ErrUnsafeResult
	}
	if _, e := hex.DecodeString(r.Digest); e != nil {
		return ErrUnsafeResult
	}
	if !validOptionalOpaque(r.ArtifactRef, 512) || !validOptionalOpaque(r.NextCursor, 512) {
		return ErrUnsafeResult
	}
	if !r.Done && r.NextCursor == "" {
		return ErrUnsafeResult
	}
	if !r.Done && r.NextCursor == cursor {
		return ErrUnsafeResult
	}
	if r.Done && r.NextCursor != "" {
		return ErrUnsafeResult
	}
	return nil
}
func EvidenceDigest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
func validID(v string) bool { return validToken(v, 128) }
func validToken(v string, max int) bool {
	if len(v) < 1 || len(v) > max || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:/-", r) {
			continue
		}
		return false
	}
	return true
}
func validOpaque(v string, max int) bool {
	return len(v) > 0 && len(v) <= max && v == strings.TrimSpace(v) && !strings.ContainsAny(v, "\r\n\x00")
}
func validOptionalOpaque(v string, max int) bool { return v == "" || validOpaque(v, max) }

var _ = fmt.Sprintf
