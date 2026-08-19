package telegram

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
	connector := New(candidateTransport{}, candidateConfigSource{}, func() time.Time { return time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC) })
	return &ConformanceCandidate{SandboxFixture: fixture, connector: connector, idempotency: map[string]string{}, webhooks: map[string]string{}}, nil
}
func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	at := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "telegram-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: Manifest().ID, Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func (c *ConformanceCandidate) Runtime(conformance.Tenant) sdk.Runtime { return candidateRuntime{} }
func (c *ConformanceCandidate) Probe(_ context.Context, r conformance.ProbeRequest) (conformance.ProbeResult, error) {
	switch r.Kind {
	case conformance.ProbeAuthValid:
		return conformance.ProbeResult{}, nil
	case conformance.ProbeAuthInvalid:
		e, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		return conformance.ProbeResult{}, e
	case conformance.ProbeRateLimited:
		e, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "flood_control", "telegram-conformance", time.Second)
		return conformance.ProbeResult{}, e
	case conformance.ProbeIdempotentWrite:
		c.mu.Lock()
		defer c.mu.Unlock()
		return telegramReplay(c.idempotency, r.IdempotencyKey, r.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return telegramReplay(c.webhooks, r.DeliveryID, r.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if r.Tenant != r.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}
func telegramReplay(store map[string]string, key, scope string) (conformance.ProbeResult, error) {
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

type candidateConfigSource struct{}

func (candidateConfigSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{ChatID: -1001234567890}, nil
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, r Request) (Response, error) {
	if len(r.BotToken) == 0 {
		return Response{StatusCode: 401}, nil
	}
	switch r.APIMethod {
	case "getMe":
		return Response{StatusCode: 200, Body: []byte(`{"ok":true,"result":{"id":777000111,"is_bot":true}}`)}, nil
	case "getChatMember":
		return Response{StatusCode: 200, Body: []byte(`{"ok":true,"result":{"status":"administrator","can_post_messages":true}}`)}, nil
	default:
		return Response{StatusCode: 404}, nil
	}
}

type candidateRuntime struct{}

func (candidateRuntime) Secrets() sdk.SecretAccessor { return candidateSecrets{} }

type candidateSecrets struct{}

func (candidateSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	if cb == nil {
		return errors.New("callback missing")
	}
	v := []byte("777000111:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdef123456")
	defer clear(v)
	return cb(v)
}
