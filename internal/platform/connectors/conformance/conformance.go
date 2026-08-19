// Package conformance defines the reusable Connector SDK v1 certification
// harness. Provider packages may import this package through the already
// approved internal/platform/connectors SDK prefix without receiving access to
// Core, databases, concrete secrets, or host network/process primitives.
package conformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	sandbox "github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

const SuiteVersion = 1

var (
	ErrInvalidCandidate  = errors.New("connector conformance: invalid candidate")
	ErrConformanceFailed = errors.New("connector conformance: required checks failed")
	ErrTenantDenied      = errors.New("connector conformance: foreign tenant access denied")
)

type CheckID string

const (
	CheckManifestSDK          CheckID = "manifest_sdk"
	CheckAuthBoundary         CheckID = "auth_boundary"
	CheckHealthNormalization  CheckID = "health_normalization"
	CheckNormalizedErrors     CheckID = "normalized_errors"
	CheckRateLimitRetry       CheckID = "rate_limit_retry"
	CheckIdempotency          CheckID = "idempotency"
	CheckWebhookReplay        CheckID = "webhook_replay"
	CheckTenantIsolation      CheckID = "tenant_isolation"
	CheckDryRunSuppression    CheckID = "dry_run_side_effect_suppression"
	CheckProductionCredential CheckID = "production_credential_rejection"
	CheckEgressGrant          CheckID = "egress_grant_enforcement"
	CheckResourceLimit        CheckID = "resource_limit_failure"
	CheckIsolation            CheckID = "sandbox_isolation"
)

var requiredChecks = []CheckID{
	CheckManifestSDK,
	CheckAuthBoundary,
	CheckHealthNormalization,
	CheckNormalizedErrors,
	CheckRateLimitRetry,
	CheckIdempotency,
	CheckWebhookReplay,
	CheckTenantIsolation,
	CheckDryRunSuppression,
	CheckProductionCredential,
	CheckEgressGrant,
	CheckResourceLimit,
	CheckIsolation,
}

type Tenant struct {
	OrganizationID string `json:"organization_id"`
	WorkspaceID    string `json:"workspace_id"`
}

func (tenant Tenant) valid() bool {
	return tenant.OrganizationID != "" && tenant.WorkspaceID != "" && tenant.OrganizationID != tenant.WorkspaceID
}

type ProbeKind string

const (
	ProbeAuthValid       ProbeKind = "auth_valid"
	ProbeAuthInvalid     ProbeKind = "auth_invalid"
	ProbeRateLimited     ProbeKind = "rate_limited"
	ProbeIdempotentWrite ProbeKind = "idempotent_write"
	ProbeWebhook         ProbeKind = "webhook"
	ProbeTenantRead      ProbeKind = "tenant_read"
)

type ProbeRequest struct {
	Kind           ProbeKind
	Tenant         Tenant
	ResourceTenant Tenant
	IdempotencyKey string
	DeliveryID     string
}

type ProbeResult struct {
	Applied           bool
	Duplicate         bool
	EffectFingerprint string
}

// Candidate is a provider-supplied conformance adapter. It is intentionally
// narrower than provider implementation internals: the runner asks only for
// observable behavior and a Task-029 sandbox fixture.
type Candidate interface {
	Connector() sdk.Connector
	Account(Tenant) sdk.Account
	Runtime(Tenant) sdk.Runtime
	Probe(context.Context, ProbeRequest) (ProbeResult, error)
	SandboxPlan() pluginsecurity.AdmissionPlan
	SandboxOperation() sandbox.Operation
	SandboxExecutor() sandbox.Executor
	SandboxSecrets() sandbox.SecretSource
	SandboxEgress() sandbox.EgressGuard
	SandboxTransport() sandbox.NetworkTransport
	IsolationProbe(context.Context, pluginsecurity.AdmissionPlan) (sandbox.SandboxProbeResult, error)
}

type CheckStatus string

const (
	StatusPass CheckStatus = "pass"
	StatusFail CheckStatus = "fail"
)

type CheckResult struct {
	ID         CheckID     `json:"id"`
	Status     CheckStatus `json:"status"`
	ReasonCode string      `json:"reason_code,omitempty"`
}

type Report struct {
	SuiteVersion     int           `json:"suite_version"`
	ConnectorID      string        `json:"connector_id"`
	ConnectorVersion string        `json:"connector_version"`
	SDKVersion       int           `json:"sdk_version"`
	Passed           bool          `json:"passed"`
	Checks           []CheckResult `json:"checks"`
	CompletedAt      time.Time     `json:"completed_at"`
	ReportSHA256     string        `json:"report_sha256"`
}

func (report Report) Validate() error {
	identity, release, major := report.ConnectorID, report.ConnectorVersion, report.SDKVersion
	if report.SuiteVersion != SuiteVersion || identity == "" || release == "" || major != sdk.SDKMajor || report.CompletedAt.IsZero() || report.CompletedAt.Location() != time.UTC || len(report.ReportSHA256) != 64 {
		return ErrInvalidCandidate
	}
	if len(report.Checks) != len(requiredChecks) {
		return ErrInvalidCandidate
	}
	passed := true
	for index, expected := range requiredChecks {
		current := report.Checks[index]
		if current.ID != expected || (current.Status != StatusPass && current.Status != StatusFail) {
			return ErrInvalidCandidate
		}
		if current.Status == StatusPass && current.ReasonCode != "" {
			return ErrInvalidCandidate
		}
		if current.Status == StatusFail {
			passed = false
			if !safeReason(current.ReasonCode) {
				return ErrInvalidCandidate
			}
		}
	}
	if report.Passed != passed {
		return ErrInvalidCandidate
	}
	if report.ReportSHA256 != reportDigest(report) {
		return ErrInvalidCandidate
	}
	return nil
}

func Run(ctx context.Context, candidate Candidate, primary, foreign Tenant, now func() time.Time) Report {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	report := Report{SuiteVersion: SuiteVersion, Checks: make([]CheckResult, 0, len(requiredChecks))}
	if candidate == nil || !primary.valid() || !foreign.valid() || primary == foreign {
		report.ConnectorID = "invalid"
		report.ConnectorVersion = "invalid"
		report.SDKVersion = sdk.SDKMajor
		for _, id := range requiredChecks {
			report.Checks = append(report.Checks, CheckResult{ID: id, Status: StatusFail, ReasonCode: "invalid_candidate"})
		}
		report.CompletedAt = now().UTC()
		report.ReportSHA256 = reportDigest(report)
		return report
	}
	extension := candidate.Connector()
	if extension != nil {
		manifest := extension.Manifest()
		report.ConnectorID = manifest.ID
		report.ConnectorVersion = manifest.Version
		report.SDKVersion = manifest.SDKVersion
	}
	checks := []struct {
		id CheckID
		fn func(context.Context, Candidate, Tenant, Tenant) string
	}{
		{CheckManifestSDK, checkManifest},
		{CheckAuthBoundary, checkAuth},
		{CheckHealthNormalization, checkHealth},
		{CheckNormalizedErrors, checkErrors},
		{CheckRateLimitRetry, checkRateLimit},
		{CheckIdempotency, checkIdempotency},
		{CheckWebhookReplay, checkWebhook},
		{CheckTenantIsolation, checkTenantIsolation},
		{CheckDryRunSuppression, checkDryRun},
		{CheckProductionCredential, checkProductionCredential},
		{CheckEgressGrant, checkEgress},
		{CheckResourceLimit, checkResourceLimit},
		{CheckIsolation, checkIsolation},
	}
	report.Passed = true
	for _, item := range checks {
		reason := "cancelled"
		if ctx != nil && ctx.Err() == nil {
			reason = item.fn(ctx, candidate, primary, foreign)
		}
		result := CheckResult{ID: item.id, Status: StatusPass}
		if reason != "" {
			result.Status = StatusFail
			result.ReasonCode = normalizeReason(reason)
			report.Passed = false
		}
		report.Checks = append(report.Checks, result)
	}
	completed := now().UTC()
	if completed.IsZero() {
		completed = time.Unix(1, 0).UTC()
	}
	report.CompletedAt = completed
	report.ReportSHA256 = reportDigest(report)
	return report
}

func Require(report Report) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if !report.Passed {
		return ErrConformanceFailed
	}
	return nil
}

func WriteJSON(writer io.Writer, report Report) error {
	if writer == nil {
		return ErrInvalidCandidate
	}
	if err := report.Validate(); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	return encoder.Encode(report)
}

func RequiredChecks() []CheckID { return append([]CheckID(nil), requiredChecks...) }

func reportDigest(report Report) string {
	copyReport := report
	copyReport.ReportSHA256 = ""
	data, _ := json.Marshal(copyReport)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func safeReason(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (index > 0 && r >= '0' && r <= '9') || (index > 0 && (r == '_' || r == '-')) {
			continue
		}
		return false
	}
	return true
}

func normalizeReason(value string) string {
	if safeReason(value) {
		return value
	}
	return "conformance_failed"
}

func checkManifest(_ context.Context, candidate Candidate, _, _ Tenant) string {
	extension := candidate.Connector()
	if extension == nil {
		return "connector_missing"
	}
	manifest := extension.Manifest()
	if err := manifest.Validate(); err != nil || manifest.SDKVersion != sdk.SDKMajor {
		return "manifest_invalid"
	}
	plan := candidate.SandboxPlan()
	if plan.ExtensionID != manifest.ID || plan.ExtensionVersion != manifest.Version || plan.BoundaryVersion != pluginsecurity.BoundaryVersion {
		return "sandbox_identity_mismatch"
	}
	granted := append([]sdk.Capability(nil), plan.Granted.Capabilities...)
	sort.Slice(granted, func(i, j int) bool { return granted[i] < granted[j] })
	for _, capability := range granted {
		if !manifest.Supports(capability) {
			return "grant_capability_mismatch"
		}
	}
	return ""
}

func checkAuth(ctx context.Context, candidate Candidate, primary, _ Tenant) string {
	manifest := candidate.Connector().Manifest()
	if _, err := candidate.Probe(ctx, ProbeRequest{Kind: ProbeAuthValid, Tenant: primary}); err != nil {
		return "auth_valid_failed"
	}
	_, err := candidate.Probe(ctx, ProbeRequest{Kind: ProbeAuthInvalid, Tenant: primary})
	if !manifest.RequiresSecret() {
		if err != nil {
			var remote *sdk.RemoteError
			if !errors.As(err, &remote) || remote.Validate() != nil || remote.Category != sdk.ErrorUnsupported {
				return "auth_none_invalid_probe"
			}
		}
		return ""
	}
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Validate() != nil || (remote.Category != sdk.ErrorUnauthorized && remote.Category != sdk.ErrorForbidden) {
		return "auth_not_normalized"
	}
	return ""
}

func checkHealth(ctx context.Context, candidate Candidate, primary, _ Tenant) string {
	extension := candidate.Connector()
	account := candidate.Account(primary)
	if account.Validate() != nil || sdk.ValidateAccountAgainstManifest(account, extension.Manifest()) != nil {
		return "account_invalid"
	}
	health, err := extension.Health(ctx, account, candidate.Runtime(primary))
	if err != nil || health.Validate() != nil || health.Status == sdk.HealthUnknown {
		return "health_invalid"
	}
	return ""
}

func checkErrors(ctx context.Context, candidate Candidate, primary, _ Tenant) string {
	_, err := candidate.Probe(ctx, ProbeRequest{Kind: ProbeRateLimited, Tenant: primary})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Validate() != nil || remote.Category != sdk.ErrorRateLimited || !remote.Retryable() {
		return "remote_error_invalid"
	}
	if remote.Error() == "" || remote.Code == "" {
		return "remote_error_empty"
	}
	return ""
}

func checkRateLimit(ctx context.Context, candidate Candidate, primary, _ Tenant) string {
	_, err := candidate.Probe(ctx, ProbeRequest{Kind: ProbeRateLimited, Tenant: primary})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Validate() != nil {
		return "rate_limit_error_invalid"
	}
	policy := candidate.Connector().Manifest().RateLimit.Retry
	first, ok := sdk.RetryDelay(policy, 1, remote)
	if !ok || first <= 0 || first > time.Duration(policy.MaxBackoffMS)*time.Millisecond {
		return "retry_delay_invalid"
	}
	if _, ok := sdk.RetryDelay(policy, policy.MaxAttempts, remote); ok {
		return "retry_attempt_bound_failed"
	}
	return ""
}

func checkIdempotency(ctx context.Context, candidate Candidate, primary, _ Tenant) string {
	request := ProbeRequest{Kind: ProbeIdempotentWrite, Tenant: primary, IdempotencyKey: "conformance-idem-001"}
	first, err := candidate.Probe(ctx, request)
	if err != nil || !first.Applied || first.Duplicate || len(first.EffectFingerprint) != 64 {
		return "idempotency_first_failed"
	}
	second, err := candidate.Probe(ctx, request)
	if err != nil || second.Applied || !second.Duplicate || second.EffectFingerprint != first.EffectFingerprint {
		return "idempotency_duplicate_failed"
	}
	return ""
}

func checkWebhook(ctx context.Context, candidate Candidate, primary, _ Tenant) string {
	request := ProbeRequest{Kind: ProbeWebhook, Tenant: primary, DeliveryID: "delivery-conformance-001"}
	first, err := candidate.Probe(ctx, request)
	if err != nil || !first.Applied || first.Duplicate || len(first.EffectFingerprint) != 64 {
		return "webhook_first_failed"
	}
	second, err := candidate.Probe(ctx, request)
	if err != nil || second.Applied || !second.Duplicate || second.EffectFingerprint != first.EffectFingerprint {
		return "webhook_replay_failed"
	}
	return ""
}

func checkTenantIsolation(ctx context.Context, candidate Candidate, primary, foreign Tenant) string {
	_, err := candidate.Probe(ctx, ProbeRequest{Kind: ProbeTenantRead, Tenant: primary, ResourceTenant: foreign})
	if !errors.Is(err, ErrTenantDenied) {
		return "foreign_tenant_visible"
	}
	if _, err := candidate.Probe(ctx, ProbeRequest{Kind: ProbeTenantRead, Tenant: primary, ResourceTenant: primary}); err != nil {
		return "own_tenant_unavailable"
	}
	return ""
}

func checkDryRun(ctx context.Context, candidate Candidate, _, _ Tenant) string {
	secrets := &countingSecrets{delegate: candidate.SandboxSecrets()}
	transport := &countingTransport{delegate: candidate.SandboxTransport()}
	session, err := sandbox.NewSession(sandbox.ModeDryRun, candidate.SandboxPlan(), sandbox.CredentialBinding{Tier: sandbox.CredentialProduction, Reference: "sec:v1:0123456789abcdef0123456789abcdef"}, secrets, candidate.SandboxEgress(), transport)
	if err != nil {
		return "dry_run_session_invalid"
	}
	result, err := session.Run(ctx, candidate.SandboxOperation(), candidate.SandboxExecutor())
	if err != nil || result.Status != sandbox.StatusPlanned || secrets.calls != 0 || transport.calls != 0 || !result.Isolation.ProductionCredentialsBlocked || !result.Isolation.EgressMediated {
		return "dry_run_side_effect"
	}
	return ""
}

func checkProductionCredential(_ context.Context, candidate Candidate, _, _ Tenant) string {
	secrets := &countingSecrets{delegate: candidate.SandboxSecrets()}
	_, err := sandbox.NewSession(sandbox.ModeTest, candidate.SandboxPlan(), sandbox.CredentialBinding{Tier: sandbox.CredentialProduction, Reference: "sec:v1:0123456789abcdef0123456789abcdef"}, secrets, candidate.SandboxEgress(), candidate.SandboxTransport())
	if !errors.Is(err, sandbox.ErrProductionCredential) || secrets.calls != 0 {
		return "production_credential_not_blocked"
	}
	return ""
}

func checkEgress(ctx context.Context, candidate Candidate, _, _ Tenant) string {
	plan := candidate.SandboxPlan()
	session, err := sandbox.NewSession(sandbox.ModeDryRun, plan, sandbox.CredentialBinding{Tier: sandbox.CredentialSandbox}, nil, candidate.SandboxEgress(), nil)
	if err != nil {
		return "egress_session_invalid"
	}
	denied := pluginsecurity.NetworkDestination{Host: "denied.invalid.example", Port: 443}
	operation := candidate.SandboxOperation()
	result, err := session.Run(ctx, operation, egressExecutor{destination: denied})
	if !errors.Is(err, sandbox.ErrEgressDenied) || result.Status != sandbox.StatusRejected || result.ReasonCode != "egress_denied" {
		return "egress_deny_failed"
	}
	if len(plan.Granted.Network) != 0 {
		allowedSession, err := sandbox.NewSession(sandbox.ModeDryRun, plan, sandbox.CredentialBinding{Tier: sandbox.CredentialSandbox}, nil, candidate.SandboxEgress(), nil)
		if err != nil {
			return "egress_allowed_session_invalid"
		}
		allowed, err := allowedSession.Run(ctx, operation, egressExecutor{destination: plan.Granted.Network[0]})
		if err != nil || allowed.Status != sandbox.StatusPlanned || len(allowed.ExternalActions) != 1 {
			return "egress_allow_failed"
		}
	}
	return ""
}

func checkResourceLimit(ctx context.Context, candidate Candidate, _, _ Tenant) string {
	plan := candidate.SandboxPlan()
	plan.Limits.MaxOutputBytes = 1024
	session, err := sandbox.NewSession(sandbox.ModeDryRun, plan, sandbox.CredentialBinding{Tier: sandbox.CredentialSandbox}, nil, candidate.SandboxEgress(), nil)
	if err != nil {
		return "resource_session_invalid"
	}
	result, err := session.Run(ctx, candidate.SandboxOperation(), outputLimitExecutor{})
	if !errors.Is(err, sandbox.ErrResourceLimit) || result.Status != sandbox.StatusLimitExceeded || result.ReasonCode != "output_limit" {
		return "resource_limit_not_enforced"
	}
	return ""
}

func checkIsolation(ctx context.Context, candidate Candidate, _, _ Tenant) string {
	result, err := candidate.IsolationProbe(ctx, candidate.SandboxPlan())
	if err != nil {
		return "isolation_probe_failed"
	}
	evidence := result.Isolation
	if result.Report.EnvironmentVisible || result.Report.FilesystemVisible || result.Report.DirectNetworkReachable || !evidence.ProductionCredentialsBlocked || !evidence.EnvironmentIsolated || !evidence.FilesystemIsolated || !evidence.DirectNetworkBlocked || !evidence.EgressMediated || !evidence.ResourceLimitsEnforced {
		return "isolation_evidence_failed"
	}
	return ""
}

type countingSecrets struct {
	delegate sandbox.SecretSource
	calls    int
}

func (source *countingSecrets) UseSecret(ctx context.Context, reference sdk.SecretReference, class string, callback func([]byte) error) error {
	source.calls++
	if source.delegate == nil {
		return errors.New("secret unavailable")
	}
	return source.delegate.UseSecret(ctx, reference, class, callback)
}

type countingTransport struct {
	delegate sandbox.NetworkTransport
	calls    int
}

func (transport *countingTransport) Do(ctx context.Context, targets []sandbox.DialTarget, request sandbox.NetworkRequest) error {
	transport.calls++
	if transport.delegate == nil {
		return errors.New("transport unavailable")
	}
	return transport.delegate.Do(ctx, targets, request)
}

type egressExecutor struct {
	destination pluginsecurity.NetworkDestination
}

func (executor egressExecutor) Execute(ctx context.Context, operation sandbox.Operation, runtime *sandbox.Runtime) ([]sandbox.Change, error) {
	if err := runtime.Network(ctx, sandbox.NetworkRequest{Method: "POST", Destination: executor.destination, RouteTemplate: "/conformance/{id}"}); err != nil {
		return nil, err
	}
	return []sandbox.Change{{ResourceType: operation.ResourceType, ResourceID: operation.ResourceID, Kind: sandbox.ChangeUpdate, BeforeSHA256: sandbox.DigestCanonical("before"), AfterSHA256: sandbox.DigestCanonical("after")}}, nil
}

type outputLimitExecutor struct{}

func (outputLimitExecutor) Execute(_ context.Context, operation sandbox.Operation, _ *sandbox.Runtime) ([]sandbox.Change, error) {
	changes := make([]sandbox.Change, 0, 32)
	for index := 0; index < 32; index++ {
		changes = append(changes, sandbox.Change{ResourceType: operation.ResourceType, ResourceID: fmt.Sprintf("%s-%03d-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", operation.ResourceID, index), Kind: sandbox.ChangeUpdate, BeforeSHA256: sandbox.DigestCanonical(index), AfterSHA256: sandbox.DigestCanonical(index + 1)})
	}
	return changes, nil
}
