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

type commerceWebhookAccountStub struct{ account sdk.Account }

func (stub commerceWebhookAccountStub) AccountByID(_ context.Context, _, _, accountID string) (sdk.Account, error) {
	if accountID != stub.account.ID {
		return sdk.Account{}, sdk.ErrAccountNotFound
	}
	return stub.account, nil
}

type commerceWebhookConfigStub struct{}

func (commerceWebhookConfigStub) Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error) {
	return json.RawMessage(`{"store_host":"shop.example.com"}`), 1, nil
}

type commerceWebhookReceiverStub struct {
	request sdk.CommerceWebhookRequest
	claim   sdk.CommerceWebhookClaim
	called  bool
}

func (stub *commerceWebhookReceiverStub) ReceiveCommerceWebhook(_ context.Context, account sdk.Account, _ sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	stub.called = true
	stub.request = request
	claim := sdk.CommerceWebhookClaim{DeliveryID: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EventType: request.ExpectedTopic, ResourceKind: "product", ResourceRemoteID: "product-1", OccurredAt: request.ReceivedAt, CanonicalPayload: json.RawMessage(`{"event":"product.updated","resource_id":"product-1"}`)}
	stub.claim = claim
	duplicate, err := dedup.ClaimCommerceWebhook(context.Background(), account, claim)
	if err != nil {
		return sdk.CommerceWebhookResult{}, err
	}
	return sdk.CommerceWebhookResult{DeliveryID: claim.DeliveryID, EventType: claim.EventType, ResourceKind: claim.ResourceKind, ResourceRemoteID: claim.ResourceRemoteID, OccurredAt: claim.OccurredAt, Duplicate: duplicate, CanonicalPayload: claim.CanonicalPayload}, nil
}

type commerceWebhookResolverStub struct {
	receiver builtinruntime.CommerceWebhookReceiver
}

func (stub commerceWebhookResolverStub) CommerceWebhookReceiver(sdk.Account, sdk.Runtime, builtinruntime.ConfigLoader) (builtinruntime.CommerceWebhookReceiver, error) {
	return stub.receiver, nil
}

type commerceWebhookDedupStub struct{ claim sdk.CommerceWebhookClaim }

func (stub *commerceWebhookDedupStub) ClaimCommerceWebhook(_ context.Context, _ sdk.Account, claim sdk.CommerceWebhookClaim) (bool, error) {
	stub.claim = claim
	return false, nil
}

func TestCommerceWebhookRouteIsPublicAndUsesContractPrefix(t *testing.T) {
	processor, err := inboxrepo.New(&sql.DB{})
	if err != nil {
		t.Fatal(err)
	}
	routes := newCommerceWebhookRoutes(
		commerceWebhookAccountStub{},
		commerceWebhookConfigStub{},
		fakeWebhookSecrets{},
		commerceWebhookResolverStub{},
		processor,
	)
	if len(routes) != 1 {
		t.Fatalf("routes=%d, want one public commerce webhook route", len(routes))
	}
	route := routes[0]
	if route.Method != http.MethodPost || route.Path != commerceWebhooksPathPrefix || !route.PathPrefix || route.Handler == nil {
		t.Fatalf("unexpected route=%#v", route)
	}
}

func TestCommerceWebhookRouteNormalizesTopicAndPassesRawBodyToReceiver(t *testing.T) {
	at := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	account := sdk.Account{ID: "storefront-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "storefront-a", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, CreatedAt: at, UpdatedAt: at, Health: sdk.Health{Status: sdk.HealthUnknown}}
	receiver := &commerceWebhookReceiverStub{}
	dedup := &commerceWebhookDedupStub{}
	api := commerceWebhookAPI{
		accounts: commerceWebhookAccountStub{account: account},
		configs:  commerceWebhookConfigStub{},
		secrets:  fakeWebhookSecrets{},
		registry: commerceWebhookResolverStub{receiver: receiver},
		dedup:    func(tenancy.Scope) sdk.CommerceWebhookDeduplicator { return dedup },
		headers: func(_ string, headers http.Header) (string, string, bool) {
			return strings.TrimSpace(headers.Get("X-Test-Signature")), normalizeCommerceWebhookTopic(headers.Get("X-Test-Topic")), true
		},
	}
	path := commerceWebhooksPathPrefix + "storefront-a/" + account.OrganizationID + "/" + account.WorkspaceID + "/" + account.ID
	body := `{"event":"PRODUCT_UPDATED","data":{"object":{"id":"UHJvZHVjdDox"}}}`
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(body))
	req.Header.Set("X-Test-Signature", "protected..signature")
	req.Header.Set("X-Test-Topic", "PRODUCT_UPDATED")
	recorder := httptest.NewRecorder()
	api.receive(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{}" {
		t.Fatalf("response=%d %q", recorder.Code, recorder.Body.String())
	}
	if !receiver.called || receiver.request.HeaderTopic != "product.updated" || receiver.request.ExpectedTopic != "product.updated" || string(receiver.request.Body) != body {
		t.Fatalf("receiver request=%#v called=%v", receiver.request, receiver.called)
	}
	if dedup.claim.EventType != "product.updated" || dedup.claim.ResourceRemoteID != "product-1" {
		t.Fatalf("claim=%#v", dedup.claim)
	}
}

func TestCommerceWebhookRouteRejectsUnsupportedHeadersBeforeReceiver(t *testing.T) {
	at := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	account := sdk.Account{ID: "storefront-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "storefront-a", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, Version: 1, CreatedAt: at, UpdatedAt: at, Health: sdk.Health{Status: sdk.HealthUnknown}}
	receiver := &commerceWebhookReceiverStub{}
	api := commerceWebhookAPI{accounts: commerceWebhookAccountStub{account: account}, configs: commerceWebhookConfigStub{}, secrets: fakeWebhookSecrets{}, registry: commerceWebhookResolverStub{receiver: receiver}, dedup: func(tenancy.Scope) sdk.CommerceWebhookDeduplicator { return &commerceWebhookDedupStub{} }, headers: func(string, http.Header) (string, string, bool) { return "", "", false }}
	path := commerceWebhooksPathPrefix + "storefront-a/" + account.OrganizationID + "/" + account.WorkspaceID + "/" + account.ID
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+path, strings.NewReader(`{"event":"PRODUCT_UPDATED"}`))
	req.Header.Set("X-Webhook-Signature", "not-used")
	req.Header.Set("X-Webhook-Topic", "product.updated")
	recorder := httptest.NewRecorder()
	api.receive(recorder, req)
	if recorder.Code != http.StatusOK || receiver.called {
		t.Fatalf("response=%d receiver_called=%v", recorder.Code, receiver.called)
	}
}

func TestCommerceWebhookHelpersAreBounded(t *testing.T) {
	if got := normalizeCommerceWebhookTopic("PRODUCT_UPDATED"); got != "product.updated" {
		t.Fatalf("normalized topic=%q", got)
	}
	for _, value := range []string{"", "order.created.extra", "payment.created", "order.cancelled"} {
		if got := normalizeCommerceWebhookTopic(value); got != "" {
			t.Fatalf("unsupported topic %q normalized to %q", value, got)
		}
	}
	one := commerceWebhookEventID("account-a", "sha256:"+strings.Repeat("a", 64))
	two := commerceWebhookEventID("account-b", "sha256:"+strings.Repeat("a", 64))
	if one == two || !strings.HasPrefix(one, "sha256:") || len(one) != len("sha256:")+64 {
		t.Fatalf("event ids are not account-bound: %q %q", one, two)
	}
}
