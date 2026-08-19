package sms

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func sc(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if e != nil {
		t.Fatal(e)
	}
	return s
}

type consent bool

func (c consent) AllowsMarketing(context.Context, tenancy.Scope, string, string) bool { return bool(c) }
func TestMarketingCannotBypassConsentAndPhoneIsFingerprintOnly(t *testing.T) {
	svc := &Service{Gateway: NewFakeProvider(), Consent: consent(false), Store: NewStore(), TenantLimit: 10}
	r := SendRequest{ExternalID: "n1", Phone: "+79991234567", Text: "sale", Class: Marketing, ConsentRef: "c1", IdempotencyKey: "i1"}
	if _, e := svc.Send(context.Background(), sc(t), r); e != ErrConsentRequired {
		t.Fatalf("err=%v", e)
	}
	svc.Consent = consent(true)
	ev, e := svc.Send(context.Background(), sc(t), r)
	if e != nil {
		t.Fatal(e)
	}
	if ev.PhoneFingerprint == r.Phone || len(ev.PhoneFingerprint) != 64 {
		t.Fatal("raw PII leaked")
	}
}
func TestCallbackDedupeAndQuota(t *testing.T) {
	svc := &Service{Gateway: NewFakeProvider(), Store: NewStore(), TenantLimit: 1}
	r := SendRequest{ExternalID: "n1", Phone: "+79991234567", Text: "otp", Class: Transactional, IdempotencyKey: "i1"}
	if _, e := svc.Send(context.Background(), sc(t), r); e != nil {
		t.Fatal(e)
	}
	r.IdempotencyKey = "i2"
	if _, e := svc.Send(context.Background(), sc(t), r); e != ErrQuota {
		t.Fatalf("quota=%v", e)
	}
	cb := DeliveryCallback{DeliveryID: "d1", RemoteID: "r1", Status: "delivered", OccurredAt: time.Now().UTC()}
	a, _ := svc.RecordCallback(sc(t), cb)
	b, _ := svc.RecordCallback(sc(t), cb)
	if !a || b {
		t.Fatal("callback replay unsafe")
	}
}

type failingGateway struct{}

func (failingGateway) Send(context.Context, tenancy.Scope, SendRequest) (SendResult, error) {
	return SendResult{}, ErrInvalid
}
func (failingGateway) Status(context.Context, tenancy.Scope, string) (SendResult, error) {
	return SendResult{}, ErrInvalid
}

type fallbackCapture struct {
	called     bool
	externalID string
	class      Class
	reason     string
}

func (f *fallbackCapture) OnSMSFailure(_ context.Context, _ tenancy.Scope, externalID string, class Class, reason string) error {
	f.called, f.externalID, f.class, f.reason = true, externalID, class, reason
	return nil
}
func TestFallbackReceivesNoPhoneOrMessage(t *testing.T) {
	fb := &fallbackCapture{}
	svc := &Service{Gateway: failingGateway{}, Fallback: fb, Store: NewStore(), TenantLimit: 10}
	r := SendRequest{ExternalID: "n-fallback", Phone: "+79991234567", Text: "otp 1234", Class: Transactional, IdempotencyKey: "i-fallback"}
	if _, err := svc.Send(context.Background(), sc(t), r); err == nil {
		t.Fatal("expected gateway error")
	}
	if !fb.called || fb.externalID != r.ExternalID || fb.class != Transactional || fb.reason != "delivery_failed" {
		t.Fatalf("fallback=%+v", fb)
	}
	if fb.externalID == r.Phone || fb.reason == r.Text {
		t.Fatal("fallback leaked PII/content")
	}
}
