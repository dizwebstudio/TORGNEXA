package maxconnector

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
	connector := New(candidateTransport{}, candidateConfigSource{}, func() time.Time {
		return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	})
	return &ConformanceCandidate{
		SandboxFixture: fixture,
		connector:      connector,
		idempotency:    map[string]string{},
		webhooks:       map[string]string{},
	}, nil
}

func (candidate *ConformanceCandidate) Connector() sdk.Connector { return candidate.connector }

func (candidate *ConformanceCandidate) Account(tenant conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID:              "max-conformance",
		OrganizationID:  tenant.OrganizationID,
		WorkspaceID:     tenant.WorkspaceID,
		ConnectorID:     Manifest().ID,
		Family:          sdk.FamilySocial,
		Status:          sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef",
		Version:         1,
		Health:          sdk.Health{Status: sdk.HealthUnknown},
		CreatedAt:       at,
		UpdatedAt:       at,
	}
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
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "max-conformance", time.Second)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeIdempotentWrite:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return maxReplay(candidate.idempotency, request.IdempotencyKey, request.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return maxReplay(candidate.webhooks, request.DeliveryID, request.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if request.Tenant != request.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}

func maxReplay(store map[string]string, key, scope string) (conformance.ProbeResult, error) {
	if key == "" {
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
	if fingerprint, exists := store[key]; exists {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: fingerprint}, nil
	}
	digest := sha256.Sum256([]byte(scope + "\x00" + key))
	fingerprint := hex.EncodeToString(digest[:])
	store[key] = fingerprint
	return conformance.ProbeResult{Applied: true, EffectFingerprint: fingerprint}, nil
}

type candidateConfigSource struct{}

func (candidateConfigSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{ChatID: -70801090403050, WebhookSecretReference: "sec:v1:1123456789abcdef0123456789abcdef"}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, request Request) (Response, error) {
	if len(request.AccessToken) == 0 {
		return Response{StatusCode: 401}, nil
	}
	switch request.Path {
	case "/me":
		return Response{StatusCode: 200, Body: []byte(`{"user_id":229229229,"is_bot":true}`)}, nil
	case "/chats/-70801090403050":
		return Response{StatusCode: 200, Body: []byte(`{"chat_id":-70801090403050,"type":"channel","status":"active"}`)}, nil
	case "/chats/-70801090403050/members/me":
		return Response{StatusCode: 200, Body: []byte(`{"user_id":229229229,"is_bot":true,"is_admin":true,"permissions":["write"]}`)}, nil
	default:
		return Response{StatusCode: 404}, nil
	}
}

func (candidateTransport) Upload(context.Context, UploadRequest) (Response, error) {
	return Response{StatusCode: 404}, nil
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	if callback == nil {
		return errors.New("callback missing")
	}
	value := []byte("max-conformance-bot-token-ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	defer clear(value)
	return callback(value)
}
