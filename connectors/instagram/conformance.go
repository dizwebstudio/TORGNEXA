package instagram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/connectors/conformance"
)

type ConformanceCandidate struct {
	*conformance.SandboxFixture
	connector *Connector
	mu        sync.Mutex
	idem      map[string]string
	hooks     map[string]string
}

func NewConformanceCandidate(exe string) (*ConformanceCandidate, error) {
	f, err := conformance.NewSandboxFixture(Manifest(), exe)
	if err != nil {
		return nil, err
	}
	c := New(candidateTransport{}, candidateConfig{}, candidateStager{}, func() time.Time { return time.Date(2026, 8, 11, 21, 30, 0, 0, time.UTC) })
	c.wait = func(context.Context, time.Duration) error { return nil }
	return &ConformanceCandidate{SandboxFixture: f, connector: c, idem: map[string]string{}, hooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 11, 21, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "instagram-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: "instagram", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func (c *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime { return candidateRuntime{} }
func (c *ConformanceCandidate) Probe(_ context.Context, r conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch r.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		return conformance.ProbeResult{}, candidateRemote(sdk.ErrorUnauthorized, "auth_rejected", 0)
	case conformance.ProbeRateLimited:
		return conformance.ProbeResult{}, candidateRemote(sdk.ErrorRateLimited, "rate_limited", time.Second)
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
func candidateRemote(cat sdk.ErrorCategory, code string, retry time.Duration) error {
	e, _ := sdk.NewRemoteError(cat, code, "instagram-conformance", retry)
	return e
}

type candidateConfig struct{}

func (candidateConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{InstagramUserID: "17841400000000001"}, nil
}

type candidateStager struct{}

func (candidateStager) Stage(_ context.Context, _ sdk.Account, _ sdk.SocialMediaRef, _ sdk.MediaDescriptor, _ io.Reader) (StagedMedia, error) {
	return StagedMedia{URL: "https://media.synthetic.example/item.jpg", ExpiresAt: time.Date(2026, 8, 11, 22, 30, 0, 0, time.UTC)}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, r Request) (Response, error) {
	if len(r.AccessToken) == 0 {
		return Response{StatusCode: 401}, nil
	}
	if r.Method == "GET" && r.Path == "/v26.0/17841400000000001" {
		return Response{StatusCode: 200, Body: []byte(`{"id":"17841400000000001","username":"fixture","account_type":"BUSINESS"}`)}, nil
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
	b := []byte("IGQVJ-conformance-token-0123456789abcdef")
	defer clear(b)
	return cb(b)
}
