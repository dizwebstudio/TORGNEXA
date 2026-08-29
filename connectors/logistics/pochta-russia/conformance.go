package pochtarussia

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

// ConformanceCandidate exposes the deterministic Russian Post adapter to the
// SDK conformance harness without production credentials or network access.
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
	return &ConformanceCandidate{
		SandboxFixture: fixture,
		connector:      New(candidateTransport{}, func() time.Time { return time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC) }),
		idem:           map[string]string{},
		hooks:          map[string]string{},
	}, nil
}

// Connector returns the candidate adapter.
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }

// Account returns a tenant-scoped synthetic account.
func (c *ConformanceCandidate) Account(tenant conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "pochta-russia-conformance", OrganizationID: tenant.OrganizationID, WorkspaceID: tenant.WorkspaceID,
		ConnectorID: "pochta-russia", Family: Manifest().Family, Status: sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1,
		Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

// Runtime returns a synthetic callback-scoped secret accessor.
func (c *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime { return candidateRuntime{} }

// Probe provides deterministic conformance behavior without remote side effects.
func (c *ConformanceCandidate) Probe(_ context.Context, request conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch request.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		return conformance.ProbeResult{}, remote(sdk.ErrorUnauthorized, "auth_rejected")
	case conformance.ProbeRateLimited:
		err, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "", time.Second)
		return conformance.ProbeResult{}, err
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
	secret := []byte(`{"token":"synthetic-token","key":"c3ludGhldGljLXVzZXI6cGFzc3dvcmQ="}`)
	defer clear(secret)
	return callback(secret)
}
