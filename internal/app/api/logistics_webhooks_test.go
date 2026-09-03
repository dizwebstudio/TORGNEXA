package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type fakeLogisticsWebhookRepository struct {
	shipment logistics.Shipment
	evidence map[string]bool
}

func (f *fakeLogisticsWebhookRepository) ShipmentByRemoteID(_ context.Context, _ tenancy.Scope, accountID, remoteID string) (logistics.Shipment, error) {
	if f.shipment.AccountID != accountID || f.shipment.RemoteID != remoteID {
		return logistics.Shipment{}, logistics.ErrNotFound
	}
	return f.shipment, nil
}

func (f *fakeLogisticsWebhookRepository) RecordWebhookEvidence(_ context.Context, _ tenancy.Scope, evidence logistics.WebhookEvidence) (bool, error) {
	if f.evidence == nil {
		f.evidence = make(map[string]bool)
	}
	if f.evidence[evidence.DeliveryID] {
		return false, nil
	}
	f.evidence[evidence.DeliveryID] = true
	return true, nil
}

type fakeLogisticsWebhookAccounts struct{ account sdk.Account }

func (f fakeLogisticsWebhookAccounts) AccountByID(_ context.Context, _, _, accountID string) (sdk.Account, error) {
	if accountID != f.account.ID {
		return sdk.Account{}, sdk.ErrAccountNotFound
	}
	return f.account, nil
}

type fakeLogisticsWebhookResolver struct {
	result sdk.LogisticsWebhook
	err    error
}

func (f fakeLogisticsWebhookResolver) LogisticsWebhook(context.Context, sdk.Account, sdk.Runtime, []byte, []byte) (sdk.LogisticsWebhook, error) {
	if f.err != nil {
		return sdk.LogisticsWebhook{}, f.err
	}
	return f.result, nil
}

func TestLogisticsWebhookRecordsVerifiedEvidenceOnce(t *testing.T) {
	now := time.Now().UTC()
	account := sdk.Account{ID: "logistics-account", ConnectorID: "logistics-a", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, CreatedAt: now, UpdatedAt: now, Health: sdk.Health{Status: sdk.HealthUnknown}}
	shipment := logistics.Shipment{ID: "shipment-1", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", AccountID: account.ID, ExternalID: "order-1", RemoteID: "1100285492", ServiceCode: "cdek_tariff_136", Status: logistics.StatusCreated, Currency: "RUB", Version: 1, UpdatedAt: now}
	repository := &fakeLogisticsWebhookRepository{shipment: shipment}
	resolver := fakeLogisticsWebhookResolver{result: sdk.LogisticsWebhook{DeliveryID: "82753031-1820-4f99-9240-aab139f05ca5", RemoteID: shipment.RemoteID, Status: "DELIVERED", OccurredAt: now}}
	api := logisticsWebhookAPI{repository: repository, accounts: fakeLogisticsWebhookAccounts{account: account}, secrets: fakeWebhookSecrets{}, registry: resolver}
	path := logisticsWebhooksPathPrefix + "logistics-a/" + shipment.OrganizationID + "/" + shipment.WorkspaceID + "/" + account.ID
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(`{"type":"ORDER_STATUS"}`))
		rr := httptest.NewRecorder()
		api.receive(rr, req)
		if rr.Code != http.StatusOK || rr.Body.String() != "{}" {
			t.Fatalf("delivery %d response=%d %q", i, rr.Code, rr.Body.String())
		}
	}
	if len(repository.evidence) != 1 {
		t.Fatalf("expected one deduplicated evidence row, got %d", len(repository.evidence))
	}
}

func TestLogisticsWebhookDoesNotRecordWhenProviderVerificationFails(t *testing.T) {
	now := time.Now().UTC()
	account := sdk.Account{ID: "logistics-account", ConnectorID: "logistics-a", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, CreatedAt: now, UpdatedAt: now, Health: sdk.Health{Status: sdk.HealthUnknown}}
	repository := &fakeLogisticsWebhookRepository{evidence: map[string]bool{}}
	api := logisticsWebhookAPI{repository: repository, accounts: fakeLogisticsWebhookAccounts{account: account}, secrets: fakeWebhookSecrets{}, registry: fakeLogisticsWebhookResolver{err: errors.New("verification failed")}}
	path := logisticsWebhooksPathPrefix + "logistics-a/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001/018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002/" + account.ID
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(`{"type":"ORDER_STATUS"}`))
	rr := httptest.NewRecorder()
	api.receive(rr, req)
	if rr.Code != http.StatusOK || len(repository.evidence) != 0 {
		t.Fatalf("verification failure response=%d evidence=%d", rr.Code, len(repository.evidence))
	}
}

func TestParseLogisticsWebhookPath(t *testing.T) {
	connectorID, orgID, workspaceID, accountID, ok := parseLogisticsWebhookPath(logisticsWebhooksPathPrefix + "logistics-a/org-1/ws-1/account-1")
	parsed := strings.Join([]string{connectorID, orgID, workspaceID, accountID}, "/")
	if !ok || parsed != "logistics-a/org-1/ws-1/account-1" {
		t.Fatalf("parse = %q %q %q %q %v", connectorID, orgID, workspaceID, accountID, ok)
	}
	for _, bad := range []string{
		logisticsWebhooksPathPrefix + "logistics-a/org-1/ws-1",
		logisticsWebhooksPathPrefix + "logistics-a/org-1/ws-1/account-1/extra",
		webhookPathPrefix + "payments/logistics-a/org-1/ws-1/account-1",
	} {
		if _, _, _, _, ok := parseLogisticsWebhookPath(bad); ok {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}

var _ secrets.SecretProvider = fakeWebhookSecrets{}
