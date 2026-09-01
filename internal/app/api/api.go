// Package api implements the TORGNEXA HTTP API process boundary.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/advertisingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/agentgovernancerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/aiadvisoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/approvalrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogimagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/cloudbillingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/compliancerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/database"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/entitlementrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/financialrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/fxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/legalpartyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/lineagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/logisticsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplaceoperationsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacepublicationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/markingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/mcpaccountsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/notificationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/operatorassistantrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/ordersrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pimrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pluginmarketplacerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pricingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/procurementrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/replenishmentrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reportrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/retentionrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/returnsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/searchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/secretrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/securitysettingsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/settlementrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/trustcontrolrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/uploadrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/userprofilerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/webhookrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workflowrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reporting"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
	"github.com/torgnexa/torgnexa/internal/platform/runtimeposture"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
	"github.com/torgnexa/torgnexa/internal/platform/webhooks"
)

// HealthPath is the public liveness endpoint declared by the v1 OpenAPI contract.
const HealthPath = "/api/v1/health"

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type problemResponse struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
}

type runtimeError struct {
	code  string
	cause error
}

func (e *runtimeError) Error() string     { return e.code }
func (e *runtimeError) ErrorCode() string { return e.code }
func (e *runtimeError) Unwrap() error     { return e.cause }

func newRuntimeError(code string, cause error) error {
	return &runtimeError{code: code, cause: cause}
}

// newHealthHandler is retained only for deterministic package tests. Production runtime uses NewProductionHandler.
func newHealthHandler(logger *slog.Logger) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(route))
}

// Run listens for HTTP requests until the process context is canceled. It applies
// bounded server timeouts and drains active requests during graceful shutdown.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if logger == nil {
		return fmt.Errorf("logger is required")
	}
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return newRuntimeError("database_startup_failed", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("database pool close failed", "event", "database.pool_close_failed")
		}
	}()
	logger.Info("database pool ready", "event", "database.pool_ready", "max_open_connections", cfg.Database.MaxOpenConns, "max_idle_connections", cfg.Database.MaxIdleConns)
	inboundWebhookInbox, err := inboxrepo.New(db)
	if err != nil {
		return newRuntimeError("inbound_webhook_inbox_startup_failed", err)
	}
	postureInspector, err := runtimeposture.NewInspector(db)
	if err != nil {
		return newRuntimeError("runtime_posture_startup_failed", err)
	}
	if _, err := postureInspector.Inspect(ctx); err != nil {
		return newRuntimeError("runtime_posture_unsafe", err)
	}
	accountRepository, err := connectorrepo.New(db)
	if err != nil {
		return newRuntimeError("connector_repository_startup_failed", err)
	}
	connectorConfigRepository, err := connectorconfigrepo.New(db)
	if err != nil {
		return newRuntimeError("connector_config_repository_startup_failed", err)
	}
	auditRepository, err := auditrepo.New(db)
	if err != nil {
		return newRuntimeError("audit_repository_startup_failed", err)
	}
	approvalRepository, err := approvalrepo.New(db)
	if err != nil {
		return newRuntimeError("approval_repository_startup_failed", err)
	}
	workflowRepository, err := workflowrepo.New(db)
	if err != nil {
		return newRuntimeError("workflow_repository_startup_failed", err)
	}
	returnsRepository, err := returnsrepo.New(db)
	if err != nil {
		return newRuntimeError("returns_repository_startup_failed", err)
	}
	replenishmentRepository, err := replenishmentrepo.New(db)
	if err != nil {
		return newRuntimeError("replenishment_repository_startup_failed", err)
	}
	auditService, err := audit.NewService(auditRepository)
	if err != nil {
		return newRuntimeError("audit_service_startup_failed", err)
	}
	secretRepository, err := secretrepo.New(db)
	if err != nil {
		return newRuntimeError("secret_repository_startup_failed", err)
	}
	keyring, err := secrets.NewStaticKeyring(cfg.Secrets.KeyID, map[string][]byte{cfg.Secrets.KeyID: cfg.Secrets.MasterKey})
	if err != nil {
		return newRuntimeError("secret_keyring_startup_failed", err)
	}
	secretProvider, err := secrets.NewLocalEncryptedProvider(secretRepository, keyring)
	if err != nil {
		return newRuntimeError("secret_provider_startup_failed", err)
	}
	connectorCallbacks, err := connectorauth.NewCallbackPolicy(cfg.Security.AllowedOrigins)
	if err != nil {
		return newRuntimeError("connector_callback_policy_startup_failed", err)
	}
	tenantRepository, err := tenancyrepo.New(db)
	if err != nil {
		return newRuntimeError("tenancy_repository_startup_failed", err)
	}
	profileRepository, err := userprofilerepo.New(db)
	if err != nil {
		return newRuntimeError("user_profile_repository_startup_failed", err)
	}
	settingsSecurityRepository, err := securitysettingsrepo.New(db)
	if err != nil {
		return newRuntimeError("settings_security_repository_startup_failed", err)
	}
	identityPolicy, err := securitysettings.NewProviderURLPolicy(cfg.OIDC.ManagedIssuerHosts, cfg.Security.AllowedOrigins, nil)
	if err != nil {
		return newRuntimeError("identity_provider_policy_startup_failed", err)
	}
	identityValidator, err := securitysettings.NewOIDCDiscoveryValidator(identityPolicy, cfg.OIDC.RequestTimeout)
	if err != nil {
		return newRuntimeError("identity_provider_validator_startup_failed", err)
	}
	searchRepository, err := searchrepo.New(db)
	if err != nil {
		return newRuntimeError("search_repository_startup_failed", err)
	}
	orderRepository, err := ordersrepo.New(db)
	if err != nil {
		return newRuntimeError("orders_repository_startup_failed", err)
	}
	marketplaceOperationsRepository, err := marketplaceoperationsrepo.New(db)
	if err != nil {
		return newRuntimeError("marketplace_operations_repository_startup_failed", err)
	}
	catalogRepository, err := catalogrepo.New(db)
	if err != nil {
		return newRuntimeError("catalog_repository_startup_failed", err)
	}
	pricingRepository, err := pricingrepo.New(db)
	if err != nil {
		return newRuntimeError("pricing_repository_startup_failed", err)
	}
	publicationQualityRepository, err := publicationqualityrepo.New(db)
	if err != nil {
		return newRuntimeError("publication_quality_repository_startup_failed", err)
	}
	marketplacePublicationRepository, err := marketplacepublicationrepo.New(db)
	if err != nil {
		return newRuntimeError("marketplace_publication_repository_startup_failed", err)
	}
	procurementRepository, err := procurementrepo.New(db)
	if err != nil {
		return newRuntimeError("procurement_repository_startup_failed", err)
	}
	pimRepository, err := pimrepo.New(db)
	if err != nil {
		return newRuntimeError("pim_repository_startup_failed", err)
	}
	imageRepository, err := catalogimagerepo.New(db)
	if err != nil {
		return newRuntimeError("catalog_image_repository_startup_failed", err)
	}
	aiAdvisoryRepository, err := aiadvisoryrepo.New(db)
	if err != nil {
		return newRuntimeError("ai_advisory_repository_startup_failed", err)
	}
	operatorAssistantRepository, err := operatorassistantrepo.New(db)
	if err != nil {
		return newRuntimeError("operator_assistant_repository_startup_failed", err)
	}
	mcpAccountsRepository, err := mcpaccountsrepo.New(db)
	if err != nil {
		return newRuntimeError("mcp_accounts_repository_startup_failed", err)
	}
	agentGovernanceRepository, err := agentgovernancerepo.New(db)
	if err != nil {
		return newRuntimeError("agent_governance_repository_startup_failed", err)
	}
	trustControlRepository, err := trustcontrolrepo.New(db)
	if err != nil {
		return newRuntimeError("trust_control_repository_startup_failed", err)
	}
	inventoryRepository, err := inventoryrepo.New(db)
	if err != nil {
		return newRuntimeError("inventory_repository_startup_failed", err)
	}
	markingRepository, err := markingrepo.New(db)
	if err != nil {
		return newRuntimeError("marking_repository_startup_failed", err)
	}
	logisticsRepository, err := logisticsrepo.New(db)
	if err != nil {
		return newRuntimeError("logistics_repository_startup_failed", err)
	}
	complianceRepository, err := compliancerepo.New(db)
	if err != nil {
		return newRuntimeError("compliance_repository_startup_failed", err)
	}
	notificationRepository, err := notificationrepo.New(db)
	if err != nil {
		return newRuntimeError("notification_repository_startup_failed", err)
	}
	destinationResolver := notificationDestinationResolver{repo: notificationRepository, secrets: secretProvider}
	notificationProviders := []notifications.Provider{notifications.WebUIProvider{}}
	if cfg.Notifications.SMTPAddress != "" && cfg.Notifications.SMTPFrom != "" {
		notificationProviders = append(notificationProviders, notifications.EmailProvider{Destinations: destinationResolver, Transport: notifications.SMTPTransport{Config: notifications.SMTPConfig{Address: cfg.Notifications.SMTPAddress, From: cfg.Notifications.SMTPFrom, Username: cfg.Notifications.SMTPUsername, Password: cfg.Notifications.SMTPPassword, ServerName: cfg.Notifications.SMTPServerName, Timeout: cfg.Notifications.Timeout, ImplicitTLS: cfg.Notifications.SMTPImplicitTLS}}})
	}
	if cfg.Notifications.ChatEndpoint != "" {
		notificationProviders = append(notificationProviders, notifications.ChatProvider{Destinations: destinationResolver, Transport: notifications.BotHTTPTransport{Endpoint: cfg.Notifications.ChatEndpoint}})
	}
	notificationService, err := notifications.NewService(notificationRepository, notificationProviders, nil)
	if err != nil {
		return newRuntimeError("notification_service_startup_failed", err)
	}
	syncRepository, err := syncrepo.New(db)
	if err != nil {
		return newRuntimeError("sync_repository_startup_failed", err)
	}
	reconciliationRepository, err := reconciliationrepo.New(db)
	if err != nil {
		return newRuntimeError("reconciliation_repository_startup_failed", err)
	}
	settlementRepository, err := settlementrepo.New(db)
	if err != nil {
		return newRuntimeError("settlement_repository_startup_failed", err)
	}
	socialRepository, err := socialrepo.New(db)
	if err != nil {
		return newRuntimeError("social_repository_startup_failed", err)
	}
	socialDispatchRepository, err := socialdispatchrepo.New(db)
	if err != nil {
		return newRuntimeError("social_dispatch_repository_startup_failed", err)
	}
	paymentsRepository, err := paymentsrepo.New(db)
	if err != nil {
		return newRuntimeError("payments_repository_startup_failed", err)
	}
	fxRepository, err := fxrepo.New(db)
	if err != nil {
		return newRuntimeError("fx_repository_startup_failed", err)
	}
	cloudSubscriptionRepository, err := cloudbillingrepo.New(db)
	if err != nil {
		return newRuntimeError("cloud_subscription_repository_startup_failed", err)
	}
	pluginRepository, err := pluginmarketplacerepo.New(db)
	if err != nil {
		return newRuntimeError("plugin_marketplace_repository_startup_failed", err)
	}
	retentionRepository, err := retentionrepo.New(db)
	if err != nil {
		return newRuntimeError("retention_repository_startup_failed", err)
	}
	privacyStore, err := retentionrepo.NewPrivacyStore(db, secretProvider)
	if err != nil {
		return newRuntimeError("privacy_store_startup_failed", err)
	}
	privacyService, err := retention.NewService(retentionRepository, []retention.Store{privacyStore}, auditService)
	if err != nil {
		return newRuntimeError("privacy_service_startup_failed", err)
	}
	uploadRepository, err := uploadrepo.New(db, cfg.Security.MaxUploadBytes)
	if err != nil {
		return newRuntimeError("upload_repository_startup_failed", err)
	}
	quarantineStore, err := uploads.NewS3QuarantineStore(uploads.S3Config{Endpoint: cfg.ObjectStorage.Endpoint, Bucket: cfg.ObjectStorage.Bucket, Region: cfg.ObjectStorage.Region, AccessKey: cfg.ObjectStorage.AccessKey, SecretKey: cfg.ObjectStorage.SecretKey, Timeout: cfg.ObjectStorage.Timeout, MaxBytes: cfg.Security.MaxUploadBytes})
	if err != nil {
		return newRuntimeError("upload_storage_startup_failed", err)
	}
	uploadPolicy := uploads.DefaultPolicy()
	uploadPolicy.MaxFileBytes = cfg.Security.MaxUploadBytes
	uploadService, err := uploads.NewService(uploadRepository, quarantineStore, uploadPolicy)
	if err != nil {
		return newRuntimeError("upload_service_startup_failed", err)
	}
	uploadAccessGate, err := uploads.NewAccessGate(uploadRepository, uploadPolicy)
	if err != nil {
		return newRuntimeError("upload_access_gate_startup_failed", err)
	}
	lineageRepository, err := lineagerepo.New(db)
	if err != nil {
		return newRuntimeError("lineage_repository_startup_failed", err)
	}
	legalPartyRepository, err := legalpartyrepo.New(db)
	if err != nil {
		return newRuntimeError("legal_party_repository_startup_failed", err)
	}
	entitlementRepository, err := entitlementrepo.New(db)
	if err != nil {
		return newRuntimeError("entitlement_repository_startup_failed", err)
	}
	entitlementService, err := entitlements.NewService(entitlementRepository)
	if err != nil {
		return newRuntimeError("entitlement_service_startup_failed", err)
	}
	quotaService, err := entitlements.NewQuotaService(entitlementRepository)
	if err != nil {
		return newRuntimeError("entitlement_quota_service_startup_failed", err)
	}
	webhookRepository, err := webhookrepo.New(db)
	if err != nil {
		return newRuntimeError("webhook_repository_startup_failed", err)
	}
	webhookService, err := webhooks.NewService(webhookRepository, secretProvider, webhooks.NewEndpointPolicy(nil), nil)
	if err != nil {
		return newRuntimeError("webhook_service_startup_failed", err)
	}
	if err := notificationService.RegisterProvider(notifications.WebhookProvider{Sink: webhookService}); err != nil {
		return newRuntimeError("notification_webhook_provider_startup_failed", err)
	}
	clickHousePort, err := reporting.NewClickHouseQueryPort(reporting.ClickHouseConfig{Endpoint: cfg.ClickHouse.Endpoint, Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password, Timeout: cfg.ClickHouse.QueryTimeout})
	if err != nil {
		return newRuntimeError("clickhouse_reporting_startup_failed", err)
	}
	reportQueries, err := reporting.NewQueryService(clickHousePort)
	if err != nil {
		return newRuntimeError("clickhouse_reporting_startup_failed", err)
	}
	reportRepository, err := newClickHouseReportReader(reportQueries)
	if err != nil {
		return newRuntimeError("clickhouse_reporting_startup_failed", err)
	}
	postgresReportRepository, err := reportrepo.New(db)
	if err != nil {
		return newRuntimeError("postgres_report_repository_startup_failed", err)
	}
	financialRepository, err := financialrepo.New(db)
	if err != nil {
		return newRuntimeError("financial_repository_startup_failed", err)
	}
	advertisingRepository, err := advertisingrepo.New(db)
	if err != nil {
		return newRuntimeError("advertising_repository_startup_failed", err)
	}
	reportRepository, err = newInventoryFallbackReportReader(reportRepository, postgresReportRepository)
	if err != nil {
		return newRuntimeError("report_reader_startup_failed", err)
	}
	authn, tenantResolver, authz, err := newOIDCSecurity(cfg, settingsSecurityRepository, tenantRepository)
	if err != nil {
		return newRuntimeError("oidc_security_startup_failed", err)
	}

	listener, err := net.Listen("tcp", cfg.HTTP.Address)
	if err != nil {
		return newRuntimeError("http_listen_failed", err)
	}
	edge := securityedge.Config{
		TrustedProxyCIDRs: cfg.Security.TrustedProxyCIDRs,
		AdminCIDRs:        cfg.Security.AdminCIDRs,
		AllowedOrigins:    cfg.Security.AllowedOrigins,
		MaxRequestBytes:   cfg.Security.MaxRequestBytes,
		MaxUploadBytes:    cfg.Security.MaxUploadBytes,
		RatePerMinute:     cfg.Security.RatePerMinute,
		HSTSSeconds:       cfg.Security.HSTSSeconds,
	}
	routeDeps := productionRouteDependencies{
		accounts: accountRepository, connectorConfigs: connectorConfigRepository, auditRepository: auditRepository, auditService: auditService, secretProvider: secretProvider, oauthRefresh: secretRepository, connectorCallbacks: connectorCallbacks,
		settingsSecurity: settingsSecurityRepository, settingsAudit: auditRepository, identityProviders: settingsSecurityRepository, identityPolicy: identityPolicy, identityValidator: identityValidator, oidc: cfg.OIDC,
		tenancy: tenantRepository, search: searchRepository, orders: orderRepository, catalog: catalogRepository, pricing: pricingRepository, publicationQuality: publicationQualityRepository, pim: pimRepository,
		images: imageRepository, inventory: inventoryRepository, marking: markingRepository, logistics: logisticsRepository, compliance: complianceRepository, notifications: notificationService,
		syncPolicies: syncRepository, reconciliations: reconciliationRepository, approvals: approvalRepository, reports: reportRepository, financialReports: financialRepository,
		lineage: lineageRepository, legalParties: legalPartyRepository, counterparties: legalPartyRepository, entitlements: entitlementService, quotas: quotaService, webhooks: webhookService,
		advertising: advertisingRepository, settlements: settlementRepository, social: socialRepository, socialReceipts: socialDispatchRepository, payments: paymentsRepository, privacy: privacyWorkflowAdapter{service: privacyService, repository: retentionRepository}, fxRates: fxRepository, cloudSubscription: cloudSubscriptionRepository, uploads: uploadService, plugins: pluginRepository, inboundWebhooks: inboundWebhookInbox,
		uploadStatus: uploadRepository, uploadAccess: uploadAccessGate, uploadEvidence: uploadRepository, uploadContent: quarantineStore, profiles: profileRepository,
		aiAdvisory: aiAdvisoryRepository, assistant: operatorAssistantRepository, aiRegistry: builtinruntime.New(), mcpAccounts: mcpAccountsRepository, agentGovernance: agentGovernanceRepository, runtimePosture: postureInspector, trustControl: trustControlRepository, workflows: workflowRepository, returns: returnsRepository, marketplacePublication: marketplacePublicationRepository, marketplaceFlows: marketplaceOperationsRepository, procurement: procurementRepository, replenishment: replenishmentRepository,
		integrationCenter: integrationCenterSource{accounts: accountRepository, configs: connectorConfigRepository, policies: syncRepository, reconciliation: reconciliationRepository, runtime: builtinruntime.New()},
	}
	routes := newProductionRoutes(routeDeps)
	handler, err := NewProductionHandler(logger, edge, securityedge.NewLimiter(), authn, tenantResolver, authz, routes, newProductionWebhookRoutes(routeDeps))
	if err != nil {
		_ = listener.Close()
		return newRuntimeError("http_security_composition_failed", err)
	}
	return serve(ctx, cfg, logger, listener, handler)
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, listener net.Listener, handler http.Handler) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
		ErrorLog:          log.New(serverLogWriter{logger: logger}, "", 0),
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	logger.Info("http server ready", "event", "http.server_ready", "address", listener.Addr().String())

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return newRuntimeError("http_serve_failed", err)
	case <-ctx.Done():
	}

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		closeErr := server.Close()
		if closeErr != nil {
			return newRuntimeError("http_force_close_failed", errors.Join(err, closeErr))
		}
		return newRuntimeError("http_shutdown_failed", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return newRuntimeError("http_shutdown_serve_failed", err)
	}
	return nil
}

type serverLogWriter struct {
	logger *slog.Logger
}

func (w serverLogWriter) Write(data []byte) (int, error) {
	if strings.TrimSpace(string(data)) != "" {
		// net/http diagnostics can include panic values, paths, or malformed
		// request content. Preserve the event without copying untrusted detail.
		w.logger.Error("http server diagnostic", "event", "http.server_diagnostic")
	}
	return len(data), nil
}

func route(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != HealthPath {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	health(w, request)
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		tracked := &responseTracker{ResponseWriter: w}
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(http.ErrAbortHandler)
			}
			if logger != nil {
				logger.Error("http handler panic", "event", "http.handler_panic")
			}
			if tracked.committed {
				// The response can no longer be replaced atomically. Ask net/http to
				// abort the connection without printing a panic value or stack.
				panic(http.ErrAbortHandler)
			}
			writeProblem(tracked, http.StatusInternalServerError, "Internal Server Error")
		}()
		next.ServeHTTP(tracked, request)
	})
}

type responseTracker struct {
	http.ResponseWriter
	committed bool
}

func (w *responseTracker) WriteHeader(status int) {
	if w.committed {
		return
	}
	w.committed = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseTracker) Write(data []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseTracker) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func health(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Service: string(config.ServiceAPI),
	})
}

func writeProblem(w http.ResponseWriter, status int, title string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problemResponse{
		Type:   "about:blank",
		Title:  title,
		Status: status,
	})
}
