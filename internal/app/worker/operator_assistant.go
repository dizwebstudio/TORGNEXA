package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/operatorassistant"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/operatorassistantrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
)

var errInvalidAssistantTransition = errors.New("worker: invalid operator assistant run transition")

// assistantRunTransition is the worker's monotonic lifecycle gate. The API
// creates queued runs; a future provider adapter may advance them, but no
// transition can move a run back to an earlier state or execute an action.
func assistantRunTransition(from, to operatorassistant.RunState) error {
	if from == to {
		return nil
	}
	allowed := map[operatorassistant.RunState][]operatorassistant.RunState{
		operatorassistant.RunQueued:            {operatorassistant.RunRetrievingContext, operatorassistant.RunProviderUnavailable, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunRetrievingContext: {operatorassistant.RunAwaitingModel, operatorassistant.RunPartial, operatorassistant.RunProviderUnavailable, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunAwaitingModel:     {operatorassistant.RunStreaming, operatorassistant.RunAwaitingApproval, operatorassistant.RunCompleted, operatorassistant.RunPartial, operatorassistant.RunStale, operatorassistant.RunProviderUnavailable, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunStreaming:         {operatorassistant.RunAwaitingApproval, operatorassistant.RunCompleted, operatorassistant.RunPartial, operatorassistant.RunStale, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunAwaitingApproval:  {operatorassistant.RunActionQueued, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunActionQueued:      {operatorassistant.RunCompleted, operatorassistant.RunPartial, operatorassistant.RunFailed},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return nil
		}
	}
	return errInvalidAssistantTransition
}

func assistantRetryable(errorCode string) bool {
	switch errorCode {
	case "source_timeout", "provider_timeout", "provider_rate_limited", "provider_unavailable":
		return true
	default:
		return false
	}
}

// assistantLeaseDeadline bounds how long a worker may hold a run lease.
func assistantLeaseDeadline(now time.Time) time.Time {
	return now.UTC().Add(45 * time.Second)
}

// runOperatorAssistant is the durable queue/recovery loop for assistant runs.
// The deterministic API path may complete a run inline when no model is
// configured; queued runs are still lease-protected and never left pending
// forever. A provider adapter can replace the recovery transition later by
// consuming the same repository boundary without changing the API contract.
func runOperatorAssistant(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, repository *operatorassistantrepo.Repository, workerID string, cfg config.Worker) error {
	if logger == nil || dispatch == nil || repository == nil || workerID == "" {
		return errors.New("operator assistant worker: dependencies are required")
	}
	return pollLoop(ctx, cfg.PollInterval, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindOperatorAssistant, workerID, cfg.DispatchBatch, cfg.Lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			run, readErr := repository.GetRunForWorker(ctx, job.Scope, job.ItemID)
			if readErr != nil {
				if releaseErr := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "assistant_run_read_failed"); releaseErr != nil {
					return releaseErr
				}
				continue
			}
			if run.State != operatorassistant.RunQueued {
				if completeErr := dispatch.Complete(ctx, job); completeErr != nil {
					return completeErr
				}
				continue
			}
			if _, transitionErr := repository.TransitionRun(ctx, job.Scope, run.ID, run.Version, operatorassistant.RunProviderUnavailable, "provider_not_configured", time.Now().UTC()); transitionErr != nil {
				if releaseErr := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "assistant_run_transition_failed"); releaseErr != nil {
					return releaseErr
				}
				continue
			}
			if completeErr := dispatch.Complete(ctx, job); completeErr != nil {
				return completeErr
			}
			logger.Info("assistant run recovered without provider", "event", "worker.operator_assistant.provider_unavailable", "run_id", run.ID, "organization_id", job.Scope.OrganizationID().String(), "workspace_id", job.Scope.WorkspaceID().String())
		}
		return nil
	})
}
