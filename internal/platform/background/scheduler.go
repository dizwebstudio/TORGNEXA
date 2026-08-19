package background

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/database"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const (
	schedulerPollInterval = 5 * time.Second
	schedulerLease        = 45 * time.Second
	schedulerBatch        = 25
)

type schedulerSyncStore interface {
	ClaimSyncJobs(context.Context, string, string, int, time.Duration) ([]syncengine.ClaimedSyncJob, error)
	ListPolicies(context.Context, tenancy.Scope, int) ([]syncengine.Policy, error)
	AdvanceSyncJob(context.Context, tenancy.Scope, string, string, string, int, time.Time) (syncengine.SyncJob, error)
	CompleteSyncJob(context.Context, tenancy.Scope, string, string, time.Time) (syncengine.SyncJob, error)
	ReleaseSyncJob(context.Context, tenancy.Scope, string, string, string, time.Time, time.Time, bool) (syncengine.SyncJob, error)
}

type schedulerAccountStore interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type schedulerRunStore interface {
	CreateRun(context.Context, tenancy.Scope, reconciliation.Run) (reconciliation.Run, error)
	Run(context.Context, tenancy.Scope, string) (reconciliation.Run, error)
}

type syncScheduler struct {
	sync     schedulerSyncStore
	accounts schedulerAccountStore
	runs     schedulerRunStore
	workerID string
	now      func() time.Time
	logger   *slog.Logger
}

// RunScheduler composes the durable Task-108 dispatch loop. It never performs
// provider transport directly; it fans out resumable reconciliation runs that
// the existing sync/reconciliation workers consume.
func RunScheduler(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if ctx == nil || logger == nil {
		return fmt.Errorf("scheduler: context and logger are required")
	}
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	syncStore, err := syncrepo.New(db)
	if err != nil {
		return err
	}
	accountStore, err := connectorrepo.New(db)
	if err != nil {
		return err
	}
	runStore, err := reconciliationrepo.New(db)
	if err != nil {
		return err
	}
	scheduler := syncScheduler{sync: syncStore, accounts: accountStore, runs: runStore, workerID: "scheduler.community", now: func() time.Time { return time.Now().UTC() }, logger: logger}
	logger.Info("service ready", "event", "service.ready", "poll_interval", schedulerPollInterval.String())
	if err := scheduler.tick(ctx); err != nil && ctx.Err() == nil {
		logger.Error("scheduler tick failed", "event", "scheduler.tick_failed", "error_code", "dispatch_failed")
	}
	ticker := time.NewTicker(schedulerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := scheduler.tick(ctx); err != nil && ctx.Err() == nil {
				logger.Error("scheduler tick failed", "event", "scheduler.tick_failed", "error_code", "dispatch_failed")
			}
		}
	}
}

func (s syncScheduler) tick(ctx context.Context) error {
	token, err := schedulerToken()
	if err != nil {
		return err
	}
	jobs, err := s.sync.ClaimSyncJobs(ctx, s.workerID, token, schedulerBatch, schedulerLease)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := s.process(ctx, job); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (s syncScheduler) process(ctx context.Context, job syncengine.ClaimedSyncJob) error {
	scope, err := tenancy.ParseScope(job.OrganizationID, job.WorkspaceID)
	if err != nil {
		return err
	}
	policies, permanentCode, err := s.eligiblePolicies(ctx, scope, job)
	if err != nil {
		return s.release(ctx, scope, job, "repository_unavailable", false, err)
	}
	if permanentCode != "" || len(policies) == 0 {
		if permanentCode == "" {
			permanentCode = "no_eligible_policies"
		}
		return s.release(ctx, scope, job, permanentCode, true, errors.New(permanentCode))
	}
	mode := reconciliation.ModeIncremental
	if job.Kind == syncengine.SyncJobInitialImport {
		mode = reconciliation.ModeOnDemand
	} else if job.Mode == syncengine.ScheduleFull {
		mode = reconciliation.ModeScheduledFull
	}
	started := job.StartedRuns
	for _, policy := range policies {
		if job.CheckpointPolicyID != "" && policy.ID <= job.CheckpointPolicyID {
			continue
		}
		now := s.now().UTC()
		runID := scheduledRunID(job.ID, policy.ID)
		run, lookupErr := s.runs.Run(ctx, scope, runID)
		if errors.Is(lookupErr, reconciliation.ErrNotFound) {
			run, lookupErr = s.runs.CreateRun(ctx, scope, reconciliation.Run{ID: runID, PolicyID: policy.ID, Mode: mode, TriggerRef: job.ID, Status: reconciliation.RunRunning, Version: 1, StartedAt: now, UpdatedAt: now})
		}
		if lookupErr != nil {
			return s.release(ctx, scope, job, "reconciliation_unavailable", false, lookupErr)
		}
		if run.PolicyID != policy.ID || run.TriggerRef != job.ID || run.Mode != mode {
			return s.release(ctx, scope, job, "run_identity_conflict", true, reconciliation.ErrConflict)
		}
		started++
		if _, err = s.sync.AdvanceSyncJob(ctx, scope, job.ID, job.LeaseToken, policy.ID, started, now); err != nil {
			return err
		}
		job.CheckpointPolicyID = policy.ID
		job.StartedRuns = started
	}
	_, err = s.sync.CompleteSyncJob(ctx, scope, job.ID, job.LeaseToken, s.now().UTC())
	if err == nil {
		s.logger.Info("sync dispatch completed", "event", "connector.sync_job.dispatched", "job_id", job.ID, "run_count", started)
	}
	return err
}

func (s syncScheduler) eligiblePolicies(ctx context.Context, scope tenancy.Scope, job syncengine.ClaimedSyncJob) ([]syncengine.Policy, string, error) {
	account, err := s.accounts.AccountByID(ctx, job.OrganizationID, job.WorkspaceID, job.AccountID)
	if err != nil {
		if errors.Is(err, sdk.ErrAccountNotFound) {
			return nil, "account_unavailable", nil
		}
		return nil, "", err
	}
	if account.Status != sdk.AccountActive || account.Health.Status != sdk.HealthHealthy {
		return nil, "account_unavailable", nil
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		return nil, "manifest_unavailable", nil
	}
	settings, err := s.accounts.AccountCapabilities(ctx, scope, account.ID)
	if err != nil {
		return nil, "", err
	}
	policies, err := s.sync.ListPolicies(ctx, scope, 200)
	if err != nil {
		return nil, "", err
	}
	eligible := make([]syncengine.Policy, 0, len(policies))
	for _, policy := range policies {
		accountMatches := sameSchedulerIdentity(policy.ConnectorAccountID, job.AccountID)
		if !policy.Enabled || !accountMatches || (job.Kind == syncengine.SyncJobInitialImport && !policy.Direction.AllowsInbound()) {
			continue
		}
		readCapability, writeCapability, ok := sdk.RequiredSyncCapabilities(account.Family, policy.EntityType)
		if !ok || (policy.Direction.AllowsInbound() && (!manifest.Supports(readCapability) || !sdk.CapabilityEnabled(settings, readCapability))) || (policy.Direction.AllowsOutbound() && (!manifest.Supports(writeCapability) || !sdk.CapabilityEnabled(settings, writeCapability))) {
			continue
		}
		eligible = append(eligible, policy)
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ID < eligible[j].ID })
	return eligible, "", nil
}

func sameSchedulerIdentity(left, right string) bool { return strings.Compare(left, right) == 0 }

func (s syncScheduler) release(ctx context.Context, scope tenancy.Scope, job syncengine.ClaimedSyncJob, code string, permanent bool, cause error) error {
	now := s.now().UTC()
	terminal := permanent || job.AttemptCount >= job.MaxAttempts
	retryAt := now
	if !terminal {
		retryAt = now.Add(retryDelay(job.ID, job.AttemptCount))
	}
	_, err := s.sync.ReleaseSyncJob(ctx, scope, job.ID, job.LeaseToken, code, retryAt, now, terminal)
	if err != nil {
		return err
	}
	s.logger.Warn("sync dispatch deferred", "event", "connector.sync_job.deferred", "job_id", job.ID, "error_code", code, "terminal", terminal)
	return cause
}

func scheduledRunID(jobID, policyID string) string {
	sum := sha256.Sum256([]byte("torgnexa.task108.run.v1\x00" + jobID + "\x00" + policyID))
	return "sync-run-" + hex.EncodeToString(sum[:16])
}

func retryDelay(jobID string, attempt int) time.Duration {
	base := time.Second << min(attempt-1, 5)
	sum := sha256.Sum256([]byte(jobID + fmt.Sprintf("/%d", attempt)))
	jitter := time.Duration(int(sum[0])%751) * time.Millisecond
	return base + jitter
}

func schedulerToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("scheduler: lease token: %w", err)
	}
	return "lease-" + hex.EncodeToString(value), nil
}
