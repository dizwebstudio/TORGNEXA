package worker

import (
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/integrationcenterrepo"
)

func TestReduceWorkerAccountDoesNotInferCredentialFromSecretReference(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	account := sdk.Account{
		ID:              "account-1",
		ConnectorID:     "test-storefront",
		Family:          sdk.FamilyStorefront,
		Status:          sdk.AccountActive,
		SecretReference: sdk.SecretReference("sec:v1:0123456789abcdef0123456789abcdef"),
		Version:         1,
		UpdatedAt:       now,
		Health:          sdk.Health{Status: sdk.HealthUnknown},
	}
	snapshot, err := reduceWorkerAccount(account, integrationcenterrepo.QueueItem{AccountID: account.ID, EventID: "evt-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Dimensions.Credential.Status; got != "unknown" {
		t.Fatalf("credential status=%s, secret reference must not imply validity", got)
	}

	account.Health = sdk.Health{Status: sdk.HealthHealthy, CheckedAt: now}
	snapshot, err = reduceWorkerAccount(account, integrationcenterrepo.QueueItem{AccountID: account.ID, EventID: "evt-2"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Dimensions.Credential.Status; got != "present" {
		t.Fatalf("healthy auth evidence credential status=%s", got)
	}
}

func TestIntegrationCenterErrorCodeNeverCopiesProviderError(t *testing.T) {
	code := integrationCenterErrorCode(assertionError("provider token=do-not-log"))
	if code != "recompute_failed" {
		t.Fatalf("error code=%q", code)
	}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
