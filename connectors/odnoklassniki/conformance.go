package ok

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
	c := New(candidateTransport{}, candidateConfig{}, func() time.Time { return time.Date(2026, 8, 12, 0, 30, 0, 0, time.UTC) })
	return &ConformanceCandidate{SandboxFixture: f, connector: c, idem: map[string]string{}, hooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "odnoklassniki-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: "odnoklassniki", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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
	e, _ := sdk.NewRemoteError(cat, code, "odnoklassniki-conformance", retry)
	return e
}

type candidateConfig struct{}

func (candidateConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{GroupID: "70000000000001", ApplicationKey: "CBAOK.synthetic.public.key", AppSecretReference: "sec:v1:abcdef0123456789abcdef0123456789"}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, r Request) (Response, error) {
	if len(r.AccessToken) == 0 {
		return Response{StatusCode: 401}, nil
	}
	if r.Host == apiHost && r.APIMethod == "group.getInfo" {
		return Response{StatusCode: 200, Body: []byte(`[{"uid":"70000000000001","name":"fixture"}]`)}, nil
	}
	return Response{StatusCode: 404}, nil
}
func (candidateTransport) Upload(context.Context, UploadRequest) (Response, error) {
	return Response{StatusCode: 404}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, ref sdk.SecretReference, cb func([]byte) error) error {
	if cb == nil {
		return errors.New("callback missing")
	}
	var b []byte
	if ref == "sec:v1:abcdef0123456789abcdef0123456789" {
		b = []byte("ok-app-secret-conformance-0123456789abcdef")
	} else {
		b = []byte("ok-oauth-token-conformance-0123456789abcdef")
	}
	defer clear(b)
	return cb(b)
}
