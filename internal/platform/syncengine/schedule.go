package syncengine

import (
	"errors"
	"time"
)

var (
	ErrPreviewUnavailable = errors.New("syncengine: bootstrap preview is unavailable")
	ErrScheduleConflict   = errors.New("syncengine: schedule version conflict")
	ErrJobConflict        = errors.New("syncengine: sync job conflict")
	ErrJobLeaseLost       = errors.New("syncengine: sync job lease lost")
)

const (
	MinScheduleIntervalMinutes = 15
	MaxScheduleIntervalMinutes = 7 * 24 * 60
	MaxSyncJobAttempts         = 5
)

// ScheduleMode controls the reconciliation mode created by a durable account schedule.
type ScheduleMode string

const (
	ScheduleIncremental ScheduleMode = "incremental"
	ScheduleFull        ScheduleMode = "scheduled_full"
)

func (m ScheduleMode) Valid() bool { return m == ScheduleIncremental || m == ScheduleFull }

// BootstrapPreview is immutable dry-run evidence bound to an account version.
// It contains counts and policy metadata only; no remote payload or credential is persisted.
type BootstrapPreview struct {
	ID, AccountID                      string
	AccountVersion                     int64
	PolicyCount, ReadCount, WriteCount int
	CreatedAt, ExpiresAt               time.Time
	ConsumedAt                         *time.Time
}

func (p BootstrapPreview) Validate() error {
	if !idPattern.MatchString(p.ID) || !idPattern.MatchString(p.AccountID) || p.AccountVersion < 1 ||
		p.PolicyCount < 1 || p.PolicyCount > 200 || p.ReadCount < 0 || p.WriteCount < 0 ||
		p.ReadCount > p.PolicyCount || p.WriteCount > p.PolicyCount || !utc(p.CreatedAt) || !utc(p.ExpiresAt) ||
		!p.ExpiresAt.After(p.CreatedAt) || p.ExpiresAt.Sub(p.CreatedAt) > time.Hour {
		return ErrInvalidRecord
	}
	if p.ConsumedAt != nil && (!utc(*p.ConsumedAt) || p.ConsumedAt.Before(p.CreatedAt)) {
		return ErrInvalidRecord
	}
	return nil
}

// AccountSchedule is the durable per-account periodic synchronization configuration.
type AccountSchedule struct {
	AccountID       string
	Mode            ScheduleMode
	IntervalMinutes int
	Enabled         bool
	NextRunAt       *time.Time
	LastEnqueuedAt  *time.Time
	LastJobID       string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s AccountSchedule) Validate() error {
	if !idPattern.MatchString(s.AccountID) || !s.Mode.Valid() || s.IntervalMinutes < MinScheduleIntervalMinutes ||
		s.IntervalMinutes > MaxScheduleIntervalMinutes || s.Version < 1 || !utc(s.CreatedAt) || !utc(s.UpdatedAt) || s.UpdatedAt.Before(s.CreatedAt) {
		return ErrInvalidRecord
	}
	if s.Enabled != (s.NextRunAt != nil) || (s.NextRunAt != nil && !utc(*s.NextRunAt)) ||
		(s.LastEnqueuedAt != nil && !utc(*s.LastEnqueuedAt)) || !optionalID(s.LastJobID) {
		return ErrInvalidRecord
	}
	return nil
}

type SyncJobKind string

const (
	SyncJobInitialImport SyncJobKind = "initial_import"
	SyncJobScheduled     SyncJobKind = "scheduled_sync"
)

func (k SyncJobKind) Valid() bool { return k == SyncJobInitialImport || k == SyncJobScheduled }

type SyncJobStatus string

const (
	SyncJobPending   SyncJobStatus = "pending"
	SyncJobRunning   SyncJobStatus = "running"
	SyncJobRetryWait SyncJobStatus = "retry_wait"
	SyncJobCompleted SyncJobStatus = "completed"
	SyncJobFailed    SyncJobStatus = "failed"
)

func (s SyncJobStatus) Valid() bool {
	return s == SyncJobPending || s == SyncJobRunning || s == SyncJobRetryWait || s == SyncJobCompleted || s == SyncJobFailed
}

// SyncJob is durable dispatch progress. The reconciliation run retains the
// entity/page cursor; CheckpointPolicyID makes policy fan-out resumable.
type SyncJob struct {
	ID, OrganizationID, WorkspaceID, AccountID string
	Kind                                       SyncJobKind
	Mode                                       ScheduleMode
	Status                                     SyncJobStatus
	PreviewID                                  string
	CheckpointPolicyID                         string
	StartedRuns, AttemptCount, MaxAttempts     int
	AvailableAt, CreatedAt, UpdatedAt          time.Time
	StartedAt, CompletedAt                     *time.Time
	LastErrorCode                              string
}

func (j SyncJob) Validate() error {
	if !idPattern.MatchString(j.ID) || !idPattern.MatchString(j.OrganizationID) || !idPattern.MatchString(j.WorkspaceID) ||
		!idPattern.MatchString(j.AccountID) || !j.Kind.Valid() || !j.Mode.Valid() || !j.Status.Valid() ||
		!optionalID(j.PreviewID) || !optionalID(j.CheckpointPolicyID) || j.StartedRuns < 0 || j.StartedRuns > 200 ||
		j.AttemptCount < 0 || j.AttemptCount > j.MaxAttempts || j.MaxAttempts < 1 || j.MaxAttempts > MaxSyncJobAttempts ||
		!utc(j.AvailableAt) || !utc(j.CreatedAt) || !utc(j.UpdatedAt) || j.UpdatedAt.Before(j.CreatedAt) ||
		(j.StartedAt != nil && !utc(*j.StartedAt)) || (j.CompletedAt != nil && !utc(*j.CompletedAt)) ||
		(j.LastErrorCode != "" && !entityPattern.MatchString(j.LastErrorCode)) {
		return ErrInvalidRecord
	}
	if j.Kind == SyncJobInitialImport && j.PreviewID == "" || j.Kind == SyncJobScheduled && j.PreviewID != "" {
		return ErrInvalidRecord
	}
	closed := j.Status == SyncJobCompleted || j.Status == SyncJobFailed
	if closed != (j.CompletedAt != nil) || (j.Status == SyncJobPending && j.StartedAt != nil) ||
		(j.Status != SyncJobPending && j.StartedAt == nil) ||
		(j.StartedAt != nil && j.StartedAt.Before(j.CreatedAt)) ||
		(j.CompletedAt != nil && (j.StartedAt == nil || j.CompletedAt.Before(*j.StartedAt))) {
		return ErrInvalidRecord
	}
	return nil
}

// ClaimedSyncJob carries the short compare-by-lease token used by the scheduler.
type ClaimedSyncJob struct {
	SyncJob
	LeaseToken string
	LeaseUntil time.Time
}

func (j ClaimedSyncJob) Validate() error {
	if j.SyncJob.Validate() != nil || j.Status != SyncJobRunning || !idPattern.MatchString(j.LeaseToken) || !utc(j.LeaseUntil) || !j.LeaseUntil.After(j.UpdatedAt) {
		return ErrInvalidRecord
	}
	return nil
}
