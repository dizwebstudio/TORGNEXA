package mvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testTransport struct{ err error }

func (t testTransport) Ping(context.Context, []byte) error { return t.err }

type testRuntime struct{ secret []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{value: r.secret} }

type testSecrets struct{ value []byte }

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn(append([]byte(nil), s.value...))
}

func testAccount() sdk.Account {
	now := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "mvideo-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "mvideo", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now, UpdatedAt: now}
}

func TestManifestAndHealthBoundary(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	now := func() time.Time { return time.Date(2026, 8, 28, 1, 0, 0, 0, time.UTC) }
	health, err := New(testTransport{}, now).Health(context.Background(), testAccount(), testRuntime{secret: []byte("synthetic-key")})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("healthy=%+v err=%v", health, err)
	}
	health, err = New(testTransport{err: errors.New("network")}, now).Health(context.Background(), testAccount(), testRuntime{secret: []byte("synthetic-key")})
	if err != nil || health.Status != sdk.HealthUnavailable || health.ReasonCode != "provider_unavailable" {
		t.Fatalf("unavailable=%+v err=%v", health, err)
	}
	health, err = New(testTransport{}, now).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthUnavailable || health.ReasonCode != "provider_unavailable" {
		t.Fatalf("missing=%+v err=%v", health, err)
	}
}
