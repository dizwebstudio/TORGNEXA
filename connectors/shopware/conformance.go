package shopware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/connectors/conformance"
)

type ConformanceCandidate struct {
	*conformance.SandboxFixture
	connector   *Connector
	mu          sync.Mutex
	idempotency map[string]string
	webhooks    map[string]string
}

func NewConformanceCandidate(emulatorExecutable string) (*ConformanceCandidate, error) {
	fixture, err := conformance.NewSandboxFixture(Manifest(), emulatorExecutable)
	if err != nil {
		return nil, err
	}
	return &ConformanceCandidate{SandboxFixture: fixture, connector: New(candidateTransport{}, candidateConfiguration{}, func() time.Time { return time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC) }), idempotency: map[string]string{}, webhooks: map[string]string{}}, nil
}
func (candidate *ConformanceCandidate) Connector() sdk.Connector { return candidate.connector }
func (candidate *ConformanceCandidate) Account(tenant conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "shopware-conformance", OrganizationID: tenant.OrganizationID, WorkspaceID: tenant.WorkspaceID, ConnectorID: Manifest().ID, Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func (candidate *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime {
	return candidateRuntime{}
}
func (candidate *ConformanceCandidate) Probe(_ context.Context, request conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch request.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		remote, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeRateLimited:
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "req-shopware", time.Second)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeIdempotentWrite:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return replayProbe(candidate.idempotency, request.IdempotencyKey, request.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return replayProbe(candidate.webhooks, request.DeliveryID, request.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if request.Tenant != request.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}
func replayProbe(store map[string]string, key, scope string) (conformance.ProbeResult, error) {
	if key == "" {
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
	if fingerprint, ok := store[key]; ok {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: fingerprint}, nil
	}
	digest := sha256.Sum256([]byte(scope + "\x00" + key))
	fingerprint := hex.EncodeToString(digest[:])
	store[key] = fingerprint
	return conformance.ProbeResult{Applied: true, EffectFingerprint: fingerprint}, nil
}

type candidateConfiguration struct{}

func (candidateConfiguration) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "USD"}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, request Request) (Response, error) {
	if request.Method == "POST" && request.Path == "/api/oauth/token" {
		return Response{StatusCode: 200, Body: []byte(`{"access_token":"candidate-token","expires_in":600}`)}, nil
	}
	if request.Headers["Authorization"] != "Bearer candidate-token" {
		return Response{StatusCode: 401}, nil
	}
	if request.Method == "POST" && request.Path == "/api/search/product" {
		return Response{StatusCode: 200, Body: []byte(`{"data":[],"total":0}`)}, nil
	}
	return Response{StatusCode: 404}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	if callback == nil {
		return errors.New("callback missing")
	}
	value := []byte(`{"client_id":"SWIACandidateClientId0","client_secret":"candidate-client-secret-value"}`)
	defer clear(value)
	return callback(value)
}
