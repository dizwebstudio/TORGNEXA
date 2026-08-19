// Package sms adds a provider-neutral SMS delivery boundary for Notification Center.
package sms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"regexp"
	"sync"
	"time"
)

var ErrInvalid = errors.New("sms: invalid value")
var ErrConsentRequired = errors.New("sms: marketing consent required")
var ErrQuota = errors.New("sms: quota exceeded")
var phoneRe = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

type Class string

const (
	Transactional Class = "transactional"
	Marketing     Class = "marketing"
)

type SendRequest struct {
	ExternalID, Phone, Text string
	Class                   Class
	ConsentRef              string
	IdempotencyKey          string
}
type SendResult struct {
	RemoteID, Status string
	ObservedAt       time.Time
}
type DeliveryCallback struct {
	DeliveryID, RemoteID, Status string
	OccurredAt                   time.Time
}
type Provider interface {
	Send(context.Context, tenancy.Scope, SendRequest) (SendResult, error)
	Status(context.Context, tenancy.Scope, string) (SendResult, error)
}
type ConsentChecker interface {
	AllowsMarketing(context.Context, tenancy.Scope, string, string) bool
}
type CallbackVerifier interface {
	Verify(context.Context, tenancy.Scope, []byte, []byte) (DeliveryCallback, error)
}
type Evidence struct {
	ExternalID, RemoteID, PhoneFingerprint, Status string
	Class                                          Class
	OccurredAt                                     time.Time
}
type Store struct {
	mu         sync.Mutex
	deliveries map[string]map[string]Evidence
	callbacks  map[string]bool
	counts     map[string]int
}

func NewStore() *Store {
	return &Store{deliveries: map[string]map[string]Evidence{}, callbacks: map[string]bool{}, counts: map[string]int{}}
}
func key(s tenancy.Scope) string { return s.OrganizationID().String() + "/" + s.WorkspaceID().String() }
func fingerprint(phone string) string {
	h := sha256.Sum256([]byte(phone))
	return hex.EncodeToString(h[:])
}

type FallbackSink interface {
	OnSMSFailure(context.Context, tenancy.Scope, string, Class, string) error
}

type Service struct {
	Gateway     Provider
	Consent     ConsentChecker
	Fallback    FallbackSink
	Store       *Store
	TenantLimit int
}

func (s *Service) Send(ctx context.Context, scope tenancy.Scope, r SendRequest) (Evidence, error) {
	if s == nil || s.Gateway == nil || s.Store == nil || !scope.Valid() || r.ExternalID == "" || !phoneRe.MatchString(r.Phone) || r.Text == "" || len(r.Text) > 1000 || r.IdempotencyKey == "" || (r.Class != Transactional && r.Class != Marketing) {
		return Evidence{}, ErrInvalid
	}
	if r.Class == Marketing && (s.Consent == nil || r.ConsentRef == "" || !s.Consent.AllowsMarketing(ctx, scope, r.Phone, r.ConsentRef)) {
		return Evidence{}, ErrConsentRequired
	}
	k := key(scope)
	s.Store.mu.Lock()
	if s.Store.deliveries[k] == nil {
		s.Store.deliveries[k] = map[string]Evidence{}
	}
	if old, ok := s.Store.deliveries[k][r.IdempotencyKey]; ok {
		s.Store.mu.Unlock()
		return old, nil
	}
	if s.TenantLimit > 0 && s.Store.counts[k] >= s.TenantLimit {
		s.Store.mu.Unlock()
		return Evidence{}, ErrQuota
	}
	s.Store.mu.Unlock()
	out, err := s.Gateway.Send(ctx, scope, r)
	if err != nil {
		if s.Fallback != nil {
			if fallbackErr := s.Fallback.OnSMSFailure(ctx, scope, r.ExternalID, r.Class, "delivery_failed"); fallbackErr != nil {
				return Evidence{}, errors.Join(err, fallbackErr)
			}
		}
		return Evidence{}, err
	}
	if out.RemoteID == "" || out.Status == "" || out.ObservedAt.IsZero() {
		return Evidence{}, ErrInvalid
	}
	ev := Evidence{ExternalID: r.ExternalID, RemoteID: out.RemoteID, PhoneFingerprint: fingerprint(r.Phone), Status: out.Status, Class: r.Class, OccurredAt: out.ObservedAt}
	s.Store.mu.Lock()
	s.Store.deliveries[k][r.IdempotencyKey] = ev
	s.Store.counts[k]++
	s.Store.mu.Unlock()
	return ev, nil
}
func (s *Service) RecordCallback(scope tenancy.Scope, c DeliveryCallback) (bool, error) {
	if s == nil || s.Store == nil || !scope.Valid() || c.DeliveryID == "" || c.RemoteID == "" || c.Status == "" || c.OccurredAt.IsZero() {
		return false, ErrInvalid
	}
	s.Store.mu.Lock()
	defer s.Store.mu.Unlock()
	k := key(scope) + "/" + c.DeliveryID
	if s.Store.callbacks[k] {
		return false, nil
	}
	s.Store.callbacks[k] = true
	return true, nil
}

type FakeProvider struct {
	mu   sync.Mutex
	sent map[string]SendResult
}

func NewFakeProvider() *FakeProvider { return &FakeProvider{sent: map[string]SendResult{}} }
func (f *FakeProvider) Send(_ context.Context, _ tenancy.Scope, r SendRequest) (SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.sent[r.IdempotencyKey]; ok {
		return old, nil
	}
	h := sha256.Sum256([]byte(r.ExternalID + "/" + r.IdempotencyKey))
	out := SendResult{RemoteID: "fake_" + hex.EncodeToString(h[:8]), Status: "accepted", ObservedAt: time.Now().UTC()}
	f.sent[r.IdempotencyKey] = out
	return out, nil
}
func (f *FakeProvider) Status(_ context.Context, _ tenancy.Scope, id string) (SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.sent {
		if v.RemoteID == id {
			return v, nil
		}
	}
	return SendResult{}, ErrInvalid
}
