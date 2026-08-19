package conformance

import (
	"context"
	"errors"
	"sort"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	sandbox "github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

// SandboxFixture supplies the provider-neutral Task-029 sandbox portion of a
// provider conformance candidate. Provider packages can embed this helper and
// stay on the Connector SDK import boundary; provider-specific semantic probes
// remain the provider's responsibility.
type SandboxFixture struct {
	manifest           sdk.Manifest
	emulatorExecutable string
	capability         sdk.Capability
	secretClass        string
}

func NewSandboxFixture(manifest sdk.Manifest, emulatorExecutable string) (*SandboxFixture, error) {
	if err := manifest.Validate(); err != nil || emulatorExecutable == "" {
		return nil, ErrInvalidCandidate
	}
	capabilities := append([]sdk.Capability(nil), manifest.Capabilities...)
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	secretClass := ""
	for _, auth := range manifest.Auth {
		if auth.Required && auth.SecretClass != "" {
			secretClass = auth.SecretClass
			break
		}
	}
	if len(capabilities) == 0 || secretClass == "" {
		return nil, ErrInvalidCandidate
	}
	return &SandboxFixture{manifest: manifest, emulatorExecutable: emulatorExecutable, capability: capabilities[0], secretClass: secretClass}, nil
}

func (fixture *SandboxFixture) SandboxPlan() pluginsecurity.AdmissionPlan {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return pluginsecurity.AdmissionPlan{
		BoundaryVersion:  pluginsecurity.BoundaryVersion,
		ExecutionMode:    pluginsecurity.ExecutionIsolatedProcessV1,
		ExtensionID:      fixture.manifest.ID,
		ExtensionVersion: fixture.manifest.Version,
		ArtifactSHA256:   digest,
		Trust:            pluginsecurity.TrustVerified,
		Granted: pluginsecurity.PermissionGrant{
			ExtensionID:      fixture.manifest.ID,
			ExtensionVersion: fixture.manifest.Version,
			ArtifactSHA256:   digest,
			Capabilities:     []sdk.Capability{fixture.capability},
			SecretClasses:    []string{fixture.secretClass},
			Network:          []pluginsecurity.NetworkDestination{{Host: "api.synthetic.example", Port: 443}},
			GrantedAt:        time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		},
		Limits: pluginsecurity.IsolationLimits{MemoryMiB: 128, CPUTimeMS: 5000, WallTimeMS: 10000, MaxOutputBytes: 1 << 20, MaxConcurrentCalls: 2},
	}
}

func (fixture *SandboxFixture) SandboxOperation() sandbox.Operation {
	return sandbox.Operation{RequestID: "provider-conformance-request", ExtensionID: fixture.manifest.ID, ExtensionVersion: fixture.manifest.Version, Capability: fixture.capability, ResourceType: "product", ResourceID: "provider-product"}
}
func (fixture *SandboxFixture) SandboxExecutor() sandbox.Executor {
	return fixtureSandboxExecutor{secretClass: fixture.secretClass}
}
func (fixture *SandboxFixture) SandboxSecrets() sandbox.SecretSource       { return fixtureSandboxSecrets{} }
func (fixture *SandboxFixture) SandboxEgress() sandbox.EgressGuard         { return sandbox.EgressGuard{} }
func (fixture *SandboxFixture) SandboxTransport() sandbox.NetworkTransport { return nil }
func (fixture *SandboxFixture) IsolationProbe(ctx context.Context, plan pluginsecurity.AdmissionPlan) (sandbox.SandboxProbeResult, error) {
	runner, err := sandbox.NewLinuxSandbox(plan)
	if err != nil {
		return sandbox.SandboxProbeResult{}, err
	}
	return runner.Probe(ctx, fixture.emulatorExecutable)
}

type fixtureSandboxSecrets struct{}

func (fixtureSandboxSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, _ string, callback func([]byte) error) error {
	value := []byte("sandbox-only-provider-secret")
	defer clear(value)
	return callback(value)
}

type fixtureSandboxExecutor struct{ secretClass string }

func (executor fixtureSandboxExecutor) Execute(ctx context.Context, operation sandbox.Operation, runtime *sandbox.Runtime) ([]sandbox.Change, error) {
	if err := runtime.UseSecret(ctx, executor.secretClass, func(secret []byte) error {
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
