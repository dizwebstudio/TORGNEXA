package connectorsandbox

import (
	"context"
	"errors"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type spySecrets struct{ calls atomic.Int64 }

func (s *spySecrets) UseSecret(_ context.Context, _ sdk.SecretReference, _ string, callback func([]byte) error) error {
	s.calls.Add(1)
	return callback([]byte("sandbox-only-secret"))
}

type fixedResolver struct {
	values []netip.Addr
	err    error
}

func (r fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r.values...), r.err
}

type spyTransport struct {
	calls   atomic.Int64
	targets []DialTarget
}

func (t *spyTransport) Do(_ context.Context, targets []DialTarget, _ NetworkRequest) error {
	t.calls.Add(1)
	t.targets = append([]DialTarget(nil), targets...)
	return nil
}

func testPlan() pluginsecurity.AdmissionPlan {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	grant := pluginsecurity.PermissionGrant{
		ExtensionID: "synthetic-shop", ExtensionVersion: "1.2.3", ArtifactSHA256: digest,
		Capabilities: []sdk.Capability{"products.read"}, SecretClasses: []string{"marketplace.oauth"},
		Network:   []pluginsecurity.NetworkDestination{{Host: "api.synthetic.example", Port: 443}},
		GrantedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}
	return pluginsecurity.AdmissionPlan{BoundaryVersion: pluginsecurity.BoundaryVersion, ExecutionMode: pluginsecurity.ExecutionIsolatedProcessV1, ExtensionID: "synthetic-shop", ExtensionVersion: "1.2.3", ArtifactSHA256: digest, Trust: pluginsecurity.TrustVerified, Granted: grant, Limits: pluginsecurity.IsolationLimits{MemoryMiB: 128, CPUTimeMS: 5000, WallTimeMS: 10000, MaxOutputBytes: 1 << 20, MaxConcurrentCalls: 2}}
}

func testOperation() Operation {
	return Operation{RequestID: "req-1", ExtensionID: "synthetic-shop", ExtensionVersion: "1.2.3", Capability: "products.read", ResourceType: "product", ResourceID: "product-1"}
}

func TestDryRunNeverTouchesCredentialOrNetworkProviders(t *testing.T) {
	plan := testPlan()
	secrets := &spySecrets{}
	transport := &spyTransport{}
	session, err := NewSession(ModeDryRun, plan, CredentialBinding{Tier: CredentialProduction, Reference: "sec:v1:0123456789abcdef0123456789abcdef"}, secrets, EgressGuard{Resolver: fixedResolver{values: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}, transport)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	result, err := session.Run(context.Background(), testOperation(), syntheticExecutor{SecretClass: "marketplace.oauth", Destination: plan.Granted.Network[0], RouteTemplate: "/v1/products/{id}"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != StatusPlanned || len(result.ExternalActions) != 1 || len(result.Changes) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if secrets.calls.Load() != 0 {
		t.Fatal("dry-run touched secret provider")
	}
	if transport.calls.Load() != 0 {
		t.Fatal("dry-run performed external network")
	}
	if !result.Isolation.ProductionCredentialsBlocked || !result.Isolation.EgressMediated {
		t.Fatalf("missing isolation evidence: %#v", result.Isolation)
	}
}

func TestTestModeRejectsProductionCredentialBeforeBrokerUse(t *testing.T) {
	secrets := &spySecrets{}
	_, err := NewSession(ModeTest, testPlan(), CredentialBinding{Tier: CredentialProduction, Reference: "sec:v1:0123456789abcdef0123456789abcdef"}, secrets, EgressGuard{}, nil)
	if !errors.Is(err, ErrProductionCredential) {
		t.Fatalf("want production credential rejection, got %v", err)
	}
	if secrets.calls.Load() != 0 {
		t.Fatal("production broker touched before rejection")
	}
}

func TestTestModeUsesOnlySandboxSecretAndPinnedEgress(t *testing.T) {
	plan := testPlan()
	secrets := &spySecrets{}
	transport := &spyTransport{}
	session, err := NewSession(ModeTest, plan, CredentialBinding{Tier: CredentialSandbox, Reference: "sec:v1:0123456789abcdef0123456789abcdef"}, secrets, EgressGuard{Resolver: fixedResolver{values: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}, transport)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Run(context.Background(), testOperation(), syntheticExecutor{SecretClass: "marketplace.oauth", Destination: plan.Granted.Network[0], RouteTemplate: "/v1/products/{id}"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != StatusSucceeded || secrets.calls.Load() != 1 || transport.calls.Load() != 1 {
		t.Fatalf("unexpected execution result=%#v secrets=%d network=%d", result, secrets.calls.Load(), transport.calls.Load())
	}
	if len(transport.targets) != 1 || transport.targets[0].IP.String() != "93.184.216.34" || transport.targets[0].ServerName != "api.synthetic.example" {
		t.Fatalf("unexpected pinned target: %#v", transport.targets)
	}
}

func TestSessionEnforcesOutputBudget(t *testing.T) {
	plan := testPlan()
	plan.Limits.MaxOutputBytes = 1024
	session, err := NewSession(ModeDryRun, plan, CredentialBinding{Tier: CredentialSandbox}, nil, EgressGuard{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Run(context.Background(), testOperation(), largeExecutor{})
	if !errors.Is(err, ErrResourceLimit) || result.Status != StatusLimitExceeded || result.ReasonCode != "output_limit" {
		t.Fatalf("want output limit, got result=%#v err=%v", result, err)
	}
}

type largeExecutor struct{}

func (largeExecutor) Execute(_ context.Context, operation Operation, _ *Runtime) ([]Change, error) {
	changes := make([]Change, 0, 40)
	for i := 0; i < 40; i++ {
		changes = append(changes, Change{ResourceType: operation.ResourceType, ResourceID: operation.ResourceID + "-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", Kind: ChangeUpdate, BeforeSHA256: DigestCanonical(i), AfterSHA256: DigestCanonical(i + 1)})
	}
	return changes, nil
}

type syntheticExecutor struct {
	SecretClass   string
	Destination   pluginsecurity.NetworkDestination
	RouteTemplate string
}

func (executor syntheticExecutor) Execute(ctx context.Context, operation Operation, runtime *Runtime) ([]Change, error) {
	if executor.SecretClass != "" {
		if err := runtime.UseSecret(ctx, executor.SecretClass, func(secret []byte) error {
			if len(secret) == 0 {
				return errors.New("empty synthetic secret")
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if executor.Destination.Host != "" {
		if err := runtime.Network(ctx, NetworkRequest{Method: "POST", Destination: executor.Destination, RouteTemplate: executor.RouteTemplate}); err != nil {
			return nil, err
		}
	}
	return []Change{{ResourceType: operation.ResourceType, ResourceID: operation.ResourceID, Kind: ChangeUpdate, BeforeSHA256: DigestCanonical(map[string]any{"version": 1}), AfterSHA256: DigestCanonical(map[string]any{"version": 2})}}, nil
}

type fixedGuard struct {
	err   error
	calls atomic.Int64
}

func (g *fixedGuard) Authorize(context.Context, Operation) error { g.calls.Add(1); return g.err }
func writePlan() pluginsecurity.AdmissionPlan {
	p := testPlan()
	p.Granted.Capabilities = []sdk.Capability{"products.write"}
	return p
}
func writeOperation() Operation { o := testOperation(); o.Capability = "products.write"; return o }
func TestProductWriteFailsClosedWithoutComplianceGuard(t *testing.T) {
	p := writePlan()
	s, e := NewSession(ModeDryRun, p, CredentialBinding{Tier: CredentialSandbox}, nil, EgressGuard{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Run(context.Background(), writeOperation(), syntheticExecutor{})
	if !errors.Is(e, ErrPolicyDenied) || result.ReasonCode != "product_compliance_denied" {
		t.Fatalf("result=%#v err=%v", result, e)
	}
}
func TestProductWriteRunsOnlyAfterHostGuard(t *testing.T) {
	p := writePlan()
	g := &fixedGuard{}
	s, e := NewGuardedSession(ModeDryRun, p, CredentialBinding{Tier: CredentialSandbox}, nil, EgressGuard{}, nil, g)
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Run(context.Background(), writeOperation(), syntheticExecutor{})
	if e != nil || result.Status != StatusPlanned || g.calls.Load() != 1 {
		t.Fatalf("result=%#v err=%v calls=%d", result, e, g.calls.Load())
	}
}

type countingExecutor struct{ calls atomic.Int64 }

func (e *countingExecutor) Execute(context.Context, Operation, *Runtime) ([]Change, error) {
	e.calls.Add(1)
	return nil, nil
}
func TestComplianceDenialHappensBeforeProviderExecutor(t *testing.T) {
	p := writePlan()
	g := &fixedGuard{err: ErrPolicyDenied}
	s, e := NewGuardedSession(ModeDryRun, p, CredentialBinding{Tier: CredentialSandbox}, nil, EgressGuard{}, nil, g)
	if e != nil {
		t.Fatal(e)
	}
	x := &countingExecutor{}
	_, e = s.Run(context.Background(), writeOperation(), x)
	if !errors.Is(e, ErrPolicyDenied) {
		t.Fatalf("err=%v", e)
	}
	if x.calls.Load() != 0 {
		t.Fatalf("executor ran %d times", x.calls.Load())
	}
}
