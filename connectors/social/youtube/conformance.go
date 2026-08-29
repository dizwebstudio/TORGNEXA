package youtube

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
	fixture, err := conformance.NewSandboxFixture(Manifest(), exe)
	if err != nil {
		return nil, err
	}
	connector := New(candidateTransport{}, candidateConfig{}, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	return &ConformanceCandidate{SandboxFixture: fixture, connector: connector, idem: map[string]string{}, hooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "youtube-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: "youtube", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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
		return replayCandidate(c.idem, r.IdempotencyKey, r.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return replayCandidate(c.hooks, r.DeliveryID, r.Tenant.WorkspaceID)
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}
func replayCandidate(m map[string]string, key, scope string) (conformance.ProbeResult, error) {
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
	e, _ := sdk.NewRemoteError(cat, code, "youtube-conformance", retry)
	return e
}

type candidateConfig struct{}

func (candidateConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{ChannelID: "UCfixtureChannel00000001", CategoryID: "22", PrivacyStatus: "unlisted"}, nil
}

type candidateTransport struct{}

func (candidateTransport) ResolveOwnedChannel(_ context.Context, token []byte) (Channel, error) {
	if len(token) == 0 {
		return Channel{}, &GoogleFailure{Kind: FailureUnauthorized}
	}
	return Channel{ID: "UCfixtureChannel00000001", Title: "Fixture channel"}, nil
}
func (candidateTransport) StartResumableUpload(context.Context, []byte, string, UploadMetadata) (UploadSession, error) {
	return UploadSession{ID: "session_fixture_048"}, nil
}
func (candidateTransport) UploadChunk(_ context.Context, _ []byte, r UploadChunkRequest) (UploadProgress, error) {
	if len(r.Body) == 0 {
		return UploadProgress{}, errors.New("body missing")
	}
	next := r.Offset + int64(len(r.Body))
	if next < r.TotalBytes {
		return UploadProgress{NextOffset: next}, nil
	}
	return UploadProgress{NextOffset: r.TotalBytes, Complete: true, Video: VideoRecord{VideoID: "dQw4w9WgXcQ", ChannelID: "UCfixtureChannel00000001", UploadStatus: "uploaded", ProcessingStatus: "processing"}}, nil
}
func (candidateTransport) ProbeResumableUpload(context.Context, []byte, string, int64) (UploadProgress, error) {
	return UploadProgress{NextOffset: 0}, nil
}
func (candidateTransport) ReadVideo(context.Context, []byte, string) (VideoRecord, error) {
	return VideoRecord{VideoID: "dQw4w9WgXcQ", ChannelID: "UCfixtureChannel00000001", UploadStatus: "processed", ProcessingStatus: "succeeded"}, nil
}
func (candidateTransport) ListCommentThreads(context.Context, []byte, string, string, int) (CommentPage, error) {
	return CommentPage{}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	if cb == nil {
		return errors.New("callback missing")
	}
	b := []byte("youtube-oauth-conformance-token-0123456789")
	defer clear(b)
	return cb(b)
}
