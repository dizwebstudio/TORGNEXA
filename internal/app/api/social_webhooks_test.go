package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
)

type socialWebhookAccountStub struct {
	account  sdk.Account
	settings []sdk.AccountCapabilitySetting
}

func (stub socialWebhookAccountStub) AccountByID(_ context.Context, _, _, accountID string) (sdk.Account, error) {
	if accountID != stub.account.ID {
		return sdk.Account{}, sdk.ErrAccountNotFound
	}
	return stub.account, nil
}

func (stub socialWebhookAccountStub) AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error) {
	return stub.settings, nil
}

type socialWebhookConfigStub struct{}

func (socialWebhookConfigStub) Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error) {
	return json.RawMessage(`{"chat_id":-70801090403050,"webhook_secret_reference":"sec:v1:1123456789abcdef0123456789abcdef"}`), 1, nil
}

type socialWebhookReceiverStub struct {
	called  bool
	request sdk.SocialWebhookRequest
}

func (stub *socialWebhookReceiverStub) ReceiveSocialWebhook(_ context.Context, _ sdk.Account, _ sdk.Runtime, request sdk.SocialWebhookRequest, _ sdk.SocialWebhookDeduplicator) (sdk.SocialWebhookResult, error) {
	stub.called = true
	stub.request = request
	return sdk.SocialWebhookResult{
		DeliveryID:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EventType:        "max.message_created",
		RemoteChannelID:  "-70801090403050",
		OccurredAt:       request.ReceivedAt,
		CanonicalPayload: request.Body,
	}, nil
}

type socialWebhookResolverStub struct {
	receiver sdk.SocialWebhookReceiver
}

func (stub socialWebhookResolverStub) SocialWebhookReceiver(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialWebhookReceiver, error) {
	return stub.receiver, nil
}

func socialWebhookTestAccount() sdk.Account {
	at := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "max-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: "max-messenger", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef",
		Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func socialWebhookTestSettings() []sdk.AccountCapabilitySetting {
	return []sdk.AccountCapabilitySetting{{Capability: "social.webhooks", Direction: sdk.CapabilityRead, Risk: sdk.CapabilityRiskRead, Enabled: true}}
}

func TestSocialWebhookRouteIsPublicAndUsesContractPrefix(t *testing.T) {
	processor, err := inboxrepo.New(&sql.DB{})
	if err != nil {
		t.Fatal(err)
	}
	routes := newSocialWebhookRoutes(socialWebhookAccountStub{}, socialWebhookConfigStub{}, fakeWebhookSecrets{}, socialWebhookResolverStub{}, processor)
	if len(routes) != 1 {
		t.Fatalf("routes=%d, want one public social webhook route", len(routes))
	}
	route := routes[0]
	if route.Method != http.MethodPost || route.Path != socialWebhooksPathPrefix || !route.PathPrefix || route.Handler == nil {
		t.Fatalf("unexpected route=%#v", route)
	}
}

func TestSocialWebhookRoutePassesMAXSecretAndBodyToReceiver(t *testing.T) {
	account := socialWebhookTestAccount()
	receiver := &socialWebhookReceiverStub{}
	api := socialWebhookAPI{
		accounts:  socialWebhookAccountStub{account: account, settings: socialWebhookTestSettings()},
		configs:   socialWebhookConfigStub{},
		secrets:   fakeWebhookSecrets{},
		registry:  socialWebhookResolverStub{receiver: receiver},
		processor: mustInboxProcessor(t),
	}
	path := socialWebhooksPathPrefix + "max-messenger/" + account.OrganizationID + "/" + account.WorkspaceID + "/" + account.ID
	body := `{"update_type":"message_created","chat_id":-70801090403050}`
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(body))
	req.Header.Set("X-Max-Bot-Api-Secret", "max-webhook-secret")
	recorder := httptest.NewRecorder()
	api.receive(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{}" {
		t.Fatalf("response=%d %q", recorder.Code, recorder.Body.String())
	}
	if !receiver.called || string(receiver.request.Body) != body || string(receiver.request.VerificationToken) != "max-webhook-secret" {
		t.Fatalf("receiver_called=%v request=%#v", receiver.called, receiver.request)
	}
}

func TestSocialWebhookRouteRejectsMissingCapabilityOrSecret(t *testing.T) {
	account := socialWebhookTestAccount()
	receiver := &socialWebhookReceiverStub{}
	api := socialWebhookAPI{
		accounts: socialWebhookAccountStub{account: account, settings: nil}, configs: socialWebhookConfigStub{}, secrets: fakeWebhookSecrets{},
		registry: socialWebhookResolverStub{receiver: receiver}, processor: mustInboxProcessor(t),
	}
	path := socialWebhooksPathPrefix + "max-messenger/" + account.OrganizationID + "/" + account.WorkspaceID + "/" + account.ID
	for _, header := range []string{"", "wrong-secret"} {
		req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(`{"update_type":"message_created"}`))
		if header != "" {
			req.Header.Set("X-Max-Bot-Api-Secret", header)
		}
		recorder := httptest.NewRecorder()
		api.receive(recorder, req)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "{}" {
			t.Fatalf("header=%q response=%d %q", header, recorder.Code, recorder.Body.String())
		}
	}
	if receiver.called {
		t.Fatal("receiver called when webhook capability was disabled")
	}
}

func TestSocialWebhookHelpersAreTenantBound(t *testing.T) {
	if connectorID, orgID, workspaceID, accountID, ok := parseSocialWebhookPath(socialWebhooksPathPrefix + "max-messenger/org/workspace/account"); !ok || connectorID != "max-messenger" || orgID != "org" || workspaceID != "workspace" || accountID != "account" {
		t.Fatalf("unexpected parsed path: %q %q %q %q %v", connectorID, orgID, workspaceID, accountID, ok)
	}
	if _, _, _, _, ok := parseSocialWebhookPath(socialWebhooksPathPrefix + "max-messenger/org/workspace"); ok {
		t.Fatal("short webhook path accepted")
	}
	one := socialWebhookEventID("account-a", "sha256:"+strings.Repeat("a", 64))
	two := socialWebhookEventID("account-b", "sha256:"+strings.Repeat("a", 64))
	if one == two || !strings.HasPrefix(one, "sha256:") {
		t.Fatalf("event ids are not account-bound: %q %q", one, two)
	}
}

func mustInboxProcessor(t *testing.T) *inboxrepo.Processor {
	t.Helper()
	processor, err := inboxrepo.New(&sql.DB{})
	if err != nil {
		t.Fatal(err)
	}
	return processor
}
