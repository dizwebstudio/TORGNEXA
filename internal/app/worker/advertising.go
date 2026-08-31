package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/advertising"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/advertisingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type advertisingAccounts interface {
	ListAccounts(context.Context, string, string, string, int) ([]sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

// runMarketplaceAdvertising refreshes the previous UTC day for every active
// account with ads.read. The repository makes the period idempotent, while
// late provider data is deliberately reread by the incremental pass.
func runMarketplaceAdvertising(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, repository *advertisingrepo.Repository, accounts advertisingAccounts, secretSource secrets.SecretProvider, refresh connectorauth.RefreshCoordinator, registry *runtimeRegistry, poll time.Duration, batch int) error {
	if ctx == nil || logger == nil || dispatch == nil || repository == nil || accounts == nil || secretSource == nil || refresh == nil || registry == nil || registry.builtins == nil || poll <= 0 || batch < 1 {
		return errors.New("worker: invalid advertising runtime dependencies")
	}
	return pollLoop(ctx, poll, func() error {
		scopes, err := dispatch.ActiveScopes(ctx, batch)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if err := syncAdvertisingScope(ctx, logger, repository, accounts, secretSource, refresh, registry, scope, batch, time.Now().UTC()); err != nil {
				logger.Warn("advertising sync deferred", "event", "worker.advertising_sync_deferred", "organization_id", scope.OrganizationID().String(), "workspace_id", scope.WorkspaceID().String(), "error_code", "advertising_sync_failed")
			}
		}
		return nil
	})
}

func syncAdvertisingScope(ctx context.Context, logger *slog.Logger, repository *advertisingrepo.Repository, accounts advertisingAccounts, secretSource secrets.SecretProvider, refresh connectorauth.RefreshCoordinator, registry *runtimeRegistry, scope tenancy.Scope, batch int, now time.Time) error {
	end := now.Truncate(24 * time.Hour)
	start := end.Add(-24 * time.Hour)
	items, err := accounts.ListAccounts(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), "", batch)
	if err != nil {
		return err
	}
	for _, account := range items {
		if account.Status != sdk.AccountActive || account.Family != sdk.FamilyMarketplace {
			continue
		}
		capabilities, capErr := accounts.AccountCapabilities(ctx, scope, account.ID)
		if capErr != nil {
			return capErr
		}
		if !sdk.CapabilityEnabled(capabilities, "ads.read") {
			continue
		}
		runtime, runtimeErr := connectorruntime.NewForAccount(secretSource, refresh, scope, account)
		if runtimeErr != nil {
			return runtimeErr
		}
		reader, readerErr := registry.builtins.AdvertisingReader(account, runtime)
		if readerErr != nil {
			continue
		}
		run, runErr := repository.StartSyncRun(ctx, scope, advertisingrepo.SyncRun{AccountID: account.ID, Channel: account.ConnectorID, From: start, To: end, Mode: "daily"})
		if runErr != nil {
			return runErr
		}
		run.Status = "running"
		if err := repository.UpdateSyncRun(ctx, scope, run); err != nil {
			return err
		}
		run, err = syncAdvertisingAccount(ctx, repository, reader, account, runtime, scope, start, end, run)
		if err != nil {
			run.Status = "partial"
			run.ErrorCode = "remote_read_failed"
			run.CompletedAt = time.Now().UTC()
			_ = repository.UpdateSyncRun(ctx, scope, run)
			logger.Warn("advertising account sync failed", "event", "worker.advertising_account_sync_failed", "account_id", account.ID, "error_code", "remote_read_failed")
			continue
		}
		run.Status = "completed"
		run.CompletedAt = time.Now().UTC()
		if err := repository.UpdateSyncRun(ctx, scope, run); err != nil {
			return err
		}
	}
	return nil
}

func syncAdvertisingAccount(ctx context.Context, repository *advertisingrepo.Repository, reader sdk.AdvertisingReader, account sdk.Account, runtime sdk.Runtime, scope tenancy.Scope, start, end time.Time, run advertisingrepo.SyncRun) (advertisingrepo.SyncRun, error) {
	page, err := reader.ReadAdvertisingCampaigns(ctx, account, runtime, sdk.PageRequest{Limit: 100})
	if err != nil {
		return run, err
	}
	run.FetchedCount = len(page.Items)
	for _, remote := range page.Items {
		campaignID := "campaign:" + account.ID + ":" + remote.RemoteID
		campaign := core.Campaign{ID: campaignID, AccountID: account.ID, Channel: account.ConnectorID, RemoteID: remote.RemoteID, Name: remote.Name, Status: core.Status(remote.Status), Currency: remote.Currency, DailyBudgetMinor: remote.DailyBudgetMinor, TotalBudgetMinor: remote.TotalBudgetMinor, ObservedAt: remote.ObservedAt, Version: 1}
		if campaign.Status == "" {
			campaign.Status = core.StatusUnknown
		}
		if err := repository.UpsertCampaign(ctx, scope, campaign); err != nil {
			return run, err
		}
		query := sdk.AdvertisingQuery{From: start, To: end, CampaignIDs: []string{remote.RemoteID}, Limit: 100}
		spends, spendErr := reader.ReadAdvertisingSpend(ctx, account, runtime, query)
		if spendErr != nil {
			return run, spendErr
		}
		performance, performanceErr := reader.ReadAdvertisingPerformance(ctx, account, runtime, query)
		if performanceErr != nil {
			return run, performanceErr
		}
		for _, fact := range spends.Items {
			normalized := core.SpendFact{ID: "spend:" + account.ID + ":" + fact.RemoteFactID, AccountID: account.ID, Channel: account.ConnectorID, CampaignID: campaignID, AdID: fact.AdID, SKU: fact.SKU, RemoteFactID: fact.RemoteFactID, PeriodStart: fact.PeriodStart, PeriodEnd: fact.PeriodEnd, AmountMinor: fact.AmountMinor, Currency: fact.Currency, Source: "marketplace_api", ObservedAt: fact.ObservedAt, EffectiveAt: fact.EffectiveAt, Quality: core.Quality(fact.Quality)}
			if normalized.Quality == "" {
				normalized.Quality = core.QualityUnknown
			}
			if err := repository.AppendSpend(ctx, scope, normalized); err != nil {
				if errors.Is(err, advertisingrepo.ErrConflict) {
					if findingErr := recordAdvertisingFinding(ctx, repository, scope, account.ID, normalized.CampaignID, normalized.RemoteFactID, "changed_historical_report", normalized.AmountMinor, "Провайдер изменил ранее загруженный расход; исходный факт сохранён, нужна сверка.", normalized.ObservedAt); findingErr != nil {
						return run, findingErr
					}
					run.RejectedCount++
					continue
				}
				return run, err
			}
			if normalized.SKU == "" {
				if err := recordAdvertisingFinding(ctx, repository, scope, account.ID, normalized.CampaignID, normalized.RemoteFactID, "unattributed_spend", normalized.AmountMinor, "Рекламный расход не связан с SKU; он остаётся в общем канале.", normalized.ObservedAt); err != nil {
					return run, err
				}
			}
			if normalized.Quality == core.QualityDelayed || normalized.Quality == core.QualityPartial || normalized.Quality == core.QualityUnknown {
				if err := recordAdvertisingFinding(ctx, repository, scope, account.ID, normalized.CampaignID, normalized.RemoteFactID, "delayed_report", normalized.AmountMinor, "Провайдер вернул неполную или задержанную статистику.", normalized.ObservedAt); err != nil {
					return run, err
				}
			}
			run.AcceptedCount++
		}
		for _, fact := range performance.Items {
			normalized := core.PerformanceFact{ID: "performance:" + account.ID + ":" + fact.RemoteFactID, AccountID: account.ID, Channel: account.ConnectorID, CampaignID: campaignID, AdID: fact.AdID, SKU: fact.SKU, RemoteFactID: fact.RemoteFactID, PeriodStart: fact.PeriodStart, PeriodEnd: fact.PeriodEnd, Impressions: fact.Impressions, Clicks: fact.Clicks, Orders: fact.Orders, RevenueMinor: fact.RevenueMinor, Currency: fact.Currency, Source: "marketplace_api", ObservedAt: fact.ObservedAt, EffectiveAt: fact.EffectiveAt, Quality: core.Quality(fact.Quality)}
			if normalized.Quality == "" {
				normalized.Quality = core.QualityUnknown
			}
			if err := repository.AppendPerformance(ctx, scope, normalized); err != nil {
				if errors.Is(err, advertisingrepo.ErrConflict) {
					if findingErr := recordAdvertisingFinding(ctx, repository, scope, account.ID, normalized.CampaignID, normalized.RemoteFactID, "changed_historical_report", normalized.RevenueMinor, "Провайдер изменил ранее загруженную performance-статистику; исходный факт сохранён, нужна сверка.", normalized.ObservedAt); findingErr != nil {
						return run, findingErr
					}
					run.RejectedCount++
					continue
				}
				return run, err
			}
			if normalized.SKU == "" {
				if err := recordAdvertisingFinding(ctx, repository, scope, account.ID, normalized.CampaignID, normalized.RemoteFactID, "unattributed_performance", normalized.RevenueMinor, "Рекламная конверсия не связана с SKU; выручка не распределяется молча.", normalized.ObservedAt); err != nil {
					return run, err
				}
			}
			run.AcceptedCount++
		}
	}
	run.WatermarkAt = time.Now().UTC()
	return run, nil
}

func recordAdvertisingFinding(ctx context.Context, repository *advertisingrepo.Repository, scope tenancy.Scope, accountID, campaignID, remoteID, kind string, actual int64, explanation string, observedAt time.Time) error {
	sum := sha256.Sum256([]byte(accountID + "\x00" + campaignID + "\x00" + remoteID + "\x00" + kind))
	return repository.RecordFinding(ctx, scope, core.Finding{ID: "finding-" + hex.EncodeToString(sum[:])[:32], Kind: kind, CampaignID: campaignID, RemoteReference: remoteID, ExpectedMinor: 0, ActualMinor: actual, Severity: "warn", Explanation: explanation, ObservedAt: observedAt.UTC()})
}

var _ advertisingAccounts = (*connectorrepo.Repository)(nil)
