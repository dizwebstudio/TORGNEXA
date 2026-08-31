package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type socialWebhookSubscriptionConfigStub struct{}

func (socialWebhookSubscriptionConfigStub) Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error) {
	return json.RawMessage(`{}`), 1, nil
}

func (socialWebhookSubscriptionConfigStub) Put(context.Context, tenancy.Scope, string, json.RawMessage, int64) (int64, error) {
	return 1, nil
}

type socialWebhookSubscriptionControllerStub struct {
	subscribeCalls      int
	unsubscribeCalls    int
	subscribeEndpoint   string
	unsubscribeEndpoint string
	err                 error
}

func (stub *socialWebhookSubscriptionControllerStub) SubscribeSocialWebhook(_ context.Context, _ sdk.Account, _ sdk.Runtime, endpoint string) error {
	stub.subscribeCalls++
	stub.subscribeEndpoint = endpoint
	return stub.err
}

func (stub *socialWebhookSubscriptionControllerStub) UnsubscribeSocialWebhook(_ context.Context, _ sdk.Account, _ sdk.Runtime, endpoint string) error {
	stub.unsubscribeCalls++
	stub.unsubscribeEndpoint = endpoint
	return stub.err
}

type socialWebhookSubscriptionRuntimeStub struct {
	controller sdk.SocialWebhookController
	err        error
}

func (stub socialWebhookSubscriptionRuntimeStub) SocialWebhookController(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialWebhookController, error) {
	return stub.controller, stub.err
}

type socialWebhookSubscriptionOperationStub struct {
	receipt      logistics.OperationReceipt
	fresh        bool
	action       string
	key          string
	complete     json.RawMessage
	completeCall int
}

func (stub *socialWebhookSubscriptionOperationStub) BeginOperation(_ context.Context, _ tenancy.Scope, action, key string, _ [32]byte) (logistics.OperationReceipt, bool, error) {
	stub.action = action
	stub.key = key
	return stub.receipt, stub.fresh, nil
}

func (stub *socialWebhookSubscriptionOperationStub) CompleteOperation(_ context.Context, _ tenancy.Scope, action, key string, result json.RawMessage) error {
	if action != stub.action || key != stub.key {
		return errors.New("operation identity mismatch")
	}
	stub.completeCall++
	stub.complete = append([]byte(nil), result...)
	stub.receipt = logistics.OperationReceipt{State: "completed", Result: append([]byte(nil), result...)}
	return nil
}

type socialWebhookSubscriptionAuditStub struct {
	entries []audit.Entry
	err     error
}

func (stub *socialWebhookSubscriptionAuditStub) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	if stub.err != nil {
		return audit.Record{}, stub.err
	}
	stub.entries = append(stub.entries, entry)
	return audit.Record{}, nil
}

func TestSocialWebhookSubscriptionIsIdempotentAndAudited(t *testing.T) {
	account := socialWebhookTestAccount()
	scope, err := tenancy.ParseScope(account.OrganizationID, account.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	controller := &socialWebhookSubscriptionControllerStub{}
	operations := &socialWebhookSubscriptionOperationStub{
		fresh:   true,
		receipt: logistics.OperationReceipt{State: "pending", Result: json.RawMessage(`{}`)},
	}
	auditor := &socialWebhookSubscriptionAuditStub{}
	routes := newSocialWebhookSubscriptionRoutes(
		socialWebhookAccountStub{account: account, settings: socialWebhookTestSettings()},
		socialWebhookSubscriptionRuntimeStub{controller: controller},
		fakeWebhookSecrets{}, socialWebhookSubscriptionConfigStub{}, operations, auditor,
	)
	route := protectedSocialWebhookSubscriptionRoute(t, routes, http.MethodPut)
	endpoint := "https://hooks.example.test/telegram/channel-main"
	request := socialWebhookSubscriptionRequest(t, http.MethodPut, endpoint, "webhook-subscribe-1", scope)
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || controller.subscribeCalls != 1 || controller.subscribeEndpoint != endpoint || operations.completeCall != 1 || len(auditor.entries) != 1 || auditor.entries[0].Action != "social.webhook.subscription_enabled" {
		t.Fatalf("status=%d calls=%d endpoint=%q complete=%d audit=%v body=%s", response.Code, controller.subscribeCalls, controller.subscribeEndpoint, operations.completeCall, auditor.entries, response.Body.String())
	}

	operations.fresh = false
	replay := socialWebhookSubscriptionRequest(t, http.MethodPut, endpoint, "webhook-subscribe-1", scope)
	replayResponse := httptest.NewRecorder()
	route.Handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusOK || controller.subscribeCalls != 1 || operations.completeCall != 1 {
		t.Fatalf("replay status=%d calls=%d complete=%d body=%s", replayResponse.Code, controller.subscribeCalls, operations.completeCall, replayResponse.Body.String())
	}
}

func TestSocialWebhookUnsubscriptionKeepsPendingOnProviderFailure(t *testing.T) {
	account := socialWebhookTestAccount()
	scope, err := tenancy.ParseScope(account.OrganizationID, account.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	controller := &socialWebhookSubscriptionControllerStub{err: errors.New("provider unavailable")}
	operations := &socialWebhookSubscriptionOperationStub{
		fresh:   true,
		receipt: logistics.OperationReceipt{State: "pending", Result: json.RawMessage(`{}`)},
	}
	auditor := &socialWebhookSubscriptionAuditStub{}
	routes := newSocialWebhookSubscriptionRoutes(
		socialWebhookAccountStub{account: account, settings: socialWebhookTestSettings()},
		socialWebhookSubscriptionRuntimeStub{controller: controller},
		fakeWebhookSecrets{}, socialWebhookSubscriptionConfigStub{}, operations, auditor,
	)
	route := protectedSocialWebhookSubscriptionRoute(t, routes, http.MethodDelete)
	request := socialWebhookSubscriptionRequest(t, http.MethodDelete, "https://hooks.example.test/telegram/channel-main", "webhook-unsubscribe-1", scope)
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || controller.unsubscribeCalls != 1 || operations.completeCall != 0 || len(auditor.entries) != 0 {
		t.Fatalf("status=%d calls=%d complete=%d audit=%v body=%s", response.Code, controller.unsubscribeCalls, operations.completeCall, auditor.entries, response.Body.String())
	}
}

func protectedSocialWebhookSubscriptionRoute(t *testing.T, routes []ProtectedRoute, method string) ProtectedRoute {
	t.Helper()
	for _, route := range routes {
		if route.Method == method && route.Path == socialWebhookSubscriptionPath {
			return route
		}
	}
	t.Fatalf("route %s not found in %#v", method, routes)
	return ProtectedRoute{}
}

func socialWebhookSubscriptionRequest(t *testing.T, method, endpoint, key string, scope tenancy.Scope) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, socialWebhookSubscriptionPath, strings.NewReader(`{"account_id":"channel-main","endpoint":"`+endpoint+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	ctx := context.WithValue(request.Context(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "operator-1"})
	return request.WithContext(ctx)
}

var _ secrets.SecretProvider = fakeWebhookSecrets{}
