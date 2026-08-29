package pochtarussia

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	return callback([]byte(`{"token":"synthetic-token","key":"c3ludGhldGljLXVzZXI6cGFzc3dvcmQ="}`))
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "pochta-russia-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: "pochta-russia", Family: sdk.FamilyLogistics, Status: sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1,
		Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func TestHealthUsesCandidateTransport(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	health, err := New(candidateTransport{}, func() time.Time { return now }).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthHealthy || !health.CheckedAt.Equal(now) {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}

type rejectingSecrets struct{}

func (rejectingSecrets) UseSecret(context.Context, sdk.SecretReference, func([]byte) error) error {
	return errors.New("secret provider unavailable")
}

type rejectingRuntime struct{}

func (rejectingRuntime) Secrets() sdk.SecretAccessor { return rejectingSecrets{} }

func TestHealthPropagatesSecretProviderFailure(t *testing.T) {
	_, err := New(candidateTransport{}, nil).Health(context.Background(), testAccount(), rejectingRuntime{})
	if err == nil || err.Error() != "secret provider unavailable" {
		t.Fatalf("expected secret provider failure, got %v", err)
	}
}
