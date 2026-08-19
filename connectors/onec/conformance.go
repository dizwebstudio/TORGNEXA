package onec

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
	connector := New(candidateTransport{}, candidateConfigSource{}, func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) })
	return &ConformanceCandidate{SandboxFixture: fixture, connector: connector, idempotency: map[string]string{}, webhooks: map[string]string{}}, nil
}

func (c *ConformanceCandidate) Connector() sdk.Connector { return c.connector }
func (c *ConformanceCandidate) Account(t conformance.Tenant) sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "onec-conformance", OrganizationID: t.OrganizationID, WorkspaceID: t.WorkspaceID, ConnectorID: Manifest().ID, Family: sdk.FamilyERP, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
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
		remote, _ := sdk.NewRemoteError(sdk.ErrorRateLimited, "rate_limited", "req-onec-conformance", 750*time.Millisecond)
		return conformance.ProbeResult{}, remote
	case conformance.ProbeIdempotentWrite:
		c.mu.Lock()
		defer c.mu.Unlock()
		return conformanceReplay(c.idempotency, r.IdempotencyKey, r.Tenant.OrganizationID)
	case conformance.ProbeWebhook:
		c.mu.Lock()
		defer c.mu.Unlock()
		return conformanceReplay(c.webhooks, r.DeliveryID, r.Tenant.WorkspaceID)
	case conformance.ProbeTenantRead:
		if r.Tenant != r.ResourceTenant {
			return conformance.ProbeResult{}, conformance.ErrTenantDenied
		}
		return conformance.ProbeResult{}, nil
	default:
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
}

func conformanceReplay(store map[string]string, key, scope string) (conformance.ProbeResult, error) {
	if key == "" {
		return conformance.ProbeResult{}, conformance.ErrInvalidCandidate
	}
	if fingerprint, ok := store[key]; ok {
		return conformance.ProbeResult{Duplicate: true, EffectFingerprint: fingerprint}, nil
	}
	digest := sha256.Sum256([]byte(scope + "\x00" + key))
	fingerprint := hex.EncodeToString(digest[:])
	store[key] = fingerprint
	return conformance.ProbeResult{Applied: true, EffectFingerprint: fingerprint}, nil
}

type candidateConfigSource struct{}

func (candidateConfigSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return testConfigurationForConformance(), nil
}
func testConfigurationForConformance() Configuration {
	return Configuration{Host: "erp.example.test", BasePath: "/demo/odata/standard.odata",
		Catalog:   CatalogMapping{Resource: "Catalog_Номенклатура", IDField: "Ref_Key", CodeField: "Code", SKUField: "Артикул", TitleField: "Description", BrandField: "Бренд", RevisionField: "DataVersion", ArchivedField: "DeletionMark"},
		Inventory: InventoryMapping{Resource: "AccumulationRegister_ТоварыНаСкладах", Function: "Balance", ProductField: "Номенклатура_Key", LocationField: "Склад_Key", QuantityField: "КоличествоBalance"}}
}

type candidateTransport struct{}

func (candidateTransport) Do(_ context.Context, request Request) (Response, error) {
	if len(request.Username) == 0 || len(request.Password) == 0 {
		return Response{StatusCode: 401}, nil
	}
	if request.Method == "GET" && request.Host == "erp.example.test" && request.Path == "/demo/odata/standard.odata/$metadata" {
		return Response{StatusCode: 200, Body: []byte("<edmx:Edmx>synthetic</edmx:Edmx>")}, nil
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
	value := []byte("sync_reader\nsynthetic-password-012345")
	defer clear(value)
	return callback(value)
}
