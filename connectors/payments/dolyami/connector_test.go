package dolyami

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testTransport struct{ err error }

func (t testTransport) Ping(context.Context, Configuration, []byte) error { return t.err }

type testConfig struct {
	value Configuration
	err   error
}

func (s testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return s.value, s.err
}

type testRuntime struct{ secret []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{value: r.secret} }

type testSecrets struct{ value []byte }

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn(append([]byte(nil), s.value...))
}

func testAccount() sdk.Account {
	now := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "dolyami-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "dolyami", Family: sdk.FamilyPayment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now, UpdatedAt: now}
}

func TestManifestAndHealthBoundary(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	now := func() time.Time { return time.Date(2026, 8, 28, 1, 0, 0, 0, time.UTC) }
	configuration := Configuration{ProbeURL: "https://api.example.test/health"}
	health, err := New(testTransport{}, testConfig{value: configuration}, now).Health(context.Background(), testAccount(), testRuntime{secret: []byte(`{"username":"demo","password":"secret"}`)})
	if err != nil || health.Status != sdk.HealthHealthy || !health.CheckedAt.Equal(now()) {
		t.Fatalf("healthy=%+v err=%v", health, err)
	}
	health, err = New(testTransport{err: errors.New("network")}, testConfig{value: configuration}, now).Health(context.Background(), testAccount(), testRuntime{secret: []byte("credential")})
	if err != nil || health.Status != sdk.HealthUnavailable || health.ReasonCode != "provider_unavailable" {
		t.Fatalf("unavailable=%+v err=%v", health, err)
	}
	health, err = New(testTransport{}, testConfig{value: Configuration{}}, now).Health(context.Background(), testAccount(), testRuntime{secret: []byte("credential")})
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "configuration_invalid" {
		t.Fatalf("invalid config=%+v err=%v", health, err)
	}
}
