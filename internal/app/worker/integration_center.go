package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/integrationcenter"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/integrationcenterrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
)

const integrationCenterKafkaConsumerGroup = "torgnexa.integration-center.v1"

// integrationCenterEventHandler turns canonical account status transitions
// into coalesced tenant-scoped recompute work. The queue has a unique tenant /
// account key, so duplicate and out-of-order deliveries converge safely.
type integrationCenterEventHandler struct {
	queue *integrationcenterrepo.Repository
}

func (h integrationCenterEventHandler) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	return h.handle(ctx, delivery, func(callCtx context.Context, scope tenancy.Scope, event eventbus.Event) error {
		return h.queue.EnqueueRecompute(callCtx, scope, event.EntityID, integrationCenterReason(event), event.ID, event.OccurredAt.Time().UTC())
	})
}

// HandleInTransaction is used with inboxrepo. It keeps the dedup receipt and
// queue mutation in one PostgreSQL transaction; a crash before commit causes
// both to roll back and Kafka safely redelivers the event.
func (h integrationCenterEventHandler) HandleInTransaction(ctx context.Context, tx inboxrepo.Transaction, delivery eventbus.Delivery) error {
	return h.handle(ctx, delivery, func(callCtx context.Context, scope tenancy.Scope, event eventbus.Event) error {
		return integrationcenterrepo.EnqueueRecomputeInTransaction(callCtx, tx, scope, event.EntityID, integrationCenterReason(event), event.ID, event.OccurredAt.Time().UTC())
	})
}

func (h integrationCenterEventHandler) handle(ctx context.Context, delivery eventbus.Delivery, enqueue func(context.Context, tenancy.Scope, eventbus.Event) error) error {
	if h.queue == nil {
		return eventbus.Permanent("integration_center_queue_unavailable")
	}
	if enqueue == nil || delivery.Validate() != nil {
		return eventbus.Permanent("integration_center_invalid_event")
	}
	event := delivery.Event
	if !strings.HasPrefix(event.Type.String(), "commerce.integration.") {
		return nil
	}
	scope, err := tenancy.ParseScope(event.OrganizationID, event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("integration_center_invalid_scope")
	}
	if event.EntityID == "" {
		return eventbus.Permanent("integration_center_missing_account")
	}
	if err := enqueue(ctx, scope, event); err != nil {
		return eventbus.Retryable("integration_center_recompute_enqueue_failed")
	}
	return nil
}

func integrationCenterReason(event eventbus.Event) string {
	if event.Type.String() == "commerce.integration.account_status_changed.v1" {
		return "account_status_changed"
	}
	return "integration_event"
}

// runIntegrationCenterRecompute drains the durable queue populated by the
// Kafka consumer. It deliberately performs only local, tenant-scoped reads;
// remote checks remain owned by connector health actions. A crash leaves the
// lease to be reclaimed by ClaimRecompute, while source/read errors use the
// bounded retry path and eventually become dead_letter.
func runIntegrationCenterRecompute(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, queue *integrationcenterrepo.Repository, accounts *connectorrepo.Repository, runtime *runtimeRegistry, workerID string, cfg config.Worker) error {
	if ctx == nil || logger == nil || dispatch == nil || queue == nil || accounts == nil || runtime == nil || workerID == "" || cfg.PollInterval <= 0 || cfg.DispatchBatch < 1 || cfg.Lease <= 0 {
		return errors.New("integration center: invalid recompute worker")
	}
	return pollLoop(ctx, cfg.PollInterval, func() error {
		scopes, err := dispatch.ActiveScopes(ctx, cfg.DispatchBatch)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			for attempt := 0; attempt < cfg.DispatchBatch; attempt++ {
				item, claimErr := queue.ClaimRecompute(ctx, scope, workerID, time.Now().UTC(), cfg.Lease)
				if errors.Is(claimErr, integrationcenterrepo.ErrNotFound) {
					break
				}
				if claimErr != nil {
					return claimErr
				}
				processErr := recomputeIntegrationAccount(ctx, scope, item, queue, accounts, runtime)
				if processErr != nil {
					delay := retryDelay(item.Attempts)
					if retryErr := queue.RetryRecompute(ctx, scope, item.AccountID, workerID, integrationCenterErrorCode(processErr), time.Now().UTC(), delay); retryErr != nil {
						return retryErr
					}
					logger.Warn("integration center recompute deferred", "event", "worker.integration_center_recompute_deferred", "account_id", item.AccountID, "error_code", integrationCenterErrorCode(processErr), "attempt", item.Attempts)
					continue
				}
				if completeErr := queue.CompleteRecompute(ctx, scope, item.AccountID, workerID, time.Now().UTC(), false); completeErr != nil {
					return completeErr
				}
			}
		}
		return nil
	})
}

func recomputeIntegrationAccount(ctx context.Context, scope tenancy.Scope, item integrationcenterrepo.QueueItem, queue *integrationcenterrepo.Repository, accounts *connectorrepo.Repository, runtime *runtimeRegistry) error {
	if item.AccountID == "" || item.EventID == "" || queue == nil || accounts == nil || runtime == nil {
		return errors.New("integration center: invalid recompute item")
	}
	account, err := accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), item.AccountID)
	if err != nil {
		return fmt.Errorf("account read: %w", err)
	}
	now := time.Now().UTC()
	snapshot, err := reduceWorkerAccount(account, item, now)
	if err != nil {
		return err
	}
	return queue.SaveSnapshot(ctx, scope, snapshot, []string{"account:" + account.ID + ":" + fmt.Sprint(account.Version), "event:" + item.EventID})
}

// reduceWorkerAccount is the durable worker's minimal local projection. The
// API read model enriches it with bulk sync/reconciliation evidence; keeping
// this fallback projection independent means a source adapter outage cannot
// strand the queue or fabricate a remote health check.
func reduceWorkerAccount(account sdk.Account, item integrationcenterrepo.QueueItem, now time.Time) (integrationcenter.Snapshot, error) {
	support, supported := builtinruntime.SupportFor(account.ConnectorID)
	runtimeStatus := integrationcenter.RuntimeNotRegistered
	surface := "unknown"
	if supported {
		surface = support.Surface
		switch {
		case support.HealthOnly:
			runtimeStatus = integrationcenter.RuntimeHealthOnly
		case support.Stage == builtinruntime.SupportReady:
			runtimeStatus = integrationcenter.RuntimeReady
		case support.Stage == builtinruntime.SupportSeparateSurface:
			runtimeStatus = integrationcenter.RuntimeSeparateSurface
		default:
			runtimeStatus = integrationcenter.RuntimeUnsupported
		}
	}
	accountStatus := integrationcenter.AccountActive
	switch account.Status {
	case sdk.AccountDisabled:
		accountStatus = integrationcenter.AccountDisabled
	case sdk.AccountSuspended:
		accountStatus = integrationcenter.AccountSuspended
	case sdk.AccountError:
		accountStatus = integrationcenter.AccountError
	}
	credential := integrationcenter.CredentialUnknown
	if account.SecretReference == "" {
		credential = integrationcenter.CredentialMissing
	} else if account.Health.Status == sdk.HealthHealthy {
		credential = integrationcenter.CredentialPresent
	} else if account.Health.ReasonCode == "oauth_reauthorization_required" || account.Health.ReasonCode == "oauth_refresh_failed" {
		credential = integrationcenter.CredentialReauthorizationRequired
	} else if account.Health.ReasonCode == "credentials_invalid" || account.Health.ReasonCode == "auth_rejected" {
		credential = integrationcenter.CredentialInvalid
	}
	configuration := integrationcenter.ConfigurationValid
	if supported && support.RuntimeConfigRequired {
		configuration = integrationcenter.ConfigurationUnknown
	}
	health := integrationcenter.HealthUnknown
	switch account.Health.Status {
	case sdk.HealthHealthy:
		health = integrationcenter.HealthHealthy
	case sdk.HealthDegraded:
		health = integrationcenter.HealthDegraded
	case sdk.HealthUnavailable:
		health = integrationcenter.HealthUnavailable
	}
	checked := account.Health.CheckedAt
	healthReason := account.Health.ReasonCode
	if health != integrationcenter.HealthUnknown && checked.IsZero() {
		health = integrationcenter.HealthUnknown
		healthReason = "health_check_missing"
	}
	if checked.IsZero() {
		checked = account.UpdatedAt
	}
	if checked.IsZero() {
		checked = now
	}
	evidence := integrationcenter.EvidenceRef{ObservedAt: checked.UTC(), CheckedAt: checked.UTC(), SourceKind: "connector_account", SourceRef: account.ID, ReasonCode: healthReason, Visibility: integrationcenter.VisibilityFull, StaleAfterSeconds: 3600, AgeSeconds: maxInt64(0, int64(now.Sub(checked).Seconds()))}
	dimensions := integrationcenter.Dimensions{
		Runtime:        integrationcenter.Dimension{Status: string(runtimeStatus), Evidence: evidence},
		Account:        integrationcenter.Dimension{Status: string(accountStatus), Evidence: evidence},
		Credential:     integrationcenter.Dimension{Status: string(credential), Evidence: evidence},
		Configuration:  integrationcenter.Dimension{Status: string(configuration), Evidence: evidence},
		Health:         integrationcenter.Dimension{Status: string(health), Evidence: evidence},
		Capability:     integrationcenter.Dimension{Status: string(integrationcenter.CapabilityNotDeclared), Evidence: evidence},
		Sync:           integrationcenter.Dimension{Status: string(integrationcenter.SyncNotConfigured), Evidence: evidence},
		Reconciliation: integrationcenter.Dimension{Status: string(integrationcenter.ReconciliationNotConfigured), Evidence: evidence},
		Webhook:        integrationcenter.Dimension{Status: string(integrationcenter.WebhookNotConfigured), Evidence: evidence},
		RateLimit:      integrationcenter.Dimension{Status: string(integrationcenter.RateLimitNotObserved), Evidence: evidence},
	}
	return integrationcenter.Reduce(integrationcenter.Input{AccountID: account.ID, ConnectorID: account.ConnectorID, Family: string(account.Family), Surface: surface, Version: account.Version, Dimensions: dimensions, Now: now, SourceWatermarks: []string{"event:" + item.EventID}})
}

func integrationCenterErrorCode(err error) string {
	// Never derive a persisted/logged code from err.Error(): SQL and connector
	// errors may contain provider text or identifiers that are not safe evidence.
	// The center deliberately exposes one bounded class until a source adapter
	// provides an explicitly normalized machine reason.
	if err == nil {
		return ""
	}
	return "recompute_failed"
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
