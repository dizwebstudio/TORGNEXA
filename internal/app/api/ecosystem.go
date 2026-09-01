package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/customerservice"
	"github.com/torgnexa/torgnexa/internal/core/ecosystem"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	connectorSDK "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/cloudbillingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/ecosystemrepo"
)

const (
	EcosystemOverviewPath   = "/api/v1/ecosystem/overview"
	EcosystemMetricsPath    = "/api/v1/ecosystem/metrics"
	EcosystemOnboardingPath = "/api/v1/ecosystem/onboarding"
	EcosystemPartnersPath   = "/api/v1/ecosystem/partners/certifications"
)

type ecosystemCustomerServiceReader interface {
	Summary(context.Context, tenancy.Scope) (customerservice.Summary, error)
}

type ecosystemAPI struct {
	repository      *ecosystemrepo.Repository
	plugins         pluginListingReader
	cloud           cloudSubscriptionReader
	customerService ecosystemCustomerServiceReader
	audit           auditCapturer
}

func newEcosystemRoutes(repository *ecosystemrepo.Repository, plugins pluginListingReader, cloud cloudSubscriptionReader, customerService ecosystemCustomerServiceReader, auditor auditCapturer) []ProtectedRoute {
	api := ecosystemAPI{repository: repository, plugins: plugins, cloud: cloud, customerService: customerService, audit: auditor}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: EcosystemOverviewPath, Permission: "ecosystem.read", Handler: http.HandlerFunc(api.overview)},
		{Method: http.MethodGet, Path: EcosystemMetricsPath, Permission: "ecosystem.read", Handler: http.HandlerFunc(api.metrics)},
		{Method: http.MethodGet, Path: EcosystemOnboardingPath, Permission: "ecosystem.read", Handler: http.HandlerFunc(api.onboarding)},
		{Method: http.MethodPost, Path: EcosystemOnboardingPath, Permission: "ecosystem.onboarding.write", Handler: http.HandlerFunc(api.createOnboarding)},
		{Method: http.MethodGet, Path: EcosystemPartnersPath, Permission: "ecosystem.read", Handler: http.HandlerFunc(api.partners)},
		{Method: http.MethodPost, Path: EcosystemPartnersPath, Permission: "ecosystem.partners.write", Handler: http.HandlerFunc(api.createPartnerCertification)},
	}
}

func (api ecosystemAPI) overview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	result, err := api.buildOverview(r.Context(), scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Ecosystem overview unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api ecosystemAPI) metrics(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	result, err := api.buildOverview(r.Context(), scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Ecosystem metrics unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"generated_at": result.GeneratedAt, "consistency": result.Consistency, "metrics": result.Metrics})
}

func (api ecosystemAPI) onboarding(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Ecosystem onboarding unavailable")
		return
	}
	items, err := api.repository.ListOnboarding(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Ecosystem onboarding unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api ecosystemAPI) partners(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Partner certifications unavailable")
		return
	}
	items, err := api.repository.ListPartnerCertifications(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Partner certifications unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api ecosystemAPI) createOnboarding(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var input struct {
		ResourceID string                      `json:"resource_id"`
		Checks     []ecosystem.OnboardingCheck `json:"checks"`
	}
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid onboarding checks")
		return
	}
	now := time.Now().UTC()
	for i := range input.Checks {
		if input.Checks[i].State == "" {
			input.Checks[i].State = ecosystem.CheckPending
		}
		if input.Checks[i].UpdatedAt.IsZero() {
			input.Checks[i].UpdatedAt = now
		}
	}
	run := ecosystem.OnboardingRun{ID: stableID("ecosystem_onboarding_", 40, scope, key), ResourceID: input.ResourceID, State: ecosystem.OnboardingDraft, Checks: input.Checks, OwnerRef: boundedActorRef(principal.Subject), IdempotencyKey: key, Version: 1, CreatedAt: now, UpdatedAt: now}
	evaluated, err := ecosystem.EvaluateOnboarding(run)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid onboarding checks")
		return
	}
	if api.audit == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit unavailable")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, auditEntryForEcosystem(principal, key, "ecosystem.onboarding.create", "onboarding", evaluated.ID, map[string]any{"resource_id": evaluated.ResourceID, "state": evaluated.State})); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit unavailable")
		return
	}
	if err := api.repository.SaveOnboarding(r.Context(), scope, evaluated); err != nil {
		if errors.Is(err, ecosystemrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Onboarding idempotency conflict")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Onboarding could not be saved")
		return
	}
	writeJSON(w, http.StatusAccepted, evaluated)
}

func (api ecosystemAPI) createPartnerCertification(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var input ecosystem.PartnerCertification
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid partner certification")
		return
	}
	now := time.Now().UTC()
	if input.ID == "" {
		input.ID = stableID("ecosystem_partner_", 40, scope, key)
	}
	input.IdempotencyKey = key
	input.Version = 1
	input.UpdatedAt = now
	if err := input.Validate(now); err != nil {
		writeProblem(w, http.StatusBadRequest, "Partner certification requires valid retained evidence")
		return
	}
	if api.audit == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit unavailable")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, auditEntryForEcosystem(principal, key, "ecosystem.partner_certification.create", "partner_certification", input.ID, map[string]any{"partner_ref": input.PartnerRef, "tier": input.Tier, "state": input.State})); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit unavailable")
		return
	}
	if err := api.repository.SavePartnerCertification(r.Context(), scope, input); err != nil {
		if errors.Is(err, ecosystemrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Partner certification idempotency conflict")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Partner certification could not be saved")
		return
	}
	writeJSON(w, http.StatusAccepted, input)
}

func (api ecosystemAPI) buildOverview(ctx context.Context, scope tenancy.Scope) (ecosystem.Overview, error) {
	now := time.Now().UTC()
	readiness, err := connectorSDK.ReadinessSnapshot()
	if err != nil {
		return ecosystem.Overview{}, err
	}
	portfolio := make([]ecosystem.PortfolioItem, 0, len(readiness.Profiles)+8)
	for _, profile := range readiness.Profiles {
		status := ecosystem.StatusIntegrated
		var evidence *ecosystem.Evidence
		if profile.Status == connectorSDK.ReadinessReady {
			status = ecosystem.StatusReady
			evidence = ecosystemEvidence("repository", "connector-readiness/"+profile.ID, profile, now)
		}
		if profile.Status == connectorSDK.ReadinessQualified && (profile.LiveQualificationStatus == "passed" || profile.LiveQualificationStatus == "qualified") {
			status = ecosystem.StatusQualified
			evidence = ecosystemEvidence("credentialed_live", "connector-readiness/"+profile.ID, profile, now)
		}
		portfolio = append(portfolio, ecosystem.PortfolioItem{ID: "connector:" + profile.ID, Kind: ecosystem.KindConnector, Tier: profile.Family, DisplayName: profile.DisplayName, Status: status, Owner: profile.Owner, Priority: profile.Priority, Decision: profile.Decision, NextAction: profile.NextAction, SupportLevel: "not_claimed", Deployment: "community_self_hosted", Capabilities: readinessCapabilityNames(profile), Evidence: evidence, Version: 1, UpdatedAt: now})
	}
	portfolio = append(portfolio, ecosystemStaticPortfolio(now)...)
	mobile, hosted, support, err := ecosystem.DefaultSurfaces(now)
	if err != nil {
		return ecosystem.Overview{}, err
	}
	visibleApps := 0
	if api.plugins != nil {
		listings, listErr := api.plugins.ListVisible(ctx, scope, 200)
		if listErr != nil {
			return ecosystem.Overview{}, listErr
		}
		visibleApps = len(listings)
	}
	if api.customerService != nil {
		queue, queueErr := api.customerService.Summary(ctx, scope)
		if queueErr != nil {
			return ecosystem.Overview{}, queueErr
		}
		support.OpenCases = int64(queue.Open + queue.Pending)
		support.AtRiskCases = int64(queue.Breached)
	}
	metrics := []ecosystem.OutcomeMetric{{Name: "connectors.catalog", Value: int64(len(readiness.Profiles)), Unit: "items", State: "observed", AsOf: now}, {Name: "connectors.ready_capabilities", Value: int64(readyCapabilityCount(readiness)), Unit: "capabilities", State: "observed", AsOf: now}, {Name: "apps.visible", Value: int64(visibleApps), Unit: "apps", State: "observed", AsOf: now}, {Name: "support.open_cases", Value: support.OpenCases, Unit: "cases", State: "observed", AsOf: now}, {Name: "support.at_risk_cases", Value: support.AtRiskCases, Unit: "cases", State: "observed", AsOf: now}}
	if api.cloud != nil {
		subscription, subErr := api.cloud.CurrentSubscription(ctx, scope)
		if subErr != nil && !errors.Is(subErr, cloudbillingrepo.ErrNotFound) {
			return ecosystem.Overview{}, subErr
		}
		active := int64(0)
		state := "not_configured"
		if subErr == nil {
			state = string(subscription.State)
			if subscription.State == "active" || subscription.State == "trial" {
				active = 1
			}
		}
		metrics = append(metrics, ecosystem.OutcomeMetric{Name: "cloud.subscription.active", Value: active, Unit: "boolean", State: state, AsOf: now})
	}
	onboarding := []ecosystem.OnboardingRun{}
	partners := []ecosystem.PartnerCertification{}
	if api.repository != nil {
		onboarding, err = api.repository.ListOnboarding(ctx, scope, 100)
		if err != nil {
			return ecosystem.Overview{}, err
		}
		partners, err = api.repository.ListPartnerCertifications(ctx, scope, 100)
		if err != nil {
			return ecosystem.Overview{}, err
		}
	}
	metrics = append(metrics, ecosystem.OutcomeMetric{Name: "onboarding.runs", Value: int64(len(onboarding)), Unit: "runs", State: "observed", AsOf: now}, ecosystem.OutcomeMetric{Name: "partners.certified", Value: int64(countCertifiedPartners(partners)), Unit: "partners", State: "observed", AsOf: now})
	return ecosystem.BuildOverview(ecosystem.OverviewInput{Now: now, Portfolio: portfolio, VisibleApps: visibleApps, Onboarding: onboarding, Partners: partners, Mobile: mobile, HostedTiers: hosted, Support: support, Metrics: metrics, ExternalGates: []string{"credentialed_connector_qualification", "partner_uat_and_rollback", "hosted_topology_slo_drill", "mobile_device_matrix", "production_backup_restore"}})
}

func ecosystemStaticPortfolio(now time.Time) []ecosystem.PortfolioItem {
	items := []struct {
		id, kind, tier, name, owner, priority, decision, next, support, deployment string
		status                                                                     ecosystem.Status
	}{
		{"app-marketplace", string(ecosystem.KindApp), "distribution", "Marketplace приложений", "platform-governance", "wave_1", "maintain", "publisher_review", "repository", "community_self_hosted", ecosystem.StatusVerified},
		{"developer-platform", string(ecosystem.KindApp), "developer", "Developer API и SDK", "developer-platform", "wave_1", "maintain", "partner_sandbox", "repository", "community_self_hosted", ecosystem.StatusVerified},
		{"partner-delivery", string(ecosystem.KindPartner), "services", "Партнёрское внедрение", "ecosystem-operations", "wave_2", "build", "certification_uat", "qualification_required", "partner_managed", ecosystem.StatusIntegrated},
		{"mobile-delivery", string(ecosystem.KindMobileSurface), "mobile", "Мобильная работа", "warehouse-product", "wave_1", "maintain", "device_matrix", "repository", "community_self_hosted", ecosystem.StatusVerified},
		{"cloud-offering", string(ecosystem.KindCloudTier), "cloud", "Hosted operations", "sre", "wave_3", "qualify", "target_topology", "qualification_required", "hosted", ecosystem.StatusIntegrated},
		{"support-desk", string(ecosystem.KindPartner), "support", "Support desk и onboarding", "customer-operations", "wave_1", "maintain", "support_evidence", "repository", "community_self_hosted", ecosystem.StatusReady},
		{"billing-packaging", string(ecosystem.KindCloudTier), "commercial", "Cloud billing и packaging", "commercial-operations", "wave_2", "maintain", "commercial_launch_review", "repository", "hosted", ecosystem.StatusReady},
	}
	out := make([]ecosystem.PortfolioItem, 0, len(items))
	for _, item := range items {
		payload := item.id + ":task-231"
		out = append(out, ecosystem.PortfolioItem{ID: item.id, Kind: ecosystem.ResourceKind(item.kind), Tier: item.tier, DisplayName: item.name, Status: item.status, Owner: item.owner, Priority: item.priority, Decision: item.decision, NextAction: item.next, SupportLevel: item.support, Deployment: item.deployment, UseCases: []string{"onboarding", "operations", "support"}, Capabilities: []string{"readiness.evidence", "tenant.scoped.operations"}, Evidence: ecosystemEvidence("repository", "task-231/"+item.id, payload, now), Version: 1, UpdatedAt: now})
	}
	return out
}

func ecosystemEvidence(kind, source string, value any, now time.Time) *ecosystem.Evidence {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return &ecosystem.Evidence{Kind: kind, SourceRef: source, Digest: hex.EncodeToString(sum[:]), CheckedAt: now, Environment: "repository"}
}

func readinessCapabilityNames(profile connectorSDK.ReadinessProfile) []string {
	names := make([]string, 0, len(profile.Capabilities))
	for _, capability := range profile.Capabilities {
		names = append(names, capability.Name)
	}
	return names
}
func readyCapabilityCount(matrix connectorSDK.ReadinessMatrix) int {
	count := 0
	for _, profile := range matrix.Profiles {
		if profile.Status == connectorSDK.ReadinessReady || profile.Status == connectorSDK.ReadinessQualified {
			count += len(profile.Capabilities)
		}
	}
	return count
}
func countCertifiedPartners(items []ecosystem.PartnerCertification) int {
	count := 0
	for _, item := range items {
		if item.State == ecosystem.PartnerCertified {
			count++
		}
	}
	return count
}
func auditEntryForEcosystem(principal Principal, key, action, resourceType, resourceID string, summary map[string]any) audit.Entry {
	return audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.ecosystem", Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: key, Risk: audit.RiskWriteSafe, Summary: summary}
}
