package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
)

type notificationResolver struct {
	scope     tenancy.Scope
	recipient string
	err       error
}

func (r notificationResolver) NotificationIdentity(*http.Request) (tenancy.Scope, string, error) {
	return r.scope, r.recipient, r.err
}

type notificationSvc struct {
	listRecipient string
	markRecipient string
	put           notifications.Preference
}

func (s *notificationSvc) List(_ context.Context, _ tenancy.Scope, r string, _ int) ([]notifications.Notification, error) {
	s.listRecipient = r
	return []notifications.Notification{}, nil
}
func (s *notificationSvc) MarkRead(_ context.Context, _ tenancy.Scope, r, id string) (notifications.Notification, error) {
	s.markRecipient = r
	if id == "missing" {
		return notifications.Notification{}, notifications.ErrNotFound
	}
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return notifications.Notification{ID: id, RecipientID: r, DedupeKey: "x", Severity: notifications.SeverityInfo, Title: "x", OccurrenceCount: 1, FirstOccurredAt: now, LastOccurredAt: now, CreatedAt: now, UpdatedAt: now}, nil
}
func (s *notificationSvc) PutPreference(_ context.Context, _ tenancy.Scope, p notifications.Preference) (notifications.Preference, error) {
	s.put = p
	return p, nil
}
func (s *notificationSvc) GetPreference(_ context.Context, _ tenancy.Scope, recipient string, ch notifications.Channel) (notifications.Preference, error) {
	return notifications.Preference{RecipientID: recipient, Channel: ch, Enabled: ch == notifications.ChannelWebUI, MinSeverity: notifications.SeverityInfo, Version: 1, UpdatedAt: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)}, nil
}
func (s *notificationSvc) Deliveries(context.Context, tenancy.Scope, string, string) ([]notifications.Delivery, error) {
	return []notifications.Delivery{}, nil
}
func notificationScope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNotificationListUsesAuthenticatedRecipientAndRejectsClientScope(t *testing.T) {
	svc := &notificationSvc{}
	h := newHandlerWithNotifications(nil, svc, notificationResolver{scope: notificationScope(t), recipient: "user_auth"})
	req := httptest.NewRequest(http.MethodGet, NotificationsPath+"?limit=10", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || svc.listRecipient != "user_auth" {
		t.Fatalf("code=%d recipient=%s", rec.Code, svc.listRecipient)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, NotificationsPath+"?recipient_id=user_other", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("client recipient code=%d", rec.Code)
	}
}
func TestNotificationAPIIsFailClosedWithoutIdentity(t *testing.T) {
	svc := &notificationSvc{}
	h := newHandlerWithNotifications(nil, svc, notificationResolver{err: errors.New("no auth")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, NotificationsPath, nil))
	if rec.Code != 401 {
		t.Fatalf("code=%d", rec.Code)
	}
}
func TestPreferenceRejectsUnknownFieldsAndBindsRecipient(t *testing.T) {
	svc := &notificationSvc{}
	h := newHandlerWithNotifications(nil, svc, notificationResolver{scope: notificationScope(t), recipient: "user_auth"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, NotificationPreferencesPath+"webhook", strings.NewReader(`{"enabled":true,"min_severity":"warning","recipient_id":"user_other"}`)))
	if rec.Code != 400 {
		t.Fatalf("unknown field code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, NotificationPreferencesPath+"webhook", strings.NewReader(`{"enabled":true,"min_severity":"warning"}`)))
	if rec.Code != 200 || svc.put.RecipientID != "user_auth" || svc.put.Channel != notifications.ChannelWebhook {
		t.Fatalf("code=%d preference=%+v", rec.Code, svc.put)
	}
}
func TestMarkReadNotFoundIsOpaque404(t *testing.T) {
	svc := &notificationSvc{}
	h := newHandlerWithNotifications(nil, svc, notificationResolver{scope: notificationScope(t), recipient: "user_auth"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, NotificationsPath+"/missing/read", nil))
	if rec.Code != 404 {
		t.Fatalf("code=%d", rec.Code)
	}
	var p problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Status != 404 {
		t.Fatalf("problem=%+v", p)
	}
}

func TestPreferenceRejectsOversizedBody(t *testing.T) {
	svc := &notificationSvc{}
	h := newHandlerWithNotifications(nil, svc, notificationResolver{scope: notificationScope(t), recipient: "user_auth"})
	body := `{"enabled":true,"min_severity":"warning"}` + strings.Repeat(" ", (32<<10)+1)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, NotificationPreferencesPath+"webhook", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rec.Code)
	}
}
