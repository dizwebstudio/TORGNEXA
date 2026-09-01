// Package ecosystem contains the provider-neutral operating model for the
// integration, partner and support surfaces. It is a projection over existing
// connector, plugin, billing, customer-service, mobile and SLO bounded
// contexts; it is not a second owner for any of those ledgers.
package ecosystem

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalid   = errors.New("ecosystem: invalid value")
	ErrConflict  = errors.New("ecosystem: conflict")
	ErrNotFound  = errors.New("ecosystem: not found")
	ErrPromotion = errors.New("ecosystem: promotion requires evidence")
)

// Status is deliberately independent from connector implementation status.
// A count of manifests can never promote a resource to a higher level.
type Status string

const (
	StatusIntegrated Status = "integrated"
	StatusVerified   Status = "verified"
	StatusReady      Status = "ready"
	StatusQualified  Status = "qualified"
	StatusSupported  Status = "supported"
	StatusDeprecated Status = "deprecated"
	StatusBlocked    Status = "blocked"
)

func (s Status) Valid() bool {
	switch s {
	case StatusIntegrated, StatusVerified, StatusReady, StatusQualified, StatusSupported, StatusDeprecated, StatusBlocked:
		return true
	default:
		return false
	}
}

func (s Status) Rank() int {
	switch s {
	case StatusIntegrated:
		return 1
	case StatusVerified:
		return 2
	case StatusReady:
		return 3
	case StatusQualified:
		return 4
	case StatusSupported:
		return 5
	default:
		return 0
	}
}

// ResourceKind identifies a catalog entry without naming a provider.
type ResourceKind string

const (
	KindConnector     ResourceKind = "connector"
	KindCapability    ResourceKind = "capability"
	KindApp           ResourceKind = "app"
	KindPartner       ResourceKind = "partner_service"
	KindMobileSurface ResourceKind = "mobile_surface"
	KindCloudTier     ResourceKind = "cloud_tier"
)

func (k ResourceKind) Valid() bool {
	switch k {
	case KindConnector, KindCapability, KindApp, KindPartner, KindMobileSurface, KindCloudTier:
		return true
	default:
		return false
	}
}

// Evidence is a redacted pointer to retained evidence. It must not contain a
// token, a remote payload or a production customer identifier.
type Evidence struct {
	Kind        string    `json:"kind"`
	SourceRef   string    `json:"source_ref"`
	Digest      string    `json:"digest"`
	CheckedAt   time.Time `json:"checked_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Environment string    `json:"environment"`
}

func (e Evidence) Validate(now time.Time) error {
	if strings.TrimSpace(e.Kind) == "" || !safeRef(e.SourceRef, 192) || !safeRef(e.Environment, 32) || len(e.Digest) != 64 || !isHex(e.Digest) || !utc(e.CheckedAt) || !utc(now) {
		return ErrInvalid
	}
	if !e.ExpiresAt.IsZero() && (!utc(e.ExpiresAt) || e.ExpiresAt.Before(e.CheckedAt)) {
		return ErrInvalid
	}
	return nil
}

// PortfolioItem is the common customer-facing catalog row for integrations,
// apps and delivery services.
type PortfolioItem struct {
	ID           string       `json:"id"`
	Kind         ResourceKind `json:"kind"`
	Tier         string       `json:"tier"`
	DisplayName  string       `json:"display_name"`
	Status       Status       `json:"status"`
	Owner        string       `json:"owner"`
	Priority     string       `json:"priority"`
	Decision     string       `json:"decision"`
	NextAction   string       `json:"next_action"`
	SupportLevel string       `json:"support_level"`
	Deployment   string       `json:"deployment"`
	UseCases     []string     `json:"use_cases"`
	Capabilities []string     `json:"capabilities"`
	Evidence     *Evidence    `json:"evidence,omitempty"`
	Version      int64        `json:"version"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

func (p PortfolioItem) Validate(now time.Time) error {
	if !safeRef(p.ID, 192) || !p.Kind.Valid() || !safeRef(p.Tier, 64) || strings.TrimSpace(p.DisplayName) == "" || !p.Status.Valid() || !safeRef(p.Owner, 192) || !safeRef(p.Priority, 32) || !safeRef(p.Decision, 128) || !safeRef(p.NextAction, 256) || !safeRef(p.SupportLevel, 64) || !safeRef(p.Deployment, 64) || p.Version < 1 || !utc(p.UpdatedAt) || len(p.UseCases) > 32 || len(p.Capabilities) > 128 {
		return ErrInvalid
	}
	for _, value := range append(append([]string{}, p.UseCases...), p.Capabilities...) {
		if !safeRef(value, 192) {
			return ErrInvalid
		}
	}
	if p.Evidence != nil {
		if err := p.Evidence.Validate(now); err != nil {
			return err
		}
	}
	if (p.Status == StatusVerified || p.Status == StatusReady || p.Status == StatusQualified || p.Status == StatusSupported) && p.Evidence == nil {
		return ErrPromotion
	}
	if p.Status == StatusQualified && p.Evidence != nil && p.Evidence.Kind != "credentialed_sandbox" && p.Evidence.Kind != "credentialed_live" {
		return ErrPromotion
	}
	if p.Status == StatusSupported && p.SupportLevel == "" {
		return ErrPromotion
	}
	return nil
}

// Promote applies the status gate without asserting that an external check
// happened. Credentialed qualification and support evidence must be explicit.
func Promote(item PortfolioItem, target Status, evidence Evidence, now time.Time) (PortfolioItem, error) {
	if !item.Status.Valid() || !target.Valid() || item.Status == StatusDeprecated || item.Status == StatusBlocked || target == StatusIntegrated || target == StatusDeprecated || target == StatusBlocked {
		return PortfolioItem{}, ErrInvalid
	}
	if target.Rank() < item.Status.Rank() || target.Rank() > item.Status.Rank()+1 {
		return PortfolioItem{}, ErrPromotion
	}
	if err := evidence.Validate(now); err != nil {
		return PortfolioItem{}, err
	}
	if target == StatusQualified && evidence.Kind != "credentialed_sandbox" && evidence.Kind != "credentialed_live" {
		return PortfolioItem{}, ErrPromotion
	}
	if target == StatusSupported && evidence.Kind != "support" {
		return PortfolioItem{}, ErrPromotion
	}
	item.Status = target
	item.Evidence = &evidence
	item.Version++
	item.UpdatedAt = now.UTC()
	return item, item.Validate(now)
}

// CheckState is the outcome of one repeatable onboarding check.
type CheckState string

const (
	CheckPending CheckState = "pending"
	CheckPassed  CheckState = "passed"
	CheckFailed  CheckState = "failed"
	CheckSkipped CheckState = "skipped"
)

func (s CheckState) Valid() bool {
	return s == CheckPending || s == CheckPassed || s == CheckFailed || s == CheckSkipped
}

type OnboardingCheck struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Required    bool       `json:"required"`
	State       CheckState `json:"state"`
	EvidenceRef string     `json:"evidence_ref,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (c OnboardingCheck) Validate() error {
	if !safeRef(c.ID, 96) || strings.TrimSpace(c.Label) == "" || !c.State.Valid() || !utc(c.UpdatedAt) || len(c.EvidenceRef) > 192 || strings.ContainsAny(c.EvidenceRef, "\r\n\x00") {
		return ErrInvalid
	}
	if c.Required && c.State == CheckSkipped {
		return ErrInvalid
	}
	return nil
}

type OnboardingState string

const (
	OnboardingDraft      OnboardingState = "draft"
	OnboardingRunning    OnboardingState = "running"
	OnboardingReady      OnboardingState = "ready"
	OnboardingBlocked    OnboardingState = "blocked"
	OnboardingRolledBack OnboardingState = "rolled_back"
)

func (s OnboardingState) Valid() bool {
	return s == OnboardingDraft || s == OnboardingRunning || s == OnboardingReady || s == OnboardingBlocked || s == OnboardingRolledBack
}

type OnboardingRun struct {
	ID             string            `json:"id"`
	ResourceID     string            `json:"resource_id"`
	State          OnboardingState   `json:"state"`
	Checks         []OnboardingCheck `json:"checks"`
	OwnerRef       string            `json:"owner_ref"`
	IdempotencyKey string            `json:"idempotency_key"`
	Version        int64             `json:"version"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func (r OnboardingRun) Validate() error {
	if !safeRef(r.ID, 192) || !safeRef(r.ResourceID, 192) || !r.State.Valid() || !safeRef(r.OwnerRef, 192) || !safeRef(r.IdempotencyKey, 128) || r.Version < 1 || !utc(r.CreatedAt) || !utc(r.UpdatedAt) || r.UpdatedAt.Before(r.CreatedAt) || len(r.Checks) == 0 || len(r.Checks) > 64 {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(r.Checks))
	for _, check := range r.Checks {
		if err := check.Validate(); err != nil {
			return err
		}
		if _, ok := seen[check.ID]; ok {
			return ErrInvalid
		}
		seen[check.ID] = struct{}{}
	}
	return nil
}

// EvaluateOnboarding derives the state from checks; it never promotes a
// connector to qualified or supported.
func EvaluateOnboarding(run OnboardingRun) (OnboardingRun, error) {
	if err := run.Validate(); err != nil {
		return OnboardingRun{}, err
	}
	failed, pending := false, false
	for _, check := range run.Checks {
		if !check.Required {
			continue
		}
		switch check.State {
		case CheckFailed:
			failed = true
		case CheckPending:
			pending = true
		}
	}
	switch {
	case failed:
		run.State = OnboardingBlocked
	case pending:
		run.State = OnboardingRunning
	default:
		run.State = OnboardingReady
	}
	return run, nil
}

type PartnerTier string

const (
	PartnerReferral          PartnerTier = "referral"
	PartnerImplementation    PartnerTier = "implementation"
	PartnerCertifiedSolution PartnerTier = "certified_solution"
	PartnerManagedOperations PartnerTier = "managed_operations"
	PartnerSupportEscalation PartnerTier = "support_escalation"
)

func (t PartnerTier) Valid() bool {
	return t == PartnerReferral || t == PartnerImplementation || t == PartnerCertifiedSolution || t == PartnerManagedOperations || t == PartnerSupportEscalation
}

type PartnerState string

const (
	PartnerApplied   PartnerState = "applied"
	PartnerSandbox   PartnerState = "sandbox_ready"
	PartnerCertified PartnerState = "certified"
	PartnerSuspended PartnerState = "suspended"
	PartnerRevoked   PartnerState = "revoked"
	PartnerExpired   PartnerState = "expired"
)

func (s PartnerState) Valid() bool {
	return s == PartnerApplied || s == PartnerSandbox || s == PartnerCertified || s == PartnerSuspended || s == PartnerRevoked || s == PartnerExpired
}

type PartnerCertification struct {
	ID             string       `json:"id"`
	PartnerRef     string       `json:"partner_ref"`
	Tier           PartnerTier  `json:"tier"`
	State          PartnerState `json:"state"`
	Evidence       *Evidence    `json:"evidence,omitempty"`
	ExpiresAt      time.Time    `json:"expires_at,omitempty"`
	IdempotencyKey string       `json:"idempotency_key"`
	Version        int64        `json:"version"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

func (p PartnerCertification) Validate(now time.Time) error {
	if !safeRef(p.ID, 192) || !safeRef(p.PartnerRef, 192) || !p.Tier.Valid() || !p.State.Valid() || !safeRef(p.IdempotencyKey, 128) || p.Version < 1 || !utc(p.UpdatedAt) || p.ExpiresAt.IsZero() || !utc(p.ExpiresAt) || p.ExpiresAt.Before(p.UpdatedAt) {
		return ErrInvalid
	}
	if p.State == PartnerCertified && p.Evidence == nil {
		return ErrPromotion
	}
	if p.Evidence != nil {
		if err := p.Evidence.Validate(now); err != nil {
			return err
		}
		if p.State == PartnerCertified && p.Evidence.Kind != "credentialed_sandbox" && p.Evidence.Kind != "credentialed_live" {
			return ErrPromotion
		}
	}
	return nil
}

type MobileSurface struct {
	ID             string    `json:"id"`
	Target         string    `json:"target"`
	SupportedOS    []string  `json:"supported_os"`
	OfflinePolicy  string    `json:"offline_policy"`
	Capabilities   []string  `json:"capabilities"`
	ReleaseChannel string    `json:"release_channel"`
	Status         Status    `json:"status"`
	Evidence       *Evidence `json:"evidence,omitempty"`
}

func (m MobileSurface) Validate(now time.Time) error {
	if !safeRef(m.ID, 192) || !safeRef(m.Target, 64) || !safeRef(m.OfflinePolicy, 128) || !safeRef(m.ReleaseChannel, 32) || !m.Status.Valid() || len(m.SupportedOS) == 0 || len(m.Capabilities) == 0 || len(m.SupportedOS) > 16 || len(m.Capabilities) > 32 {
		return ErrInvalid
	}
	for _, value := range append(append([]string{}, m.SupportedOS...), m.Capabilities...) {
		if !safeRef(value, 128) {
			return ErrInvalid
		}
	}
	if m.Evidence != nil {
		return m.Evidence.Validate(now)
	}
	return nil
}

type HostedTier struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Deployment           string    `json:"deployment"`
	DataResidency        string    `json:"data_residency"`
	AvailabilityBPS      int64     `json:"availability_bps"`
	APIResponseP95MS     int64     `json:"api_response_p95_ms"`
	RecoveryPointMinutes int64     `json:"recovery_point_minutes"`
	RecoveryTimeMinutes  int64     `json:"recovery_time_minutes"`
	SupportLevel         string    `json:"support_level"`
	SLAClaimable         bool      `json:"sla_claimable"`
	Evidence             *Evidence `json:"evidence,omitempty"`
}

func (t HostedTier) Validate(now time.Time) error {
	if !safeRef(t.ID, 96) || strings.TrimSpace(t.Name) == "" || !safeRef(t.Deployment, 64) || !safeRef(t.DataResidency, 64) || t.AvailabilityBPS < 0 || t.AvailabilityBPS > 10000 || t.APIResponseP95MS < 0 || t.RecoveryPointMinutes < 0 || t.RecoveryTimeMinutes < 0 || !safeRef(t.SupportLevel, 64) {
		return ErrInvalid
	}
	if t.SLAClaimable && t.Evidence == nil {
		return ErrPromotion
	}
	if t.Evidence != nil {
		return t.Evidence.Validate(now)
	}
	return nil
}

type SupportPolicy struct {
	Tier                    string `json:"tier"`
	Coverage                string `json:"coverage"`
	FirstResponseMinutes    int64  `json:"first_response_minutes"`
	ResolutionTargetMinutes int64  `json:"resolution_target_minutes"`
	Owner                   string `json:"owner"`
}

func (p SupportPolicy) Validate() error {
	if !safeRef(p.Tier, 64) || !safeRef(p.Coverage, 128) || p.FirstResponseMinutes <= 0 || p.ResolutionTargetMinutes < p.FirstResponseMinutes || !safeRef(p.Owner, 192) {
		return ErrInvalid
	}
	return nil
}

type SupportSnapshot struct {
	OpenCases   int64           `json:"open_cases"`
	AtRiskCases int64           `json:"at_risk_cases"`
	Policies    []SupportPolicy `json:"policies"`
	Diagnostics string          `json:"diagnostics"`
}

func (s SupportSnapshot) Validate() error {
	if s.OpenCases < 0 || s.AtRiskCases < 0 || s.AtRiskCases > s.OpenCases || len(s.Policies) == 0 || len(s.Policies) > 16 || !safeRef(s.Diagnostics, 256) {
		return ErrInvalid
	}
	for _, policy := range s.Policies {
		if err := policy.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type OutcomeMetric struct {
	Name     string    `json:"name"`
	Value    int64     `json:"value"`
	Unit     string    `json:"unit"`
	State    string    `json:"state"`
	AsOf     time.Time `json:"as_of"`
	Evidence *Evidence `json:"evidence,omitempty"`
}

func (m OutcomeMetric) Validate(now time.Time) error {
	if !safeRef(m.Name, 128) || m.Value < 0 || !safeRef(m.Unit, 32) || !safeRef(m.State, 32) || !utc(m.AsOf) {
		return ErrInvalid
	}
	if m.Evidence != nil {
		return m.Evidence.Validate(now)
	}
	return nil
}

type StatusCounts struct {
	Integrated int `json:"integrated"`
	Verified   int `json:"verified"`
	Ready      int `json:"ready"`
	Qualified  int `json:"qualified"`
	Supported  int `json:"supported"`
	Deprecated int `json:"deprecated"`
	Blocked    int `json:"blocked"`
}

type Overview struct {
	SchemaVersion int                    `json:"schema_version"`
	GeneratedAt   time.Time              `json:"generated_at"`
	Consistency   string                 `json:"consistency"`
	StatusCounts  StatusCounts           `json:"status_counts"`
	Portfolio     []PortfolioItem        `json:"portfolio"`
	VisibleApps   int                    `json:"visible_apps"`
	Onboarding    []OnboardingRun        `json:"onboarding"`
	Partners      []PartnerCertification `json:"partners"`
	Mobile        []MobileSurface        `json:"mobile"`
	HostedTiers   []HostedTier           `json:"hosted_tiers"`
	Support       SupportSnapshot        `json:"support"`
	Metrics       []OutcomeMetric        `json:"metrics"`
	ExternalGates []string               `json:"external_gates"`
}

// OverviewInput is assembled by the application layer from canonical domain
// readers. The core only validates and aggregates the projection.
type OverviewInput struct {
	Now           time.Time
	Portfolio     []PortfolioItem
	VisibleApps   int
	Onboarding    []OnboardingRun
	Partners      []PartnerCertification
	Mobile        []MobileSurface
	HostedTiers   []HostedTier
	Support       SupportSnapshot
	Metrics       []OutcomeMetric
	ExternalGates []string
}

func BuildOverview(input OverviewInput) (Overview, error) {
	if !utc(input.Now) || input.VisibleApps < 0 || len(input.Portfolio) > 2048 || len(input.Onboarding) > 128 || len(input.Partners) > 128 || len(input.Mobile) > 16 || len(input.HostedTiers) > 16 || len(input.Metrics) > 128 || len(input.ExternalGates) > 32 {
		return Overview{}, ErrInvalid
	}
	out := Overview{SchemaVersion: 1, GeneratedAt: input.Now.UTC(), Consistency: "best_effort", VisibleApps: input.VisibleApps, Support: input.Support, ExternalGates: cloneStrings(input.ExternalGates)}
	if err := out.Support.Validate(); err != nil {
		return Overview{}, err
	}
	for _, item := range input.Portfolio {
		if err := item.Validate(input.Now); err != nil {
			return Overview{}, err
		}
		out.Portfolio = append(out.Portfolio, clonePortfolio(item))
		switch item.Status {
		case StatusIntegrated:
			out.StatusCounts.Integrated++
		case StatusVerified:
			out.StatusCounts.Verified++
		case StatusReady:
			out.StatusCounts.Ready++
		case StatusQualified:
			out.StatusCounts.Qualified++
		case StatusSupported:
			out.StatusCounts.Supported++
		case StatusDeprecated:
			out.StatusCounts.Deprecated++
		case StatusBlocked:
			out.StatusCounts.Blocked++
		}
	}
	for _, run := range input.Onboarding {
		if err := run.Validate(); err != nil {
			return Overview{}, err
		}
		out.Onboarding = append(out.Onboarding, cloneOnboarding(run))
	}
	for _, partner := range input.Partners {
		if err := partner.Validate(input.Now); err != nil {
			return Overview{}, err
		}
		out.Partners = append(out.Partners, partner)
	}
	for _, mobile := range input.Mobile {
		if err := mobile.Validate(input.Now); err != nil {
			return Overview{}, err
		}
		out.Mobile = append(out.Mobile, mobile)
	}
	for _, tier := range input.HostedTiers {
		if err := tier.Validate(input.Now); err != nil {
			return Overview{}, err
		}
		out.HostedTiers = append(out.HostedTiers, tier)
	}
	for _, metric := range input.Metrics {
		if err := metric.Validate(input.Now); err != nil {
			return Overview{}, err
		}
		out.Metrics = append(out.Metrics, metric)
	}
	sort.Slice(out.Portfolio, func(i, j int) bool { return out.Portfolio[i].ID < out.Portfolio[j].ID })
	sort.Slice(out.Onboarding, func(i, j int) bool { return out.Onboarding[i].ID < out.Onboarding[j].ID })
	sort.Slice(out.Partners, func(i, j int) bool { return out.Partners[i].ID < out.Partners[j].ID })
	return out, nil
}

// DefaultSurfaces are product commitments, not cloud SLA claims. The caller
// must attach retained topology evidence before setting SLAClaimable/status
// above verified.
func DefaultSurfaces(now time.Time) ([]MobileSurface, []HostedTier, SupportSnapshot, error) {
	if !utc(now) {
		return nil, nil, SupportSnapshot{}, ErrInvalid
	}
	digest := digestFor("task-231-repository-evidence")
	evidence := &Evidence{Kind: "repository", SourceRef: "task-231/repository", Digest: digest, CheckedAt: now.UTC(), Environment: "repository"}
	mobile := []MobileSurface{{ID: "mobile.warehouse", Target: "responsive_pwa", SupportedOS: []string{"android_handheld", "ios_browser", "desktop_browser"}, OfflinePolicy: "bounded_scan_queue_only", Capabilities: []string{"warehouse.pick", "warehouse.pack", "warehouse.scan", "warehouse.print", "approvals.read", "incidents.read"}, ReleaseChannel: "repository_vertical_slice", Status: StatusVerified, Evidence: evidence}}
	hosted := []HostedTier{{ID: "community.self_hosted", Name: "Community", Deployment: "self_hosted", DataResidency: "operator_selected", AvailabilityBPS: 0, APIResponseP95MS: 0, RecoveryPointMinutes: 0, RecoveryTimeMinutes: 0, SupportLevel: "community_documentation", SLAClaimable: false}, {ID: "hosted.production", Name: "Hosted production", Deployment: "hosted", DataResidency: "region_selected", AvailabilityBPS: 0, APIResponseP95MS: 0, RecoveryPointMinutes: 0, RecoveryTimeMinutes: 0, SupportLevel: "qualification_required", SLAClaimable: false}}
	support := SupportSnapshot{Policies: []SupportPolicy{{Tier: "community", Coverage: "documentation_and_issue_tracker", FirstResponseMinutes: 4320, ResolutionTargetMinutes: 0 + 10080, Owner: "community-maintainers"}, {Tier: "implementation", Coverage: "partner_managed_with_escalation", FirstResponseMinutes: 1440, ResolutionTargetMinutes: 4320, Owner: "implementation-partner"}, {Tier: "hosted", Coverage: "hosted-operations-qualification-required", FirstResponseMinutes: 60, ResolutionTargetMinutes: 1440, Owner: "hosted-operations"}}, Diagnostics: "Support targets are repository policy; actual SLA starts only after target-topology evidence."}
	return mobile, hosted, support, nil
}

func digestFor(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func utc(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
func safeRef(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max && !strings.ContainsAny(value, "\r\n\x00")
}
func isHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
func cloneStrings(values []string) []string { return append([]string(nil), values...) }
func clonePortfolio(value PortfolioItem) PortfolioItem {
	value.UseCases = cloneStrings(value.UseCases)
	value.Capabilities = cloneStrings(value.Capabilities)
	if value.Evidence != nil {
		copy := *value.Evidence
		value.Evidence = &copy
	}
	return value
}
func cloneOnboarding(value OnboardingRun) OnboardingRun {
	value.Checks = append([]OnboardingCheck(nil), value.Checks...)
	return value
}
