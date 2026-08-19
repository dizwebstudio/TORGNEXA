package opencart

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

func NewConformanceCandidate(exe string) (*ConformanceCandidate, error) {
	f, e := conformance.NewSandboxFixture(Manifest(), exe)
	if e != nil {
		return nil, e
	}
	return &ConformanceCandidate{SandboxFixture: f, connector: New(candidateTransport{}, candidateConfiguration{}, func() time.Time { return time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC) }), idempotency: map[string]string{}, webhooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "opencart-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: Manifest().ID, Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func (c *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime { return candidateRuntime{} }
func (c *ConformanceCandidate) Probe(_ context.Context, r conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch r.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		remote, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeRateLimited:
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "req-oc", time.Second)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeIdempotentWrite:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replayProbe(c.idempotency, r.IdempotencyKey, r.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replayProbe(c.webhooks, r.DeliveryID, r.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if r.Tenant != r.ResourceTenant {
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
	if fp, ok := store[key]; ok {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: fp}, nil
	}
	d := sha256.Sum256([]byte(scope + "\x00" + key))
	fp := hex.EncodeToString(d[:])
	store[key] = fp
	return conformance.ProbeResult{Applied: true, EffectFingerprint: fp}, nil
}

type candidateConfiguration struct{}

func (candidateConfiguration) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "USD"}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, r Request) (Response, error) {
	if len(r.Token) == 0 {
		return Response{StatusCode: 401}, nil
	}
	for _, q := range r.Query {
		if q.Name == "route" && q.Value == "extension/torgnexa/api/health" {
			return Response{StatusCode: 200, Body: []byte(`{"ok":true,"api_version":"v1"}`)}, nil
		}
	}
	return Response{StatusCode: 404}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	if cb == nil {
		return errors.New("callback missing")
	}
	v := []byte(`{"token":"oc_12345678901234567890123456789012"}`)
	defer clear(v)
	return cb(v)
}
