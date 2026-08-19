package conformance

import (
	"context"
	"errors"
	"sync"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	sandbox "github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

// ReferenceCandidate is a deterministic provider-neutral specimen used to
// qualify the harness itself. Real providers implement Candidate in their own
// conformance tests; this specimen is never registered as a provider.
type ReferenceCandidate struct {
	EmulatorExecutable string
	mu                 sync.Mutex
	idempotency        map[string]string
	webhooks           map[string]string
}

func NewReferenceCandidate(emulatorExecutable string) *ReferenceCandidate {
	return &ReferenceCandidate{EmulatorExecutable: emulatorExecutable, idempotency: map[string]string{}, webhooks: map[string]string{}}
}

func (candidate *ReferenceCandidate) Connector() sdk.Connector { return referenceConnector{} }

func referenceManifest() sdk.Manifest {
	return sdk.Manifest{
		ID: "conformance-reference", Name: "Conformance Reference", Family: sdk.FamilyMarketplace, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"products.read"},
		Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthOAuth2, SecretClass: "marketplace.oauth", Required: true}},
		RateLimit:    sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 10, RequestTimeoutMS: 5000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 100, MaxBackoffMS: 2000}},
	}
}

type referenceConnector struct{}

func (referenceConnector) Manifest() sdk.Manifest { return referenceManifest() }
func (referenceConnector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if account.Validate() != nil || runtime == nil || runtime.Secrets() == nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		if len(secret) == 0 {
			return errors.New("empty")
		}
		return nil
	})
	if err != nil {
		remote, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		return sdk.Health{CheckedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}, remote
	}
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidate *ReferenceCandidate) Account(tenant Tenant) sdk.Account {
	return sdk.Account{
		ID: "conformance-account", OrganizationID: tenant.OrganizationID, WorkspaceID: tenant.WorkspaceID,
		ConnectorID: referenceManifest().ID, Family: sdk.FamilyMarketplace, Status: sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown},
		CreatedAt: time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC),
	}
}

func (candidate *ReferenceCandidate) Runtime(Tenant) sdk.Runtime {
	return referenceRuntime{secrets: referenceSecrets{}}
}

type referenceRuntime struct{ secrets sdk.SecretAccessor }

func (runtime referenceRuntime) Secrets() sdk.SecretAccessor { return runtime.secrets }

type referenceSecrets struct{}

func (referenceSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := []byte("sandbox-reference-secret")
	defer clear(value)
	return callback(value)
}

func (candidate *ReferenceCandidate) Probe(_ context.Context, request ProbeRequest) (ProbeResult, error) {
	switch request.Kind {
	case ProbeAuthValid:
		return ProbeResult{}, nil
	case ProbeAuthInvalid:
		remote, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		return ProbeResult{}, remote
	case ProbeRateLimited:
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "req-conformance-1", 750*time.Millisecond)
		return ProbeResult{}, remote
	case ProbeIdempotentWrite:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		if request.IdempotencyKey == "" {
			return ProbeResult{}, errors.New("missing key")
		}
		if fingerprint, exists := candidate.idempotency[request.IdempotencyKey]; exists {
			return ProbeResult{Duplicate: true, EffectFingerprint: fingerprint}, nil
		}
		fingerprint := sandbox.DigestCanonical(map[string]string{"tenant": request.Tenant.OrganizationID, "key": request.IdempotencyKey})
		candidate.idempotency[request.IdempotencyKey] = fingerprint
		return ProbeResult{Applied: true, EffectFingerprint: fingerprint}, nil
	case ProbeWebhook:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		if request.DeliveryID == "" {
			return ProbeResult{}, errors.New("missing delivery")
		}
		if fingerprint, exists := candidate.webhooks[request.DeliveryID]; exists {
			return ProbeResult{Duplicate: true, EffectFingerprint: fingerprint}, nil
		}
		fingerprint := sandbox.DigestCanonical(map[string]string{"tenant": request.Tenant.WorkspaceID, "delivery": request.DeliveryID})
		candidate.webhooks[request.DeliveryID] = fingerprint
		return ProbeResult{Applied: true, EffectFingerprint: fingerprint}, nil
	case ProbeTenantRead:
		if request.Tenant != request.ResourceTenant {
			return ProbeResult{}, ErrTenantDenied
		}
		return ProbeResult{}, nil
	default:
		return ProbeResult{}, ErrInvalidCandidate
	}
}

func (candidate *ReferenceCandidate) SandboxPlan() pluginsecurity.AdmissionPlan {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return pluginsecurity.AdmissionPlan{
		BoundaryVersion: pluginsecurity.BoundaryVersion, ExecutionMode: pluginsecurity.ExecutionIsolatedProcessV1,
		ExtensionID: referenceManifest().ID, ExtensionVersion: referenceManifest().Version, ArtifactSHA256: digest, Trust: pluginsecurity.TrustVerified,
		Granted: pluginsecurity.PermissionGrant{ExtensionID: referenceManifest().ID, ExtensionVersion: referenceManifest().Version, ArtifactSHA256: digest,
			Capabilities: []sdk.Capability{"products.read"}, SecretClasses: []string{"marketplace.oauth"}, Network: []pluginsecurity.NetworkDestination{{Host: "api.synthetic.example", Port: 443}}, GrantedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)},
		Limits: pluginsecurity.IsolationLimits{MemoryMiB: 128, CPUTimeMS: 5000, WallTimeMS: 10000, MaxOutputBytes: 1 << 20, MaxConcurrentCalls: 2},
	}
}
func (candidate *ReferenceCandidate) SandboxOperation() sandbox.Operation {
	return sandbox.Operation{RequestID: "conformance-request", ExtensionID: referenceManifest().ID, ExtensionVersion: referenceManifest().Version, Capability: "products.read", ResourceType: "product", ResourceID: "reference-product"}
}
func (candidate *ReferenceCandidate) SandboxExecutor() sandbox.Executor {
	return referenceSandboxExecutor{}
}
func (candidate *ReferenceCandidate) SandboxSecrets() sandbox.SecretSource {
	return referenceSandboxSecrets{}
}
func (candidate *ReferenceCandidate) SandboxEgress() sandbox.EgressGuard {
	return sandbox.EgressGuard{}
}
func (candidate *ReferenceCandidate) SandboxTransport() sandbox.NetworkTransport { return nil }
func (candidate *ReferenceCandidate) IsolationProbe(ctx context.Context, plan pluginsecurity.AdmissionPlan) (sandbox.SandboxProbeResult, error) {
	runner, err := sandbox.NewLinuxSandbox(plan)
	if err != nil {
		return sandbox.SandboxProbeResult{}, err
	}
	return runner.Probe(ctx, candidate.EmulatorExecutable)
}

type referenceSandboxSecrets struct{}

func (referenceSandboxSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, _ string, callback func([]byte) error) error {
	value := []byte("sandbox-only-reference-secret")
	defer clear(value)
	return callback(value)
}

type referenceSandboxExecutor struct{}

func (referenceSandboxExecutor) Execute(ctx context.Context, operation sandbox.Operation, runtime *sandbox.Runtime) ([]sandbox.Change, error) {
	if err := runtime.UseSecret(ctx, "marketplace.oauth", func(secret []byte) error {
		if len(secret) == 0 {
			return errors.New("empty")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := runtime.Network(ctx, sandbox.NetworkRequest{Method: "GET", Destination: pluginsecurity.NetworkDestination{Host: "api.synthetic.example", Port: 443}, RouteTemplate: "/v1/products/{id}"}); err != nil {
		return nil, err
	}
	return []sandbox.Change{{ResourceType: operation.ResourceType, ResourceID: operation.ResourceID, Kind: sandbox.ChangeUpdate, BeforeSHA256: sandbox.DigestCanonical("v1"), AfterSHA256: sandbox.DigestCanonical("v2")}}, nil
}
