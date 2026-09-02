package worker

import (
	"context"
	"errors"
	"log/slog"

	"github.com/torgnexa/torgnexa/internal/core/marketplaceoperations"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplaceoperationsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
)

var ErrMarketplaceActionExecutorUnavailable = errors.New("worker: marketplace action executor unavailable")

// MarketplaceFindingActionExecutor is the provider-neutral boundary used by
// the durable action worker. Implementations must re-check capability,
// approval, mapping and current remote state before any external write.
type MarketplaceFindingActionExecutor interface {
	Execute(context.Context, tenancy.Scope, marketplaceoperations.Finding, marketplaceoperations.FindingAction) error
}

// MarketplaceFindingActionExecutorFunc adapts a function to the worker
// boundary, which keeps connector details outside the marketplace core.
type MarketplaceFindingActionExecutorFunc func(context.Context, tenancy.Scope, marketplaceoperations.Finding, marketplaceoperations.FindingAction) error

// Execute invokes the adapted provider-neutral action executor.
func (f MarketplaceFindingActionExecutorFunc) Execute(ctx context.Context, scope tenancy.Scope, finding marketplaceoperations.Finding, action marketplaceoperations.FindingAction) error {
	if f == nil {
		return ErrMarketplaceActionExecutorUnavailable
	}
	return f(ctx, scope, finding, action)
}

const marketplaceActionMaxAttempts = 8

// runMarketplaceOperationActions drains retry/reconcile intents using the
// shared lease queue. A missing provider executor fails closed and is retried;
// after the bounded attempt budget the durable delivery state moves to DLQ.
// Immutable finding and action history is never deleted or rewritten.
func runMarketplaceOperationActions(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, repository *marketplaceoperationsrepo.Repository, executor MarketplaceFindingActionExecutor, workerID string, cfg config.Worker) error {
	if logger == nil || dispatch == nil || repository == nil || executor == nil || workerID == "" {
		return errors.New("marketplace action worker: dependencies are required")
	}
	return pollLoop(ctx, cfg.PollInterval, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindMarketplaceOperations, workerID, cfg.DispatchBatch, cfg.Lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			item, readErr := repository.FindingActionJob(ctx, job.Scope, job.ItemID)
			if readErr != nil {
				if releaseOrDeadLetterMarketplaceAction(ctx, dispatch, repository, job, "marketplace_action_read_failed") != nil {
					return readErr
				}
				continue
			}
			if item.Finding.Status == marketplaceoperations.FindingResolved {
				if completeErr := repository.CompleteFindingActionJob(ctx, job.Scope, job.ItemID, job.LeaseToken); completeErr != nil {
					return completeErr
				}
				continue
			}
			if executeErr := executor.Execute(ctx, job.Scope, item.Finding, item.Action); executeErr != nil {
				if job.AttemptCount >= marketplaceActionMaxAttempts {
					if deadLetterErr := repository.DeadLetterFindingActionJob(ctx, job.Scope, job.ItemID, job.LeaseToken, marketplaceActionErrorCode(executeErr)); deadLetterErr != nil {
						return deadLetterErr
					}
					logger.Error("marketplace action moved to dead letter", "event", "worker.marketplace_action_dead_letter", "job_id", job.ItemID, "finding_id", item.Finding.ID, "action", item.Action.Action, "error_code", marketplaceActionErrorCode(executeErr))
					continue
				}
				if releaseErr := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), marketplaceActionErrorCode(executeErr)); releaseErr != nil {
					return releaseErr
				}
				continue
			}
			if completeErr := repository.CompleteFindingActionJob(ctx, job.Scope, job.ItemID, job.LeaseToken); completeErr != nil {
				return completeErr
			}
			logger.Info("marketplace action completed", "event", "worker.marketplace_action_completed", "job_id", job.ItemID, "finding_id", item.Finding.ID, "action", item.Action.Action)
		}
		return nil
	})
}

func releaseOrDeadLetterMarketplaceAction(ctx context.Context, dispatch *workerrepo.Repository, repository *marketplaceoperationsrepo.Repository, job workerrepo.Job, code string) error {
	if job.AttemptCount >= marketplaceActionMaxAttempts {
		return repository.DeadLetterFindingActionJob(ctx, job.Scope, job.ItemID, job.LeaseToken, code)
	}
	return dispatch.Release(ctx, job, retryDelay(job.AttemptCount), code)
}

func marketplaceActionErrorCode(err error) string {
	if err == nil {
		return "marketplace_action_failed"
	}
	const max = 63
	value := err.Error()
	if len(value) > max {
		value = value[:max]
	}
	if !validWorkerErrorCode(value) {
		return "marketplace_action_failed"
	}
	return value
}

func validWorkerErrorCode(value string) bool {
	if len(value) < 1 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (index > 0 && ((char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-')) {
			continue
		}
		return false
	}
	return true
}
