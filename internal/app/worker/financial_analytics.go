package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/postgres/financialrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
)

// runFinancialAnalytics periodically materializes the previous UTC day. The
// calculation is snapshot-idempotent, so a process restart or a late poll
// cannot create duplicate evidence for the same period.
func runFinancialAnalytics(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, repository *financialrepo.Repository, poll time.Duration, batch int) error {
	if ctx == nil || logger == nil || dispatch == nil || repository == nil || poll <= 0 || batch < 1 {
		return errors.New("worker: invalid financial analytics runner")
	}
	return pollLoop(ctx, poll, func() error {
		scopes, err := dispatch.ActiveScopes(ctx, batch)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if _, err := repository.RefreshDaily(ctx, scope, time.Now().UTC()); err != nil {
				logger.Warn("financial analytics refresh deferred", "event", "worker.financial_analytics_deferred", "organization_id", scope.OrganizationID().String(), "workspace_id", scope.WorkspaceID().String(), "error_code", "financial_snapshot_failed")
			}
		}
		return nil
	})
}
