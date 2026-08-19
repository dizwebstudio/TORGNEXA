package moysklad

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
	return &ConformanceCandidate{SandboxFixture: fixture, connector: New(candidateTransport{}, func() time.Time { return time.Date(2026, 8, 10, 17, 30, 0, 0, time.UTC) }), idempotency: map[string]string{}, webhooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "moysklad-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: Manifest().ID, Family: sdk.FamilyERP, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
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
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "req-ms-conformance", 750*time.Millisecond)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeIdempotentWrite:
		c.mu.Lock()
		defer c.mu.Unlock()
		return msReplay(c.idempotency, r.IdempotencyKey, r.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return msReplay(c.webhooks, r.DeliveryID, r.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if r.Tenant != r.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}
func msReplay(store map[string]string, key, scope string) (conformance.ProbeResult, error) {
	if key == "" {
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
	if f, ok := store[key]; ok {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: f}, nil
	}
	d := sha256.Sum256([]byte(scope + "\x00" + key))
	f := hex.EncodeToString(d[:])
	store[key] = f
	return conformance.ProbeResult{Applied: true, EffectFingerprint: f}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, r Request) (Response, error) {
	if len(r.Token) == 0 {
		return Response{StatusCode: 401}, nil
	}
	if r.Method == "GET" && r.Host == apiHost && r.Path == apiBasePath+"/entity/assortment" {
		return Response{StatusCode: 200, Body: []byte(`{"meta":{"size":0,"limit":1,"offset":0},"rows":[]}`)}, nil
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
	value := []byte("synthetic-moysklad-token-0123456789")
	defer clear(value)
	return callback(value)
}
