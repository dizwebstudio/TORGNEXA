package connectors

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSDKV1RootSurfaceIsFrozen(t *testing.T) {
	tests := []struct {
		name    string
		typeOf  reflect.Type
		methods []string
	}{
		{name: "Connector", typeOf: reflect.TypeOf((*Connector)(nil)).Elem(), methods: []string{"Health", "Manifest"}},
		{name: "Runtime", typeOf: reflect.TypeOf((*Runtime)(nil)).Elem(), methods: []string{"Secrets"}},
		{name: "SecretAccessor", typeOf: reflect.TypeOf((*SecretAccessor)(nil)).Elem(), methods: []string{"UseSecret"}},
	}
	for _, test := range tests {
		if test.typeOf.NumMethod() != len(test.methods) {
			t.Fatalf("SDK v1 %s method count changed: got %d want %d", test.name, test.typeOf.NumMethod(), len(test.methods))
		}
		for index, name := range test.methods {
			if method := test.typeOf.Method(index); method.Name != name {
				t.Fatalf("SDK v1 %s method[%d]=%s want %s", test.name, index, method.Name, name)
			}
		}
	}
}

func validMarketplaceManifest() Manifest {
	return Manifest{
		ID:           "synthetic-market",
		Name:         "Synthetic Market",
		Family:       FamilyMarketplace,
		Version:      "1.2.3",
		SDKVersion:   SDKMajor,
		Capabilities: []Capability{"orders.read", "products.read", "prices.write"},
		Auth:         []AuthRequirement{{Kind: AuthOAuth2, SecretClass: "oauth_refresh", Required: true}},
		RateLimit:    RateLimitPolicy{MaxConcurrency: 4, MinIntervalMS: 10, RequestTimeoutMS: 30000, Retry: RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 250, MaxBackoffMS: 10000}},
	}
}

func TestManifestValidatesCapabilityFamilyAndSDK(t *testing.T) {
	manifest := validMarketplaceManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if !manifest.RequiresSecret() {
		t.Fatal("oauth connector must require secret")
	}
	if err := RequireCapability(manifest, "prices.write"); err != nil {
		t.Fatal(err)
	}
	if err := RequireCapability(manifest, "social.post.text"); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("missing capability: %v", err)
	}

	bad := manifest
	bad.Capabilities = []Capability{"social.post.text"}
	if err := bad.Validate(); !errors.Is(err, ErrCapabilityFamily) {
		t.Fatalf("family mismatch accepted: %v", err)
	}
	bad = manifest
	bad.SDKVersion = 2
	if err := bad.Validate(); !errors.Is(err, ErrUnsupportedSDKVersion) {
		t.Fatalf("unsupported sdk accepted: %v", err)
	}
	bad = manifest
	bad.Capabilities = []Capability{"prices.write", "prices.write"}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("duplicate capability accepted: %v", err)
	}
}

func TestFXAndNotificationAreFirstClassFamilies(t *testing.T) {
	cases := []Manifest{
		{ID: "reference-fx", Name: "Reference FX", Family: FamilyFX, Version: "1.0.0", SDKVersion: 1, Capabilities: []Capability{"fx.rates.read"}, Auth: []AuthRequirement{{Kind: AuthAPIKey, SecretClass: "connector_token", Required: true}}, RateLimit: RateLimitPolicy{MaxConcurrency: 1, RequestTimeoutMS: 1000, Retry: RetryPolicy{MaxAttempts: 2, BaseBackoffMS: 100, MaxBackoffMS: 1000}}},
		{ID: "reference-sms", Name: "Reference SMS", Family: FamilyNotification, Version: "1.0.0", SDKVersion: 1, Capabilities: []Capability{"notifications.sms.send", "notifications.sms.status.read"}, Auth: []AuthRequirement{{Kind: AuthBearer, SecretClass: "connector_token", Required: true}}, RateLimit: RateLimitPolicy{MaxConcurrency: 1, RequestTimeoutMS: 1000, Retry: RetryPolicy{MaxAttempts: 2, BaseBackoffMS: 100, MaxBackoffMS: 1000}}},
	}
	for _, manifest := range cases {
		if err := manifest.Validate(); err != nil {
			t.Fatalf("%s: %v", manifest.ID, err)
		}
	}
}

func TestCapabilityRegistryMatchesCanonicalYAML(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "contracts", "connector-capabilities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var yamlCaps []string
	inCaps := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "capabilities:" {
			inCaps = true
			continue
		}
		if !inCaps || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") {
			continue
		}
		name, _, found := strings.Cut(strings.TrimSpace(line), ":")
		if found && name != "" {
			yamlCaps = append(yamlCaps, name)
		}
	}
	sort.Strings(yamlCaps)
	known := KnownCapabilities()
	got := make([]string, len(known))
	for i, capability := range known {
		got[i] = string(capability)
	}
	if strings.Join(got, "\n") != strings.Join(yamlCaps, "\n") {
		t.Fatalf("Go capability registry drifted from connector-capabilities.yaml\nGo:\n%s\nYAML:\n%s", strings.Join(got, "\n"), strings.Join(yamlCaps, "\n"))
	}
}

func TestAccountAndSecretReferenceValidation(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	account := Account{ID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0011", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "synthetic-market", Family: FamilyMarketplace, Status: AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 2, Health: Health{Status: HealthHealthy, CheckedAt: now}, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := ValidateAccountAgainstManifest(account, validMarketplaceManifest()); err != nil {
		t.Fatal(err)
	}
	account.SecretReference = "Bearer plaintext"
	if err := account.Validate(); err == nil {
		t.Fatal("plaintext secret accepted as reference")
	}
}

func TestHealthRejectsRawTextAndRequiresUTC(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := (Health{Status: HealthUnavailable, ReasonCode: "upstream_timeout", CheckedAt: now}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Health{Status: HealthUnavailable, ReasonCode: "Bearer secret leaked", CheckedAt: now}).Validate(); err == nil {
		t.Fatal("unsafe health reason accepted")
	}
	if err := (Health{Status: HealthHealthy, ReasonCode: "ok", CheckedAt: now}).Validate(); err == nil {
		t.Fatal("healthy health with reason accepted")
	}
	local := now.In(time.FixedZone("plus3", 3*60*60))
	if err := (Health{Status: HealthUnknown, CheckedAt: local}).Validate(); err == nil {
		t.Fatal("non-UTC health timestamp accepted")
	}
}

func TestRemoteErrorIsSafeAndRetryPolicyBounded(t *testing.T) {
	remote, err := NewRemoteError(ErrorRateLimited, "quota_exceeded", "request-42", 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(remote.Error(), "request-42") {
		t.Fatalf("request id leaked into Error string: %s", remote.Error())
	}
	if !remote.Retryable() {
		t.Fatal("rate limit must be retryable")
	}
	policy := RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 250, MaxBackoffMS: 10000}
	if delay, ok := RetryDelay(policy, 1, remote); !ok || delay != 10*time.Second {
		t.Fatalf("retry-after must be capped by policy: %v %v", delay, ok)
	}
	transient, _ := NewRemoteError(ErrorTransient, "upstream_reset", "", 0)
	if delay, ok := RetryDelay(policy, 3, transient); !ok || delay != time.Second {
		t.Fatalf("expected 1s exponential delay, got %v %v", delay, ok)
	}
	unsafe, err := NewRemoteError(ErrorInternal, "Authorization:Bearer-x", "", 0)
	if err == nil || unsafe != nil {
		t.Fatal("unsafe remote error code accepted")
	}
}

type fakeConnector struct{ manifest Manifest }

func (f fakeConnector) Manifest() Manifest { return f.manifest }
func (f fakeConnector) Health(context.Context, Account, Runtime) (Health, error) {
	return Health{Status: HealthHealthy, CheckedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}, nil
}

func TestRegistryValidatesAndRejectsDuplicateConnectorID(t *testing.T) {
	connector := fakeConnector{manifest: validMarketplaceManifest()}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	_, manifest, err := registry.Connector("synthetic-market")
	if err != nil || !manifest.Supports("orders.read") {
		t.Fatalf("lookup failed: %#v %v", manifest, err)
	}
	duplicateMarker := ErrConnectorDuplicate
	if err := registry.Register(connector); !errors.Is(err, duplicateMarker) {
		t.Fatalf("duplicate accepted: %v", err)
	}
}

type memoryAccountRepository struct {
	account Account
}

func (repo *memoryAccountRepository) CreateAccount(_ context.Context, command AccountCreate, manifest Manifest) (Account, error) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	repo.account = Account{
		ID: command.ID, OrganizationID: command.OrganizationID, WorkspaceID: command.WorkspaceID,
		ConnectorID: manifest.ID, Family: manifest.Family, Status: AccountDisabled,
		SecretReference: command.SecretReference, Version: 1,
		Health: Health{Status: HealthUnknown}, CreatedAt: now, UpdatedAt: now,
	}
	return repo.account, nil
}
func (repo *memoryAccountRepository) AccountByID(_ context.Context, _, _, _ string) (Account, error) {
	if repo.account.ID == "" {
		return Account{}, ErrAccountNotFound
	}
	return repo.account, nil
}
func (repo *memoryAccountRepository) ChangeAccountStatus(_ context.Context, command AccountStatusChange) (Account, error) {
	if repo.account.Version != command.ExpectedVersion {
		return Account{}, ErrAccountConflict
	}
	repo.account.Status = command.Status
	repo.account.Version++
	repo.account.UpdatedAt = repo.account.UpdatedAt.Add(time.Second)
	return repo.account, nil
}
func (repo *memoryAccountRepository) RecordAccountHealth(_ context.Context, command AccountHealthUpdate) (Account, error) {
	if repo.account.Version != command.ExpectedVersion {
		return Account{}, ErrAccountConflict
	}
	repo.account.Health = command.Health
	repo.account.Version++
	repo.account.UpdatedAt = command.Health.CheckedAt
	return repo.account, nil
}

type failingHealthConnector struct {
	fakeConnector
	err error
}

func (f failingHealthConnector) Health(context.Context, Account, Runtime) (Health, error) {
	return Health{}, f.err
}

func TestManagerRequiresSecretAndNormalizesHealthFailure(t *testing.T) {
	manifest := validMarketplaceManifest()
	remote, err := NewRemoteError(ErrorUnavailable, "provider_unavailable", "rid-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	connector := failingHealthConnector{fakeConnector: fakeConnector{manifest: manifest}, err: remote}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryAccountRepository{}
	manager, err := NewManager(registry, repo)
	if err != nil {
		t.Fatal(err)
	}
	manager.clock = func() time.Time { return time.Date(2026, 8, 9, 12, 5, 0, 0, time.UTC) }
	command := AccountCreate{ID: "connector-a", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: manifest.ID}
	if _, err := manager.CreateAccount(context.Background(), command); !errors.Is(err, ErrSecretReference) {
		t.Fatalf("missing secret accepted: %v", err)
	}
	command.SecretReference = "sec:v1:0123456789abcdef0123456789abcdef"
	account, err := manager.CreateAccount(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if account.Status != AccountDisabled {
		t.Fatalf("new account status=%s", account.Status)
	}
	updated, err := manager.CheckHealth(context.Background(), command.OrganizationID, command.WorkspaceID, command.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Health.Status != HealthUnavailable || updated.Health.ReasonCode != "provider_unavailable" {
		t.Fatalf("normalized health=%#v", updated.Health)
	}
}

func TestManagerDiscardsUnnormalizedProviderErrorText(t *testing.T) {
	manifest := validMarketplaceManifest()
	connector := failingHealthConnector{fakeConnector: fakeConnector{manifest: manifest}, err: errors.New("Authorization: Bearer secret-value")}
	registry, _ := NewRegistry(connector)
	repo := &memoryAccountRepository{}
	manager, _ := NewManager(registry, repo)
	manager.clock = func() time.Time { return time.Date(2026, 8, 9, 12, 5, 0, 0, time.UTC) }
	command := AccountCreate{ID: "connector-a", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: manifest.ID, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef"}
	if _, err := manager.CreateAccount(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.CheckHealth(context.Background(), command.OrganizationID, command.WorkspaceID, command.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Health.ReasonCode != "connector_health_failed" || strings.Contains(updated.Health.ReasonCode, "secret") {
		t.Fatalf("unsafe health result=%#v", updated.Health)
	}
}
