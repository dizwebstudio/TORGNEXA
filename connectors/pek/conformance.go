package pek

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

// ConformanceCandidate exposes the deterministic ПЭК adapter to the SDK
// conformance harness without production credentials or network access.
type ConformanceCandidate struct {
	*conformance.SandboxFixture
	connector *Connector
	mu        sync.Mutex
	idem      map[string]string
	hooks     map[string]string
}

// NewConformanceCandidate creates a sandbox-only candidate.
func NewConformanceCandidate(executable string) (*ConformanceCandidate, error) {
	fixture, err := conformance.NewSandboxFixture(Manifest(), executable)
	if err != nil {
		return nil, err
	}
	return &ConformanceCandidate{SandboxFixture: fixture, connector: New(candidateTransport{}, func() time.Time { return candidateTime }), idem: map[string]string{}, hooks: map[string]string{}}, nil
}

// Connector returns the candidate adapter.
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }

// Account returns a tenant-scoped synthetic account.
func (c *ConformanceCandidate) Account(tenant conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "pek-conformance", OrganizationID: tenant.OrganizationID, WorkspaceID: tenant.WorkspaceID, ConnectorID: "pek", Family: Manifest().Family, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

// Runtime returns a synthetic callback-scoped secret accessor.
func (c *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime { return candidateRuntime{} }

// Probe provides deterministic conformance behavior without remote side effects.
func (c *ConformanceCandidate) Probe(_ context.Context, request conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch request.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		return conformance.ProbeResult{}, remote(sdk.ErrorUnauthorized, "auth_rejected", 0)
	case conformance.ProbeRateLimited:
		return conformance.ProbeResult{}, remote(sdk.ErrorRateLimited, "rate_limited", time.Second)
	case conformance.ProbeTenantRead:
		if request.Tenant != request.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	case conformance.ProbeIdempotentWrite:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replay(c.idem, request.IdempotencyKey, request.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replay(c.hooks, request.DeliveryID, request.Tenant.WorkspaceID)
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}

func replay(values map[string]string, key, scope string) (conformance.ProbeResult, error) {
	if key == "" {
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
	if fingerprint, ok := values[key]; ok {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: fingerprint}, nil
	}
	digest := sha256.Sum256([]byte(scope + "\x00" + key))
	fingerprint := hex.EncodeToString(digest[:])
	values[key] = fingerprint
	return conformance.ProbeResult{Applied: true, EffectFingerprint: fingerprint}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	if callback == nil {
		return errors.New("callback missing")
	}
	secret := []byte(`{"username":"synthetic-user","password":"synthetic-key"}`)
	defer clear(secret)
	return callback(secret)
}
