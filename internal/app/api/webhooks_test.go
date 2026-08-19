package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/webhooks"
)

type webhookResolverStub struct {
	scope tenancy.Scope
	err   error
}

func (s webhookResolverStub) WebhookScope(*http.Request) (tenancy.Scope, error) {
	return s.scope, s.err
}

type webhookManagerStub struct {
	scope                      tenancy.Scope
	createID, endpoint, secret string
	eventTypes                 []eventbus.EventType
	overlap                    time.Duration
	actionID                   string
	create                     webhooks.Subscription
	list                       []webhooks.Subscription
	replay                     webhooks.Delivery
	history                    []webhooks.HistoryEntry
	err                        error
}

func (m *webhookManagerStub) CreateSubscription(_ context.Context, s tenancy.Scope, id, endpoint string, types []eventbus.EventType, material []byte) (webhooks.Subscription, error) {
	m.scope = s
	m.createID = id
	m.endpoint = endpoint
	m.secret = string(material)
	m.eventTypes = append([]eventbus.EventType(nil), types...)
	return m.create, m.err
}
func (m *webhookManagerStub) ListSubscriptions(_ context.Context, s tenancy.Scope) ([]webhooks.Subscription, error) {
	m.scope = s
	return m.list, m.err
}
func (m *webhookManagerStub) DisableSubscription(_ context.Context, s tenancy.Scope, id string) error {
	m.scope = s
	m.actionID = id
	return m.err
}
func (m *webhookManagerStub) RotateSigningSecret(_ context.Context, s tenancy.Scope, id string, material []byte, overlap time.Duration) (webhooks.Subscription, error) {
	m.scope = s
	m.actionID = id
	m.secret = string(material)
	m.overlap = overlap
	return m.create, m.err
}
func (m *webhookManagerStub) Replay(_ context.Context, s tenancy.Scope, id string) (webhooks.Delivery, error) {
	m.scope = s
	m.actionID = id
	return m.replay, m.err
}
func (m *webhookManagerStub) DeliveryHistory(_ context.Context, s tenancy.Scope, id string, limit int) ([]webhooks.HistoryEntry, error) {
	m.scope = s
	m.actionID = id
	return m.history, m.err
}

func webhookScope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWebhookCreateUsesAuthenticatedScopeAndNeverEchoesSecret(t *testing.T) {
	scope := webhookScope(t)
	mgr := &webhookManagerStub{create: webhooks.Subscription{ID: "whs_test", Endpoint: "https://hooks.example/t", Status: webhooks.SubscriptionActive, Version: 1, CreatedAt: time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)}}
	handler := newHandlerWithWebhooks(slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), mgr, webhookResolverStub{scope: scope})
	secret := "0123456789abcdef0123456789abcdef"
	body := `{"id":"whs_test","endpoint":"https://hooks.example/t","event_types":["commerce.orders.order_created.v1"],"signing_secret":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPost, WebhookSubscriptionsPath, strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if mgr.scope != scope {
		t.Fatal("authenticated scope not propagated")
	}
	if mgr.secret != secret || mgr.createID != "whs_test" || len(mgr.eventTypes) != 1 {
		t.Fatalf("wrong create call %#v", mgr)
	}
	if strings.Contains(rr.Body.String(), secret) || strings.Contains(rr.Body.String(), "sec:v1:") {
		t.Fatalf("secret leaked in response: %s", rr.Body.String())
	}
}

func TestWebhookCreateRejectsClientTenantIdentifiers(t *testing.T) {
	scope := webhookScope(t)
	mgr := &webhookManagerStub{}
	handler := newHandlerWithWebhooks(nil, mgr, webhookResolverStub{scope: scope})
	secret := "0123456789abcdef0123456789abcdef"
	body := `{"id":"whs_test","endpoint":"https://hooks.example/t","event_types":["commerce.orders.order_created.v1"],"signing_secret":"` + secret + `","organization_id":"018f0000-0000-7000-8000-000000000099"}`
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, WebhookSubscriptionsPath, strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if mgr.createID != "" {
		t.Fatal("manager called for client-supplied tenant override")
	}
}

func TestWebhookAPIUnauthorizedFailsBeforeManager(t *testing.T) {
	mgr := &webhookManagerStub{}
	handler := newHandlerWithWebhooks(nil, mgr, webhookResolverStub{err: errors.New("no auth")})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, WebhookSubscriptionsPath, nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
	if mgr.scope.Valid() {
		t.Fatal("manager called")
	}
}

func TestWebhookRotateReplayAndHistoryRoutes(t *testing.T) {
	scope := webhookScope(t)
	mgr := &webhookManagerStub{replay: webhooks.Delivery{ID: "whd_new", ReplayOf: "whd_old", Status: webhooks.DeliveryPending}, history: []webhooks.HistoryEntry{{DeliveryID: "whd_old", Attempt: 1, Outcome: webhooks.OutcomeDLQ, HTTPStatus: 410, DurationMS: 12, ErrorCode: "http_permanent", CompletedAt: time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)}}}
	h := newHandlerWithWebhooks(nil, mgr, webhookResolverStub{scope: scope})
	secret := "abcdef0123456789abcdef0123456789"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, WebhookSubscriptionsPath+"/whs_1/rotate-secret", strings.NewReader(`{"signing_secret":"`+secret+`","overlap_seconds":600}`)))
	if rr.Code != http.StatusOK || mgr.actionID != "whs_1" || mgr.overlap != 10*time.Minute {
		t.Fatalf("rotate status=%d id=%q overlap=%s", rr.Code, mgr.actionID, mgr.overlap)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, WebhookDeliveriesPrefix+"whd_old/replay", nil))
	if rr.Code != http.StatusAccepted || !strings.Contains(rr.Body.String(), "whd_new") {
		t.Fatalf("replay status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, WebhookDeliveriesPrefix+"whd_old/history?limit=10", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "http_permanent") {
		t.Fatalf("history status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWebhookDisableRouteUsesAuthenticatedScope(t *testing.T) {
	scope := webhookScope(t)
	mgr := &webhookManagerStub{}
	h := newHandlerWithWebhooks(nil, mgr, webhookResolverStub{scope: scope})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, WebhookSubscriptionsPath+"/whs_n8n", nil))
	if rr.Code != http.StatusNoContent || mgr.actionID != "whs_n8n" || mgr.scope != scope {
		t.Fatalf("disable status=%d id=%q scope=%#v", rr.Code, mgr.actionID, mgr.scope)
	}
}

func TestWebhookAPIStrictJSONAndMethod(t *testing.T) {
	scope := webhookScope(t)
	h := newHandlerWithWebhooks(nil, &webhookManagerStub{}, webhookResolverStub{scope: scope})
	for _, body := range []string{`{"id":"x","unknown":true}`, `{} {}`} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, WebhookSubscriptionsPath, strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("body %q status=%d", body, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, WebhookSubscriptionsPath, nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete status=%d", rr.Code)
	}
}
