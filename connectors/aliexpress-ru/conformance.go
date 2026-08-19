package aliexpressru

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
	return &ConformanceCandidate{
		SandboxFixture: fixture,
		connector:      New(candidateTransport{}, func() time.Time { return time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC) }),
		idempotency:    map[string]string{}, webhooks: map[string]string{},
	}, nil
}

func (candidate *ConformanceCandidate) Connector() sdk.Connector { return candidate.connector }
func (candidate *ConformanceCandidate) Account(tenant conformance.Tenant) sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "aliexpress-ru-conformance", OrganizationID: tenant.OrganizationID, WorkspaceID: tenant.WorkspaceID, ConnectorID: Manifest().ID, Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
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
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "ali-ru-conformance", 1500*time.Millisecond)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeIdempotentWrite:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return conformanceReplay(candidate.idempotency, request.IdempotencyKey, request.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return conformanceReplay(candidate.webhooks, request.DeliveryID, request.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if request.Tenant != request.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}

func conformanceReplay(store map[string]string, key, scope string) (conformance.ProbeResult, error) {
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

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, request Request) (Response, error) {
	if len(request.XAuthToken) == 0 {
		return Response{StatusCode: 401}, nil
	}
	if request.Method == "POST" && request.Host == apiHost && request.Path == productsPath {
		return Response{StatusCode: 200, Body: []byte(`{"data":[{"id":"10001","ali_updated_at":"2026-08-10T18:00:00Z","subject":"Conformance product","sku":[{"id":"sku-internal-1","sku_id":"sku-1","code":"SELLER-1"}]}],"error":null}`)}, nil
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
	value := []byte("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzeW50aGV0aWMifQ.c2lnbmF0dXJl")
	defer clear(value)
	return callback(value)
}
