package robokassa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/connectors/conformance"
	"sync"
	"time"
)

type ConformanceCandidate struct {
	*conformance.SandboxFixture
	connector *Connector
	mu        sync.Mutex
	idem      map[string]string
	hooks     map[string]string
}

func NewConformanceCandidate(exe string) (*ConformanceCandidate, error) {
	f, e := conformance.NewSandboxFixture(Manifest(), exe)
	if e != nil {
		return nil, e
	}
	return &ConformanceCandidate{SandboxFixture: f, connector: New(candidateTransport{}, func() time.Time { return time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC) }), idem: map[string]string{}, hooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "robokassa-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: "robokassa", Family: Manifest().Family, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func (c *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime { return candidateRuntime{} }
func (c *ConformanceCandidate) Probe(_ context.Context, r conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch r.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		return conformance.ProbeResult{}, remote(sdk.ErrorUnauthorized, "auth_rejected", 0)
	case conformance.ProbeRateLimited:
		return conformance.ProbeResult{}, remote(sdk.ErrorRateLimited, "rate_limited", time.Second)
	case conformance.ProbeTenantRead:
		if r.Tenant != r.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	case conformance.ProbeIdempotentWrite:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replay(c.idem, r.IdempotencyKey, r.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replay(c.hooks, r.DeliveryID, r.Tenant.WorkspaceID)
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}
func replay(m map[string]string, key, scope string) (conformance.ProbeResult, error) {
	if key == "" {
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
	if f, ok := m[key]; ok {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: f}, nil
	}
	h := sha256.Sum256([]byte(scope + "\x00" + key))
	f := hex.EncodeToString(h[:])
	m[key] = f
	return conformance.ProbeResult{Applied: true, EffectFingerprint: f}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	if fn == nil {
		return errors.New("callback missing")
	}
	v := []byte("synthetic-credential")
	defer clear(v)
	return fn(v)
}
