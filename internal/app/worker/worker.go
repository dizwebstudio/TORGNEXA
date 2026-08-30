// Package worker is the production composition root for asynchronous TORGNEXA work.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/workflow"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/kafkaeventbus"
	"github.com/torgnexa/torgnexa/internal/platform/kafkatransport"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/outbox"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/approvalrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectormaprepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/database"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/integrationcenterrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/logisticsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/notificationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/ordersrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pricingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/retentionrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/secretrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/uploadrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/webhookrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workflowrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/reporting"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
	"github.com/torgnexa/torgnexa/internal/platform/runtimeposture"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
	"github.com/torgnexa/torgnexa/internal/platform/webhooks"
	"github.com/torgnexa/torgnexa/internal/platform/workflowengine"
)

var ErrConnectorSourceBridgeUnavailable = errors.New("worker: connector reconciliation source bridge unavailable")

// reportingKafkaConsumerGroup is deliberately independent from
// cfg.Worker.KafkaConsumerGroup: it gives Task-049 reporting ingestion its
// own Kafka offsets so a ClickHouse outage never stalls webhook delivery.
const reportingKafkaConsumerGroup = "torgnexa.reporting.v1"

const logisticsKafkaConsumerGroup = "torgnexa.logistics.v1"

type runtimeError struct {
	code  string
	cause error
}

func (e *runtimeError) Error() string     { return e.code }
func (e *runtimeError) ErrorCode() string { return e.code }
func (e *runtimeError) Unwrap() error     { return e.cause }
func fail(code string, err error) error   { return &runtimeError{code: code, cause: err} }

// ReconciliationSourceRegistry is intentionally narrower than the connector SDK.
// Provider adapters register a canonical reconciliation source without exposing
// credentials or concrete transport types to the composition root.
type ReconciliationSourceRegistry interface {
	Source(context.Context, tenancy.Scope, syncengine.Policy, sdk.Account, sdk.Manifest, sdk.Runtime) (reconciliation.Source, error)
}

type unavailableSourceRegistry struct{}

func (unavailableSourceRegistry) Source(context.Context, tenancy.Scope, syncengine.Policy, sdk.Account, sdk.Manifest, sdk.Runtime) (reconciliation.Source, error) {
	return nil, ErrConnectorSourceBridgeUnavailable
}

// Run builds the production background runtime and supervises every long-lived
// component under the process context owned by bootstrap.Run.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	return run(ctx, cfg, logger, nil)
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger, sourceRegistry ReconciliationSourceRegistry) error {
	if ctx == nil || logger == nil {
		return fail("worker_invalid_startup", errors.New("worker dependencies are required"))
	}
	workerID, err := newWorkerID()
	if err != nil {
		return fail("worker_id_startup_failed", err)
	}

	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return fail("worker_database_startup_failed", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("worker database pool close failed", "event", "worker.database_close_failed")
		}
	}()
	postureInspector, err := runtimeposture.NewInspector(db)
	if err != nil {
		return fail("worker_runtime_posture_startup_failed", err)
	}
	if _, err := postureInspector.Inspect(ctx); err != nil {
		return fail("worker_runtime_posture_unsafe", err)
	}

	secretRepository, err := secretrepo.New(db)
	if err != nil {
		return fail("worker_secret_repository_startup_failed", err)
	}
	keyring, err := secrets.NewStaticKeyring(cfg.Secrets.KeyID, map[string][]byte{cfg.Secrets.KeyID: cfg.Secrets.MasterKey})
	if err != nil {
		return fail("worker_secret_keyring_startup_failed", err)
	}
	secretProvider, err := secrets.NewLocalEncryptedProvider(secretRepository, keyring)
	if err != nil {
		return fail("worker_secret_provider_startup_failed", err)
	}

	runtimeRegistry, err := newRuntimeRegistry(db)
	if err != nil {
		return fail("worker_connector_registry_startup_failed", err)
	}
	if sourceRegistry == nil {
		sourceRegistry = runtimeRegistry
	}

	producer, err := kafkatransport.NewProducer(cfg.Worker.KafkaBrokers, workerID+".producer")
	if err != nil {
		return fail("worker_kafka_producer_startup_failed", err)
	}
	defer func() { _ = producer.Close() }()
	publisher, err := kafkaeventbus.NewPublisher(producer)
	if err != nil {
		return fail("worker_event_publisher_startup_failed", err)
	}
	outboxRepository, err := outboxrepo.New(db)
	if err != nil {
		return fail("worker_outbox_repository_startup_failed", err)
	}
	outboxPolicy := outbox.DefaultRetryPolicy()
	outboxPolicy.BatchSize = cfg.Worker.DispatchBatch
	outboxPolicy.LeaseDuration = cfg.Worker.Lease
	relay, err := outbox.NewRelay(outboxRepository, publisher, workerID, outboxPolicy)
	if err != nil {
		return fail("worker_outbox_relay_startup_failed", err)
	}

	webhookRepository, err := webhookrepo.New(db)
	if err != nil {
		return fail("worker_webhook_repository_startup_failed", err)
	}
	endpointPolicy := webhooks.NewEndpointPolicy(nil)
	webhookDelivery := &webhooks.Worker{
		Repo:      webhookRepository,
		Secrets:   secretProvider,
		Endpoints: endpointPolicy,
		Transport: webhooks.HTTPTransport{Timeout: 10 * time.Second},
		Backoff:   webhooks.DefaultBackoff(),
		WorkerID:  workerID,
		Lease:     cfg.Worker.Lease,
	}

	consumerTopics, err := kafkaConsumerTopics(cfg.Worker.KafkaTopics)
	if err != nil {
		return fail("worker_kafka_consumer_policy_startup_failed", err)
	}
	reader, err := kafkatransport.NewReader(cfg.Worker.KafkaBrokers, workerID+".consumer", cfg.Worker.KafkaConsumerGroup, consumerTopics)
	if err != nil {
		return fail("worker_kafka_reader_startup_failed", err)
	}
	defer func() { _ = reader.Close() }()
	consumer, err := kafkaeventbus.NewConsumer(reader, producer, cfg.Worker.KafkaTopics, kafkaeventbus.RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Minute}, nil)
	if err != nil {
		return fail("worker_kafka_consumer_startup_failed", err)
	}
	inboxProcessor, err := inboxrepo.New(db)
	if err != nil {
		return fail("worker_inbox_startup_failed", err)
	}
	workflowReader, err := kafkatransport.NewReader(cfg.Worker.KafkaBrokers, workerID+".workflow-consumer", "torgnexa.workflow.v1", consumerTopics)
	if err != nil {
		return fail("worker_workflow_kafka_reader_startup_failed", err)
	}
	defer func() { _ = workflowReader.Close() }()
	workflowConsumer, err := kafkaeventbus.NewConsumer(workflowReader, producer, cfg.Worker.KafkaTopics, kafkaeventbus.RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Minute}, nil)
	if err != nil {
		return fail("worker_workflow_kafka_consumer_startup_failed", err)
	}

	components := make([]component, 0, 8)
	dispatchRepository, err := workerrepo.New(db)
	if err != nil {
		return fail("worker_dispatch_repository_startup_failed", err)
	}
	shipmentRepository, err := logisticsrepo.New(db)
	if err != nil {
		return fail("worker_logistics_repository_startup_failed", err)
	}
	workflowApprovalRepository, err := approvalrepo.New(db)
	if err != nil {
		return fail("worker_workflow_approval_repository_startup_failed", err)
	}
	logisticsAccountRepository, err := connectorrepo.New(db)
	if err != nil {
		return fail("worker_logistics_connector_repository_startup_failed", err)
	}
	logisticsCancelRoute, routeErr := newLogisticsCancelRoute(shipmentRepository, logisticsAccountRepository, workflowApprovalRepository, runtimeRegistry, func(runtimeCtx context.Context, runtimeScope tenancy.Scope, account sdk.Account) (sdk.Runtime, error) {
		return connectorruntime.NewForAccount(secretProvider, secretRepository, runtimeScope, account)
	})
	if routeErr != nil {
		return fail("worker_logistics_cancel_route_startup_failed", routeErr)
	}
	logisticsReader, readerErr := kafkatransport.NewReader(cfg.Worker.KafkaBrokers, workerID+".logistics-consumer", logisticsKafkaConsumerGroup, consumerTopics)
	if readerErr != nil {
		return fail("worker_logistics_kafka_reader_startup_failed", readerErr)
	}
	defer func() { _ = logisticsReader.Close() }()
	logisticsConsumer, consumerErr := kafkaeventbus.NewConsumer(logisticsReader, producer, cfg.Worker.KafkaTopics, kafkaeventbus.RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Minute}, nil)
	if consumerErr != nil {
		return fail("worker_logistics_kafka_consumer_startup_failed", consumerErr)
	}
	components = append(components, component{name: "logistics-cancel", run: func(componentCtx context.Context) error {
		return logisticsConsumer.Run(componentCtx, logisticsCancelRoute.Handle)
	}})

	workflowRepository, err := workflowrepo.New(db)
	if err != nil {
		return fail("worker_workflow_repository_startup_failed", err)
	}
	workflowNotificationRepository, err := notificationrepo.New(db)
	if err != nil {
		return fail("worker_workflow_notification_repository_startup_failed", err)
	}
	workflowNotifications, err := notifications.NewService(workflowNotificationRepository, []notifications.Provider{notifications.WebUIProvider{}}, nil)
	if err != nil {
		return fail("worker_workflow_notification_service_startup_failed", err)
	}
	workflowReconciliationRepository, err := reconciliationrepo.New(db)
	if err != nil {
		return fail("worker_workflow_reconciliation_repository_startup_failed", err)
	}
	workflowAdapters, err := workflowengine.NewRegistry(map[string]workflowengine.Adapter{
		"notification.create": workflowNotificationAdapter{service: workflowNotifications},
		"sync.dry_run":        workflowengine.AdapterFunc(func(context.Context, workflowengine.ActionRequest) error { return nil }),
		"reconciliation.run":  workflowReconciliationAdapter{repository: workflowReconciliationRepository},
		"approval.request":    workflowApprovalAdapter{repository: workflowApprovalRepository},
	})
	if err != nil {
		return fail("worker_workflow_adapter_registry_startup_failed", err)
	}
	workflowEngine, err := workflowengine.New(workflowRepository, workflowAdapters)
	if err != nil {
		return fail("worker_workflow_engine_startup_failed", err)
	}
	socialRepository, err := socialrepo.New(db)
	if err != nil {
		return fail("worker_social_repository_startup_failed", err)
	}
	socialDispatchRepository, err := socialdispatchrepo.New(db)
	if err != nil {
		return fail("worker_social_dispatch_repository_startup_failed", err)
	}
	socialAccountRepository, err := connectorrepo.New(db)
	if err != nil {
		return fail("worker_social_account_repository_startup_failed", err)
	}

	reportingSink, err := reporting.NewClickHouseSink(reporting.ClickHouseConfig{Endpoint: cfg.ClickHouse.Endpoint, Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password, Timeout: cfg.ClickHouse.QueryTimeout})
	if err != nil {
		return fail("worker_reporting_sink_startup_failed", err)
	}
	reportingIngestor, err := reporting.NewIngestor(reportingSink, nil)
	if err != nil {
		return fail("worker_reporting_ingestor_startup_failed", err)
	}
	reportingReader, err := kafkatransport.NewReader(cfg.Worker.KafkaBrokers, workerID+".reporting-consumer", reportingKafkaConsumerGroup, consumerTopics)
	if err != nil {
		return fail("worker_reporting_kafka_reader_startup_failed", err)
	}
	defer func() { _ = reportingReader.Close() }()
	reportingConsumer, err := kafkaeventbus.NewConsumer(reportingReader, producer, cfg.Worker.KafkaTopics, kafkaeventbus.RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Minute}, nil)
	if err != nil {
		return fail("worker_reporting_kafka_consumer_startup_failed", err)
	}
	reportingIngestBatcher := newReportingBatcher(reportingBatchMaxItems, reportingBatchMaxDelay, func(batchCtx context.Context, events []eventbus.Event) error {
		return reportingIngestor.Ingest(batchCtx, events, nil, "")
	})
	integrationCenterRepository, err := integrationcenterrepo.New(db)
	if err != nil {
		return fail("worker_integration_center_repository_startup_failed", err)
	}
	integrationCenterReader, err := kafkatransport.NewReader(cfg.Worker.KafkaBrokers, workerID+".integration-center-consumer", integrationCenterKafkaConsumerGroup, consumerTopics)
	if err != nil {
		return fail("worker_integration_center_kafka_reader_startup_failed", err)
	}
	defer func() { _ = integrationCenterReader.Close() }()
	integrationCenterConsumer, err := kafkaeventbus.NewConsumer(integrationCenterReader, producer, cfg.Worker.KafkaTopics, kafkaeventbus.RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Minute}, nil)
	if err != nil {
		return fail("worker_integration_center_kafka_consumer_startup_failed", err)
	}

	components = append(components, []component{
		{name: "tenant-dispatch", run: func(componentCtx context.Context) error {
			return runTenantDispatch(componentCtx, logger, dispatchRepository, relay, webhookDelivery, cfg.Worker.PollInterval, cfg.Worker.DispatchBatch)
		}},
		{name: "kafka-webhooks", run: func(componentCtx context.Context) error {
			return consumer.Run(componentCtx, func(eventCtx context.Context, delivery eventbus.Delivery) error {
				scope, scopeErr := tenancy.ParseScope(delivery.Event.OrganizationID, delivery.Event.WorkspaceID)
				if scopeErr != nil {
					return eventbus.Permanent("inbox_invalid_scope")
				}
				handler := inboxProcessor.EventHandler(scope, cfg.Worker.KafkaConsumerGroup, func(callCtx context.Context, tx inboxrepo.Transaction, item eventbus.Delivery) error {
					if projectErr := webhookRepository.ProjectEventTransaction(callCtx, scope, tx, item, nil, time.Now().UTC()); projectErr != nil {
						return eventbus.Retryable("webhook_projection_failed")
					}
					return nil
				})
				return handler(eventCtx, delivery)
			})
		}},
		// reporting-ingest is intentionally its own Kafka consumer group so a
		// ClickHouse outage only ever grows Task-049's observable freshness
		// lag; it never blocks or retries webhook delivery. ClickHouse's
		// ReplacingMergeTree/AggregatingMergeTree schema is keyed by
		// event_id, so redelivery after a crash is safe without a Postgres
		// inbox-dedup wrapper: reporting is disposable derived state, not a
		// second transactional source of truth.
		{name: "reporting-ingest", run: func(componentCtx context.Context) error {
			return reportingConsumer.Run(componentCtx, func(eventCtx context.Context, delivery eventbus.Delivery) error {
				if ingestErr := reportingIngestBatcher.submit(eventCtx, delivery.Event); ingestErr != nil {
					return eventbus.Retryable("reporting_ingest_failed")
				}
				return nil
			})
		}},
		{name: "integration-center-events", run: func(componentCtx context.Context) error {
			return integrationCenterConsumer.Run(componentCtx, integrationCenterEventHandler{queue: integrationCenterRepository}.Handle)
		}},
	}...)
	components = append(components, component{name: "workflow-automation", run: func(componentCtx context.Context) error {
		return runWorkflowAutomation(componentCtx, logger, dispatchRepository, workflowRepository, workflowEngine, workerID, cfg.Worker)
	}})
	components = append(components, component{name: "workflow-events", run: func(componentCtx context.Context) error {
		return workflowConsumer.Run(componentCtx, func(eventCtx context.Context, delivery eventbus.Delivery) error {
			scope, scopeErr := workflow.ParseScope(delivery.Event.OrganizationID, delivery.Event.WorkspaceID)
			if scopeErr != nil {
				return eventbus.Permanent("workflow_invalid_scope")
			}
			tenantScope, tenantScopeErr := tenancy.ParseScope(scope.OrganizationID(), scope.WorkspaceID())
			if tenantScopeErr != nil {
				return eventbus.Permanent("workflow_invalid_scope")
			}
			// Keep the workflow run insert and the Inbox receipt in one
			// PostgreSQL transaction. This is the durable deduplication
			// boundary for Kafka redelivery; a separate TriggerEvent
			// transaction could otherwise commit a run and lose its receipt
			// between process crashes.
			handler := inboxProcessor.EventHandler(tenantScope, "workflow.triggers.v1", func(callCtx context.Context, tx inboxrepo.Transaction, item eventbus.Delivery) error {
				if _, triggerErr := workflowRepository.TriggerEventInTransaction(callCtx, scope, tx, item.Event); triggerErr != nil {
					return eventbus.Retryable("workflow_trigger_persist_failed")
				}
				return nil
			})
			return handler(eventCtx, delivery)
		})
	}})
	components = append(components, component{name: "social-publications", run: func(componentCtx context.Context) error {
		return runSocialPublications(componentCtx, logger, dispatchRepository, socialRepository, socialAccountRepository, socialDispatchRepository, secretProvider, secretRepository, runtimeRegistry, workerID, cfg.Worker.PollInterval, cfg.Worker.DispatchBatch, cfg.Worker.Lease)
	}})

	fxReferenceResolver, err := newFXReferenceResolver(db, runtimeRegistry.builtins)
	if err != nil {
		return fail("worker_fx_reference_startup_failed", err)
	}
	components = append(components, component{name: "fx-reference", run: func(componentCtx context.Context) error {
		return runFXReferenceRefresh(componentCtx, logger, fxReferenceResolver)
	}})

	if cfg.Worker.ReconciliationEnabled {
		syncRepository, repoErr := syncrepo.New(db)
		if repoErr != nil {
			return fail("worker_sync_repository_startup_failed", repoErr)
		}
		reconciliationRepository := workflowReconciliationRepository
		accountRepository, repoErr := connectorrepo.New(db)
		if repoErr != nil {
			return fail("worker_connector_repository_startup_failed", repoErr)
		}
		mappingRepository, repoErr := connectormaprepo.New(db)
		if repoErr != nil {
			return fail("worker_mapping_repository_startup_failed", repoErr)
		}
		catalogRepository, repoErr := catalogrepo.New(db)
		if repoErr != nil {
			return fail("worker_catalog_repository_startup_failed", repoErr)
		}
		publicationQualityRepository, repoErr := publicationqualityrepo.New(db)
		if repoErr != nil {
			return fail("worker_publication_quality_repository_startup_failed", repoErr)
		}
		orderRepository, repoErr := ordersrepo.New(db)
		if repoErr != nil {
			return fail("worker_orders_repository_startup_failed", repoErr)
		}
		priceRepository, repoErr := pricingrepo.New(db)
		if repoErr != nil {
			return fail("worker_pricing_repository_startup_failed", repoErr)
		}
		inventoryRepository, repoErr := inventoryrepo.New(db)
		if repoErr != nil {
			return fail("worker_inventory_repository_startup_failed", repoErr)
		}
		paymentsRepository, repoErr := paymentsrepo.New(db)
		if repoErr != nil {
			return fail("worker_payments_repository_startup_failed", repoErr)
		}
		approvalRepository := workflowApprovalRepository
		notificationRepository, repoErr := notificationrepo.New(db)
		if repoErr != nil {
			return fail("worker_notification_repository_startup_failed", repoErr)
		}
		actionExecutor, actionErr := newReconciliationActionExecutor(syncRepository, accountRepository, mappingRepository, catalogRepository, orderRepository, priceRepository, inventoryRepository, approvalRepository, notificationRepository, secretProvider, secretRepository, runtimeRegistry)
		if actionErr != nil {
			return fail("worker_reconciliation_action_startup_failed", actionErr)
		}
		engine, engineErr := reconciliation.New(syncRepository, reconciliationRepository, actionExecutor)
		if engineErr != nil {
			return fail("worker_reconciliation_engine_startup_failed", engineErr)
		}
		resolver := &sourceResolver{syncRepo: syncRepository, accounts: accountRepository, secrets: secretProvider, oauthRefresh: secretRepository, registry: sourceRegistry}
		components = append(components, component{name: "reconciliation", run: func(componentCtx context.Context) error {
			return runReconciliation(componentCtx, logger, dispatchRepository, engine, reconciliationRepository, resolver, runtimeRegistry, workerID, cfg.Worker)
		}})
		commerceRoute, routeErr := newCommerceWriteRoute(syncRepository, accountRepository, mappingRepository, catalogRepository, runtimeRegistry, func(runtimeCtx context.Context, runtimeScope tenancy.Scope, account sdk.Account) (sdk.Runtime, error) {
			return connectorruntime.NewForAccount(secretProvider, secretRepository, runtimeScope, account)
		}, postgresPublicationQualityGate{repository: publicationQualityRepository})
		if routeErr != nil {
			return fail("worker_commerce_sync_route_startup_failed", routeErr)
		}
		commerceReader, readerErr := kafkatransport.NewReader(cfg.Worker.KafkaBrokers, workerID+".commerce-sync-consumer", commerceSyncKafkaConsumerGroup, consumerTopics)
		if readerErr != nil {
			return fail("worker_commerce_sync_kafka_reader_startup_failed", readerErr)
		}
		defer func() { _ = commerceReader.Close() }()
		commerceConsumer, consumerErr := kafkaeventbus.NewConsumer(commerceReader, producer, cfg.Worker.KafkaTopics, kafkaeventbus.RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Minute}, nil)
		if consumerErr != nil {
			return fail("worker_commerce_sync_kafka_consumer_startup_failed", consumerErr)
		}
		components = append(components, component{name: "commerce-sync", run: func(componentCtx context.Context) error {
			return commerceConsumer.Run(componentCtx, commerceRoute.Handle)
		}})
		components = append(components, component{name: "payment-reconciliation", run: func(componentCtx context.Context) error {
			return runPaymentReconciliation(componentCtx, logger, dispatchRepository, accountRepository, paymentsRepository, secretProvider, secretRepository, runtimeRegistry, cfg.Worker.PollInterval)
		}})
	}

	privacyRepository, repoErr := retentionrepo.New(db)
	if repoErr != nil {
		return fail("worker_privacy_repository_startup_failed", repoErr)
	}
	privacyStore, repoErr := retentionrepo.NewPrivacyStore(db, secretProvider)
	if repoErr != nil {
		return fail("worker_privacy_store_startup_failed", repoErr)
	}
	privacyAuditRepository, repoErr := auditrepo.New(db)
	if repoErr != nil {
		return fail("worker_privacy_audit_repository_startup_failed", repoErr)
	}
	privacyAuditor, repoErr := audit.NewService(privacyAuditRepository)
	if repoErr != nil {
		return fail("worker_privacy_audit_startup_failed", repoErr)
	}
	privacyService, repoErr := retention.NewService(privacyRepository, []retention.Store{privacyStore}, privacyAuditor)
	if repoErr != nil {
		return fail("worker_privacy_service_startup_failed", repoErr)
	}
	components = append(components, component{name: "privacy", run: func(componentCtx context.Context) error {
		return runPrivacy(componentCtx, logger, dispatchRepository, privacyService, workerID, cfg.Worker)
	}})

	warehouseRepository, repoErr := inventoryrepo.New(db)
	if repoErr != nil {
		return fail("worker_warehouse_incident_repository_startup_failed", repoErr)
	}
	components = append(components, component{name: "warehouse-incidents", run: func(componentCtx context.Context) error {
		return runWarehouseIncidents(componentCtx, logger, dispatchRepository, warehouseRepository, workerID, cfg.Worker)
	}})

	if cfg.Worker.UploadsEnabled {
		policy := uploads.DefaultPolicy()
		policy.MaxFileBytes = cfg.Security.MaxUploadBytes
		uploadRepository, repoErr := uploadrepo.New(db, policy.MaxFileBytes)
		if repoErr != nil {
			return fail("worker_upload_repository_startup_failed", repoErr)
		}
		storage, storageErr := uploads.NewS3QuarantineStore(uploads.S3Config{
			Endpoint: cfg.ObjectStorage.Endpoint, Bucket: cfg.ObjectStorage.Bucket, Region: cfg.ObjectStorage.Region,
			AccessKey: cfg.ObjectStorage.AccessKey, SecretKey: cfg.ObjectStorage.SecretKey, Timeout: cfg.ObjectStorage.Timeout,
			MaxBytes: policy.MaxFileBytes,
		})
		if storageErr != nil {
			return fail("worker_upload_storage_startup_failed", storageErr)
		}
		scanner, scannerErr := uploads.NewClamAVScanner(uploads.ClamAVConfig{
			Network: cfg.Worker.ClamAVNetwork, Address: cfg.Worker.ClamAVAddress,
			EngineVersion: cfg.Worker.ClamAVEngineVersion, SignatureVersion: cfg.Worker.ClamAVSignatureVersion,
			Timeout: cfg.Worker.ClamAVTimeout, MaxBytes: policy.MaxFileBytes,
		})
		if scannerErr != nil {
			return fail("worker_upload_scanner_startup_failed", scannerErr)
		}
		pipeline, pipelineErr := uploads.NewPipeline(uploadRepository, storage, storage, scanner, nil, policy)
		if pipelineErr != nil {
			return fail("worker_upload_pipeline_startup_failed", pipelineErr)
		}
		components = append(components, component{name: "upload-security", run: func(componentCtx context.Context) error {
			return runUploads(componentCtx, logger, dispatchRepository, pipeline, workerID, cfg.Worker)
		}})
	}

	logger.Info("worker runtime ready", "event", "worker.runtime_ready", "worker_id", workerID, "components", len(components))
	return supervise(ctx, logger, components)
}

func kafkaConsumerTopics(base []string) ([]string, error) {
	if len(base) == 0 {
		return nil, errors.New("worker: at least one Kafka base topic is required")
	}
	out := make([]string, 0, len(base)*2)
	seen := make(map[string]struct{}, len(base))
	for _, topic := range base {
		if _, exists := seen[topic]; exists {
			return nil, errors.New("worker: duplicate Kafka base topic")
		}
		seen[topic] = struct{}{}
		if err := kafkaeventbus.ValidateTopic(topic); err != nil {
			return nil, err
		}
		retry, err := kafkaeventbus.RetryTopic(topic)
		if err != nil {
			return nil, err
		}
		out = append(out, topic, retry)
	}
	return out, nil
}

type component struct {
	name string
	run  func(context.Context) error
}

func supervise(ctx context.Context, logger *slog.Logger, components []component) error {
	if ctx == nil || logger == nil || len(components) == 0 {
		return fail("worker_supervisor_invalid", errors.New("supervisor components required"))
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(components))
	var wg sync.WaitGroup
	for _, c := range components {
		if c.name == "" || c.run == nil {
			return fail("worker_supervisor_invalid_component", errors.New("invalid component"))
		}
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("worker component started", "event", "worker.component_started", "component", c.name)
			err := c.run(child)
			results <- result{name: c.name, err: err}
		}()
	}

	select {
	case <-ctx.Done():
		cancel()
		wg.Wait()
		return context.Canceled
	case r := <-results:
		cancel()
		wg.Wait()
		if r.err == nil || errors.Is(r.err, context.Canceled) {
			if ctx.Err() != nil {
				return context.Canceled
			}
			return fail("worker_component_stopped", fmt.Errorf("component %s stopped unexpectedly", r.name))
		}
		logger.Error("worker component failed", "event", "worker.component_failed", "component", r.name, "error", r.err)
		return fail("worker_component_failed", fmt.Errorf("component %s: %w", r.name, r.err))
	}
}

func runTenantDispatch(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, relay *outbox.Relay, webhookWorker *webhooks.Worker, poll time.Duration, batch int) error {
	return pollLoop(ctx, poll, func() error {
		scopes, err := dispatch.ActiveScopes(ctx, batch)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if _, err := relay.RunOnce(ctx, scope); err != nil {
				return err
			}
			for i := 0; i < batch; i++ {
				processed, err := webhookWorker.ProcessOne(ctx, scope)
				if err != nil {
					return err
				}
				if !processed {
					break
				}
			}
		}
		if len(scopes) > 0 {
			logger.Debug("worker tenant dispatch completed", "event", "worker.tenant_dispatch", "scopes", len(scopes))
		}
		return nil
	})
}

func runWorkflowAutomation(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, repository *workflowrepo.Repository, engine *workflowengine.Engine, workerID string, cfg config.Worker) error {
	return pollLoop(ctx, cfg.PollInterval, func() error {
		scopes, err := dispatch.ActiveScopes(ctx, cfg.DispatchBatch)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			leaseToken, tokenErr := randomID("workflow_lease_")
			if tokenErr != nil {
				return tokenErr
			}
			claims, claimErr := repository.ClaimRuns(ctx, workflowScope(scope), workerID, leaseToken, cfg.DispatchBatch, cfg.Lease)
			if claimErr != nil {
				return claimErr
			}
			for _, claim := range claims {
				if execErr := engine.Execute(ctx, workflowScope(scope), claim.Run.ID, claim.LeaseToken); execErr != nil && !errors.Is(execErr, workflowengine.ErrApprovalRequired) {
					logger.Warn("workflow run deferred", "event", "workflow.run_deferred", "run_id", claim.Run.ID, "error_code", "workflow_execution_failed")
				}
			}
		}
		return nil
	})
}

func workflowScope(scope tenancy.Scope) workflow.Scope {
	converted, _ := workflow.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	return converted
}

type workflowNotificationAdapter struct{ service *notifications.Service }

func (a workflowNotificationAdapter) Execute(ctx context.Context, request workflowengine.ActionRequest) error {
	if a.service == nil {
		return workflowengine.ErrAdapterUnavailable
	}
	var input struct {
		RecipientID string                 `json:"recipient_id"`
		DedupeKey   string                 `json:"dedupe_key"`
		Severity    notifications.Severity `json:"severity"`
		Title       string                 `json:"title"`
		Body        string                 `json:"body"`
	}
	if len(request.Node.Config) > 0 {
		if err := json.Unmarshal(request.Node.Config, &input); err != nil {
			return workflow.ErrInvalid
		}
	}
	if input.RecipientID == "" {
		input.RecipientID = "operator"
	}
	if input.DedupeKey == "" {
		input.DedupeKey = "workflow." + request.RunID + "." + request.Node.ID
	}
	if !input.Severity.Valid() {
		input.Severity = notifications.SeverityInfo
	}
	if input.Title == "" {
		input.Title = "Автоматизация завершена"
	}
	if input.Body == "" {
		input.Body = "Workflow action выполнен."
	}
	scope, err := tenancy.ParseScope(request.Scope.OrganizationID(), request.Scope.WorkspaceID())
	if err != nil {
		return workflow.ErrInvalid
	}
	_, err = a.service.Notify(ctx, scope, notifications.Request{RecipientID: input.RecipientID, DedupeKey: input.DedupeKey, Severity: input.Severity, Title: input.Title, Body: input.Body, OccurredAt: time.Now().UTC()})
	return err
}

// workflowApprovalAdapter bridges the declarative approval node to the normal
// Task-017 repository.  The node only carries an opaque resource reference;
// policy lookup, audit and outbox publication remain owned by approvalrepo.
type workflowApprovalAdapter struct{ repository *approvalrepo.Repository }

func (a workflowApprovalAdapter) Execute(ctx context.Context, request workflowengine.ActionRequest) error {
	if a.repository == nil || ctx == nil {
		return workflowengine.ErrAdapterUnavailable
	}
	var input struct {
		Action       string             `json:"action"`
		ResourceType string             `json:"resource_type"`
		ResourceID   string             `json:"resource_id"`
		Risk         approval.RiskClass `json:"risk"`
	}
	if len(request.Node.Config) > 0 {
		if err := json.Unmarshal(request.Node.Config, &input); err != nil {
			return workflow.ErrInvalid
		}
	}
	// Defaults make a node usable in the builder while still requiring an
	// explicitly installed tenant policy for the resulting request.
	if input.Action == "" {
		input.Action = "workflow.sensitive_action"
	}
	if input.ResourceType == "" {
		input.ResourceType = "workflow_run"
	}
	if input.ResourceID == "" {
		input.ResourceID = request.RunID + ":" + request.Node.ID
	}
	if input.Risk == "" {
		input.Risk = approval.RiskWriteSensitive
	}
	if !input.Risk.Valid() || len(input.Action) > 160 || len(input.ResourceType) > 128 || len(input.ResourceID) > 512 {
		return workflow.ErrInvalid
	}
	scope, err := tenancy.ParseScope(request.Scope.OrganizationID(), request.Scope.WorkspaceID())
	if err != nil {
		return workflow.ErrInvalid
	}
	requestID := stableUUID("workflow.approval:" + request.Scope.OrganizationID() + ":" + request.Scope.WorkspaceID() + ":" + request.RunID + ":" + request.Node.ID)
	if existing, lookupErr := a.repository.Request(ctx, scope, requestID); lookupErr == nil && existing.ID == requestID {
		switch existing.State {
		case approval.StateApproved:
			return nil
		case approval.StateRejected, approval.StateExpired, approval.StateCancelled, approval.StateFailed:
			return approval.ErrDenied
		default:
			return workflowengine.ErrApprovalRequired
		}
	}
	now := time.Now().UTC()
	mutationKey := "workflow.approval:" + requestID
	_, err = a.repository.CreateRequest(ctx, scope, input.Action, input.ResourceType, approval.RequestCommand{
		RequestID:  requestID,
		ResourceID: input.ResourceID,
		Risk:       input.Risk,
		Mutation: approval.Mutation{
			AuditID:       stableUUID(mutationKey + ":audit"),
			EventID:       stableUUID(mutationKey + ":event"),
			ActorID:       "workflow.engine",
			Source:        "workflow.engine",
			CorrelationID: requestID,
			CausationID:   request.Node.ID,
			OccurredAt:    now,
		},
	})
	if err != nil {
		// Missing/insufficient policy is deliberately fail-closed.  It must not
		// be represented as an approval wait because no operator request exists.
		if errors.Is(err, approval.ErrDenied) {
			return workflowengine.ErrAdapterUnavailable
		}
		// A concurrent delivery may have created the deterministic request
		// between the lookup and insert.  Re-read it and preserve its state.
		if existing, lookupErr := a.repository.Request(ctx, scope, requestID); lookupErr == nil && existing.ID == requestID {
			if existing.State == approval.StateApproved {
				return nil
			}
			if existing.State == approval.StatePending {
				return workflowengine.ErrApprovalRequired
			}
		}
		return err
	}
	return workflowengine.ErrApprovalRequired
}

// workflowReconciliationAdapter creates the existing Task-014 run record.
// The normal reconciliation worker owns transport resolution and execution;
// this adapter only submits a bounded, tenant-scoped request to that durable
// queue and never touches a connector itself.
type workflowReconciliationAdapter struct {
	repository *reconciliationrepo.Repository
}

func (a workflowReconciliationAdapter) Execute(ctx context.Context, request workflowengine.ActionRequest) error {
	if a.repository == nil || ctx == nil {
		return workflowengine.ErrAdapterUnavailable
	}
	var input struct {
		PolicyID   string              `json:"policy_id"`
		Mode       reconciliation.Mode `json:"mode"`
		TriggerRef string              `json:"trigger_ref"`
	}
	if len(request.Node.Config) > 0 {
		if err := json.Unmarshal(request.Node.Config, &input); err != nil {
			return workflow.ErrInvalid
		}
	}
	if input.PolicyID == "" || len(input.PolicyID) > 128 {
		return workflow.ErrInvalid
	}
	if input.Mode == "" {
		input.Mode = reconciliation.ModeOnDemand
	}
	if !input.Mode.Valid() {
		return workflow.ErrInvalid
	}
	if input.TriggerRef == "" {
		input.TriggerRef = request.RunID
	}
	if len(input.TriggerRef) > 128 {
		return workflow.ErrInvalid
	}
	scope, err := tenancy.ParseScope(request.Scope.OrganizationID(), request.Scope.WorkspaceID())
	if err != nil {
		return workflow.ErrInvalid
	}
	runID := stableID("rec_wf_", request.RunID+":"+request.Node.ID)
	if existing, lookupErr := a.repository.Run(ctx, scope, runID); lookupErr == nil && existing.ID == runID {
		return nil
	}
	now := time.Now().UTC()
	_, err = a.repository.CreateRun(ctx, scope, reconciliation.Run{ID: runID, PolicyID: input.PolicyID, Mode: input.Mode, TriggerRef: input.TriggerRef, Status: reconciliation.RunRunning, Version: 1, StartedAt: now, UpdatedAt: now})
	if err != nil {
		// A worker crash after commit but before evidence publication is safe to
		// replay: the deterministic run identity is the idempotency boundary.
		if existing, lookupErr := a.repository.Run(ctx, scope, runID); lookupErr == nil && existing.ID == runID {
			return nil
		}
		return err
	}
	return nil
}

type sourceResolver struct {
	syncRepo     *syncrepo.Repository
	accounts     *connectorrepo.Repository
	secrets      secrets.SecretProvider
	oauthRefresh connectorauth.RefreshCoordinator
	registry     ReconciliationSourceRegistry
}

func (r *sourceResolver) Resolve(ctx context.Context, scope tenancy.Scope, run reconciliation.Run) (reconciliation.Source, syncengine.Policy, sdk.Account, error) {
	if r == nil || r.syncRepo == nil || r.accounts == nil || r.secrets == nil || r.oauthRefresh == nil || r.registry == nil {
		return nil, syncengine.Policy{}, sdk.Account{}, ErrConnectorSourceBridgeUnavailable
	}
	policy, err := r.syncRepo.Policy(ctx, scope, run.PolicyID)
	if err != nil {
		return nil, syncengine.Policy{}, sdk.Account{}, err
	}
	account, err := r.accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.ConnectorAccountID)
	if err != nil {
		return nil, syncengine.Policy{}, sdk.Account{}, err
	}
	if account.Status != sdk.AccountActive {
		return nil, syncengine.Policy{}, sdk.Account{}, errors.New("connector account is not active")
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		return nil, syncengine.Policy{}, sdk.Account{}, err
	}
	runtime, err := connectorruntime.NewForAccount(r.secrets, r.oauthRefresh, scope, account)
	if err != nil {
		return nil, syncengine.Policy{}, sdk.Account{}, err
	}
	source, err := r.registry.Source(ctx, scope, policy, account, manifest, runtime)
	if err != nil {
		return nil, syncengine.Policy{}, sdk.Account{}, err
	}
	return source, policy, account, nil
}

func sourceBridgeUnavailable(err error) bool {
	return errors.Is(err, ErrConnectorSourceBridgeUnavailable)
}

func runReconciliation(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, engine *reconciliation.Engine, repo *reconciliationrepo.Repository, resolver *sourceResolver, registry *runtimeRegistry, workerID string, cfg config.Worker) error {
	return pollLoop(ctx, cfg.PollInterval, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindReconciliation, workerID, cfg.DispatchBatch, cfg.Lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			run, readErr := repo.Run(ctx, job.Scope, job.ItemID)
			if readErr != nil {
				_ = dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "reconciliation_read_failed")
				continue
			}
			source, policy, account, resolveErr := resolver.Resolve(ctx, job.Scope, run)
			if resolveErr != nil {
				code := "reconciliation_source_unavailable"
				if sourceBridgeUnavailable(resolveErr) {
					code = "connector_bridge_unavailable"
				}
				if releaseErr := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), code); releaseErr != nil {
					return releaseErr
				}
				logger.Warn("reconciliation job deferred", "event", "worker.reconciliation_deferred", "run_id", job.ItemID, "error_code", code)
				continue
			}
			outboundWritable := registry != nil && registry.supportsWriteForEntity(account, policy.EntityType)
			actions := actionPolicyFor(policy, outboundWritable)
			_, execErr := engine.Resume(ctx, job.Scope, job.ItemID, 24*time.Hour, actions, reconciliation.MaxPageSize, source)
			if execErr != nil {
				if releaseErr := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "reconciliation_execution_failed"); releaseErr != nil {
					return releaseErr
				}
				continue
			}
			if err := dispatch.Complete(ctx, job); err != nil {
				return err
			}
		}
		return nil
	})
}

func runPrivacy(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, service *retention.Service, workerID string, cfg config.Worker) error {
	return pollLoop(ctx, cfg.PollInterval, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindPrivacy, workerID, cfg.DispatchBatch, cfg.Lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			result, advanceErr := service.Advance(ctx, job.Scope, job.ItemID, 20)
			switch {
			case advanceErr == nil && result.Status == retention.StatusCompleted:
				if err := dispatch.Complete(ctx, job); err != nil {
					return err
				}
			case errors.Is(advanceErr, retention.ErrLegalHold):
				if err := dispatch.Release(ctx, job, 5*time.Minute, "privacy_legal_hold"); err != nil {
					return err
				}
			case errors.Is(advanceErr, retention.ErrManualReview), errors.Is(advanceErr, retention.ErrUnsupported):
				if err := dispatch.Release(ctx, job, time.Hour, "privacy_manual_review"); err != nil {
					return err
				}
			case advanceErr != nil:
				if err := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "privacy_execution_failed"); err != nil {
					return err
				}
			default:
				if err := dispatch.Release(ctx, job, time.Second, "privacy_in_progress"); err != nil {
					return err
				}
			}
			if advanceErr != nil {
				logger.Warn("privacy job deferred", "event", "worker.privacy_deferred", "job_id", job.ItemID)
			}
		}
		return nil
	})
}

func runWarehouseIncidents(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, repository *inventoryrepo.Repository, workerID string, cfg config.Worker) error {
	return pollLoop(ctx, cfg.PollInterval, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindWarehouseIncident, workerID, cfg.DispatchBatch, cfg.Lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			scope, scopeErr := inventory.ParseScope(job.Scope.OrganizationID().String(), job.Scope.WorkspaceID().String())
			if scopeErr != nil {
				if err := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "warehouse_incident_failed"); err != nil {
					return err
				}
				logger.Warn("warehouse incident deferred", "event", "worker.warehouse_incident_deferred", "incident_id", job.ItemID)
				continue
			}
			incident, processErr := repository.ProcessWarehouseIncidentBatch(ctx, scope, job.ItemID, 200)
			if processErr != nil {
				if err := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "warehouse_incident_failed"); err != nil {
					return err
				}
				logger.Warn("warehouse incident deferred", "event", "worker.warehouse_incident_deferred", "incident_id", job.ItemID)
				continue
			}
			switch incident.Status {
			case "completed", "needs_attention", "resolved":
				if err := dispatch.Complete(ctx, job); err != nil {
					return err
				}
				level := slog.LevelInfo
				if incident.Status == "needs_attention" {
					level = slog.LevelWarn
				}
				logger.Log(ctx, level, "warehouse incident processed", "event", "worker.warehouse_incident_processed", "incident_id", incident.ID, "warehouse_id", incident.WarehouseID.String(), "status", string(incident.Status), "routed", incident.RoutedCount, "no_route", incident.NoRouteCount, "rerouted_allocations", incident.ReroutedAllocationCount, "execution_attention", incident.ExecutionAttentionCount)
			default:
				if err := dispatch.Release(ctx, job, 100*time.Millisecond, "warehouse_incident_in_progress"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func runUploads(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, pipeline *uploads.Pipeline, workerID string, cfg config.Worker) error {
	return pollLoop(ctx, cfg.PollInterval, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindUpload, workerID, cfg.DispatchBatch, cfg.Lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			mutation, mutationErr := uploadMutation(job.ItemID)
			if mutationErr != nil {
				return mutationErr
			}
			_, processErr := pipeline.Process(ctx, job.Scope, uploads.ID(job.ItemID), mutation)
			switch {
			case processErr == nil, errors.Is(processErr, uploads.ErrSecurityRejected):
				if err := dispatch.Complete(ctx, job); err != nil {
					return err
				}
			case errors.Is(processErr, uploads.ErrScannerUnavailable), errors.Is(processErr, uploads.ErrStorage):
				if err := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "upload_dependency_unavailable"); err != nil {
					return err
				}
			default:
				if err := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "upload_pipeline_failed"); err != nil {
					return err
				}
			}
			if processErr != nil {
				logger.Warn("upload security job did not complete cleanly", "event", "worker.upload_result", "upload_id", job.ItemID, "error_code", uploadErrorCode(processErr))
			}
		}
		return nil
	})
}

func pollLoop(ctx context.Context, interval time.Duration, work func() error) error {
	if interval <= 0 || work == nil {
		return errors.New("worker: invalid poll loop")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := work(); err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second
	for i := 1; i < attempt && delay < 15*time.Minute; i++ {
		delay *= 2
		if delay > 15*time.Minute {
			return 15 * time.Minute
		}
	}
	return delay
}

func uploadMutation(itemID string) (uploads.Mutation, error) {
	id, err := randomID("evt_worker_upload_")
	if err != nil {
		return uploads.Mutation{}, err
	}
	return uploads.Mutation{EventID: id, OccurredAt: time.Now().UTC(), Source: "worker.upload", CausationID: itemID}, nil
}

func newWorkerID() (string, error) { return randomID("worker_") }

func randomID(prefix string) (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}

func uploadErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, uploads.ErrSecurityRejected):
		return "security_rejected"
	case errors.Is(err, uploads.ErrScannerUnavailable):
		return "scanner_unavailable"
	case errors.Is(err, uploads.ErrStorage):
		return "storage_unavailable"
	case errors.Is(err, uploads.ErrInvalid):
		return "invalid_pipeline_state"
	case errors.Is(err, uploads.ErrConflict):
		return "pipeline_conflict"
	case errors.Is(err, uploads.ErrNotFound):
		return "upload_not_found"
	default:
		return "pipeline_failed"
	}
}
