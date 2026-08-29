package rutube

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
	fixture, err := conformance.NewSandboxFixture(Manifest(), exe)
	if err != nil {
		return nil, err
	}
	connector := New(candidateTransport{}, candidateConfig{}, func() time.Time { return time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC) })
	return &ConformanceCandidate{SandboxFixture: fixture, connector: connector, idem: map[string]string{}, hooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "rutube-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: "rutube", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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
	sum := sha256.Sum256([]byte(scope + "\x00" + key))
	f := hex.EncodeToString(sum[:])
	m[key] = f
	return conformance.ProbeResult{Applied: true, EffectFingerprint: f}, nil
}
func candidateRemote(cat sdk.ErrorCategory, code string, retry time.Duration) error {
	e, _ := sdk.NewRemoteError(cat, code, "rutube-conformance", retry)
	return e
}

type candidateConfig struct{}

func (candidateConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{ChannelID: "channel_fixture_001", ContractID: "partner-contract-v1"}, nil
}

type candidateTransport struct{}

func (candidateTransport) ResolveChannel(_ context.Context, token []byte, contractID, channelID string) (Channel, error) {
	if len(token) == 0 || contractID == "" {
		return Channel{}, &PartnerFailure{Kind: FailureUnauthorized}
	}
	return Channel{ID: channelID, Name: "Fixture channel"}, nil
}
func (candidateTransport) CreateUpload(context.Context, []byte, CreateUploadRequest) (UploadSession, error) {
	return UploadSession{ID: "session_fixture_001", MaxBytes: 1 << 30, ExpiresAt: time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Upload(_ context.Context, _ []byte, r UploadRequest) error {
	if r.Body == nil {
		return errors.New("body missing")
	}
	_, _ = io.Copy(io.Discard, r.Body)
	return nil
}
func (candidateTransport) CommitUpload(context.Context, []byte, CommitUploadRequest) (VideoRecord, error) {
	return VideoRecord{VideoID: "video_fixture_001", ChannelID: "channel_fixture_001", State: VideoStateProcessing}, nil
}
func (candidateTransport) ReadVideo(context.Context, []byte, VideoStatusRequest) (VideoRecord, error) {
	return VideoRecord{VideoID: "video_fixture_001", ChannelID: "channel_fixture_001", State: VideoStatePublished}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	if cb == nil {
		return errors.New("callback missing")
	}
	b := []byte("rutube-partner-conformance-credential-0123456789")
	defer clear(b)
	return cb(b)
}
