package api

import (
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/advertisingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/agentgovernancerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/aiadvisoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/approvalrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogimagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/cloudbillingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/compliancerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/financialrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/fxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/logisticsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacepublicationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/markingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/mcpaccountsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/operatorassistantrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pimrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pluginmarketplacerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pricingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/procurementrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/returnsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/searchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/settlementrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/trustcontrolrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/userprofilerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workflowrepo"
	"github.com/torgnexa/torgnexa/internal/platform/runtimeposture"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

type productionRouteDependencies struct {
	advertising            *advertisingrepo.Repository
	accounts               *connectorrepo.Repository
	connectorConfigs       *connectorconfigrepo.Repository
	auditRepository        auditReader
	auditService           *audit.Service
	secretProvider         secrets.SecretProvider
	oauthRefresh           connectorauth.RefreshCoordinator
	connectorCallbacks     *connectorauth.CallbackPolicy
	tenancy                *tenancyrepo.Repository
	search                 *searchrepo.Repository
	orders                 orderStatusRepository
	catalog                *catalogrepo.Repository
	pricing                *pricingrepo.Repository
	publicationQuality     *publicationqualityrepo.Repository
	pim                    *pimrepo.Repository
	images                 *catalogimagerepo.Repository
	inventory              *inventoryrepo.Repository
	logistics              *logisticsrepo.Repository
	compliance             *compliancerepo.Repository
	notifications          *notifications.Service
	syncPolicies           *syncrepo.Repository
	reconciliations        *reconciliationrepo.Repository
	approvals              *approvalrepo.Repository
	reports                reportReader
	financialReports       *financialrepo.Repository
	lineage                lineage.Reader
	legalParties           LegalPartySearcher
	counterparties         counterpartyLister
	entitlements           *entitlements.Service
	quotas                 *entitlements.QuotaService
	webhooks               webhookService
	inboundWebhooks        *inboxrepo.Processor
	settingsSecurity       securitysettings.Store
	settingsAudit          securitysettings.SettingsAuditReader
	identityProviders      securitysettings.IdentityProviderStore
	identityPolicy         *securitysettings.ProviderURLPolicy
	identityValidator      securitysettings.ProviderValidator
	profiles               *userprofilerepo.Repository
	settlements            *settlementrepo.Repository
	social                 *socialrepo.Repository
	socialReceipts         *socialdispatchrepo.Repository
	payments               *paymentsrepo.Repository
	privacy                privacyWorkflow
	fxRates                *fxrepo.Repository
	cloudSubscription      *cloudbillingrepo.Repository
	uploads                *uploads.Service
	uploadStatus           uploadStatusReader
	uploadAccess           uploadReleaseGate
	uploadEvidence         uploadEvidenceReader
	uploadContent          uploads.ReleaseReader
	plugins                *pluginmarketplacerepo.Repository
	aiAdvisory             *aiadvisoryrepo.Repository
	assistant              *operatorassistantrepo.Repository
	aiRegistry             *builtinruntime.Registry
	integrationCenter      integrationCenterReader
	workflows              *workflowrepo.Repository
	returns                *returnsrepo.Repository
	marking                *markingrepo.Repository
	marketplacePublication *marketplacepublicationrepo.Repository
	procurement            *procurementrepo.Repository
	mcpAccounts            *mcpaccountsrepo.Repository
	agentGovernance        *agentgovernancerepo.Repository
	runtimePosture         *runtimeposture.Inspector
	trustControl           *trustcontrolrepo.Repository
	oidc                   config.OIDC
}

func newProductionRoutes(deps productionRouteDependencies) []ProtectedRoute {
	capabilityGuard := connectorAccountCapabilityGuard{repository: deps.accounts, runtime: deps.aiRegistry}
	routes := append(newConnectorAccountRoutes(deps.accounts, deps.connectorConfigs, deps.auditService, deps.secretProvider, deps.oauthRefresh, deps.connectorCallbacks, deps.aiRegistry, connectorManualSync{policies: deps.syncPolicies, runs: deps.reconciliations, guard: capabilityGuard, previews: deps.syncPolicies}), newWorkspaceSettingsRoutes(deps.tenancy, deps.auditService)...)
	routes = append(routes, newConnectorBootstrapRoutes(deps.accounts, deps.syncPolicies, capabilityGuard, deps.auditService)...)
	routes = append(routes, newIntegrationCenterRoutes(deps.integrationCenter)...)
	routes = append(routes, newMemberSettingsRoutes(deps.tenancy, deps.auditService, deps.profiles)...)
	routes = append(routes, newSettingsSecurityRoutes(deps.settingsSecurity, deps.settingsAudit, deps.oidc, deps.runtimePosture)...)
	routes = append(routes, newIdentityProviderSettingsRoutes(deps.identityProviders, deps.secretProvider, deps.auditService, deps.identityPolicy, deps.identityValidator)...)
	routes = append(routes, newAuditRoutes(deps.auditRepository)...)
	routes = append(routes, newRealtimeRoutes(deps.auditRepository)...)
	routes = append(routes, newApprovalRoutes(deps.approvals, deps.search)...)
	routes = append(routes, newSearchRoutes(deps.search, deps.auditService)...)
	routes = append(routes, newOrderStatusRoutes(deps.orders)...)
	routes = append(routes, newCatalogRoutes(catalogAPI{catalog: deps.catalog, prices: deps.pricing, pim: deps.pim, images: deps.images, uploadAccess: deps.uploadAccess})...)
	routes = append(routes, newPublicationQualityRoutes(deps.publicationQuality)...)
	routes = append(routes, newInventoryRoutes(deps.inventory)...)
	routes = append(routes, newWMSTaskRoutes(deps.inventory)...)
	routes = append(routes, newMarkingRoutes(deps.marking)...)
	routes = append(routes, newMarketplacePublicationRoutes(deps.marketplacePublication, deps.publicationQuality, deps.accounts, deps.approvals, deps.aiRegistry)...)
	routes = append(routes, newProcurementRoutes(deps.procurement, deps.approvals, deps.uploadAccess, deps.uploadContent)...)
	routes = append(routes, newComplianceRoutes(deps.compliance)...)
	routes = append(routes, newNotificationRoutes(deps.notifications)...)
	routes = append(routes, newSocialRoutes(deps.social, deps.accounts, deps.aiRegistry, socialRouteDependency{secrets: deps.secretProvider, configs: deps.connectorConfigs, receipts: deps.socialReceipts, approvals: deps.approvals, operations: deps.logistics, webhookControllerRuntime: deps.aiRegistry, audit: deps.auditService, uploadAccess: deps.uploadAccess, uploadContent: deps.uploadContent})...)
	logisticsRoutes := newLogisticsRoutes(deps.accounts, deps.secretProvider, deps.aiRegistry, logisticsRouteDependency{shipments: deps.logistics, approvals: deps.approvals, operations: deps.logistics})
	routes = append(routes, logisticsRoutes...)
	routes = append(routes, newPaymentsRoutes(deps.payments, deps.accounts, deps.connectorConfigs, deps.secretProvider, deps.aiRegistry)...)
	routes = append(routes, newReturnsRoutes(deps.returns)...)
	routes = append(routes, newFinancialReportRoutes(deps.financialReports, deps.auditService)...)
	routes = append(routes, newAdvertisingRoutes(deps.advertising)...)
	routes = append(routes, newReportRoutes(deps.reports)...)
	routes = append(routes, newSyncRoutes(deps.syncPolicies, deps.reconciliations, capabilityGuard)...)
	routes = append(routes, newLineageRoutes(deps.lineage)...)
	routes = append(routes, newLegalPartyRoutes(deps.legalParties)...)
	routes = append(routes, newEntitlementRoutes(deps.entitlements, deps.quotas)...)
	routes = append(routes, newWebhookRoutes(deps.webhooks)...)
	routes = append(routes, newAIAdvisoryRoutes(deps.aiAdvisory, deps.secretProvider, deps.aiRegistry, deps.auditService, deps.trustControl)...)
	routes = append(routes, newOperatorAssistantRoutesWithApproval(deps.assistant, operatorAssistantSources{integration: deps.integrationCenter, quality: deps.publicationQuality, inventory: deps.inventory, returns: deps.returns, sync: deps.syncPolicies, reconciliation: deps.reconciliations, reports: deps.reports}, deps.approvals, deps.auditService)...)
	routes = append(routes, newMCPAccountRoutes(deps.mcpAccounts, deps.auditService)...)
	routes = append(routes, newMCPAgentPolicyRoutes(deps.mcpAccounts, deps.agentGovernance, deps.agentGovernance, deps.auditService)...)
	routes = append(routes, newTrustControlRoutes(deps.trustControl)...)
	routes = append(routes, newWorkflowRoutes(deps.workflows)...)
	routes = append(routes, newUploadReadRoutes(deps.uploadStatus, deps.uploadAccess, deps.uploadContent)...)
	routes = append(routes, newUserProfileRoutes(profileAPI{profiles: deps.profiles, audit: deps.auditService, uploads: deps.uploads, uploadStatus: deps.uploadStatus, uploadAccess: deps.uploadAccess, uploadEvidence: deps.uploadEvidence, privacy: deps.privacy})...)
	routes = append(routes, newReservedContractRoutes(deps, capabilityGuard)...)
	return routes
}

// newProductionWebhookRoutes registers the unauthenticated PublicWebhookRoute
// table (ADR-0105/Task 136). Kept separate from newProductionRoutes because
// the two route types have different security postures and neither may be
// registered through the other's table.
func newProductionWebhookRoutes(deps productionRouteDependencies) []PublicWebhookRoute {
	var routes []PublicWebhookRoute
	routes = append(routes, newLogisticsWebhookRoutes(deps.logistics, deps.accounts, deps.secretProvider, deps.aiRegistry)...)
	routes = append(routes, newPaymentWebhookRoutes(deps.payments, deps.accounts, deps.connectorConfigs, deps.secretProvider, deps.aiRegistry)...)
	routes = append(routes, newCommerceWebhookRoutes(deps.accounts, deps.connectorConfigs, deps.secretProvider, deps.aiRegistry, deps.inboundWebhooks)...)
	routes = append(routes, newSocialWebhookRoutes(deps.accounts, deps.connectorConfigs, deps.secretProvider, deps.aiRegistry, deps.inboundWebhooks)...)
	return routes
}
