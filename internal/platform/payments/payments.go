// Package payments keeps tenant-scoped payment state and verified webhook replay protection without card data.
package payments

import (
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"sync"
	"time"
)

var ErrInvalid = errors.New("payments: invalid value")

type Payment struct {
	ID, RailID, RemoteID, ExternalID, Status string
	Amount                                   domain.Money
	Commission                               domain.Money
	Version                                  int64
	ObservedAt                               time.Time
}
type WebhookEvidence struct {
	DeliveryID, RemoteID, EventType, BodyDigest string
	VerifiedAt                                  time.Time
}
type Store struct {
	mu       sync.Mutex
	payments map[string]map[string]Payment
	hooks    map[string]map[string]WebhookEvidence
}

func NewStore() *Store {
	return &Store{payments: map[string]map[string]Payment{}, hooks: map[string]map[string]WebhookEvidence{}}
}
func pk(sc tenancy.Scope) string {
	return sc.OrganizationID().String() + "/" + sc.WorkspaceID().String()
}
func (s *Store) UpsertRemote(sc tenancy.Scope, p Payment) (Payment, error) {
	if !sc.Valid() || p.ID == "" || p.RailID == "" || p.RemoteID == "" || p.ExternalID == "" || p.Status == "" || p.Amount.Validate() != nil || p.Version < 1 || p.ObservedAt.IsZero() {
		return Payment{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := pk(sc)
	if s.payments[k] == nil {
		s.payments[k] = map[string]Payment{}
	}
	old, ok := s.payments[k][p.ID]
	if ok && old.Version >= p.Version {
		return old, nil
	}
	s.payments[k][p.ID] = p
	return p, nil
}
func (s *Store) RecordWebhook(sc tenancy.Scope, e WebhookEvidence) (bool, error) {
	if !sc.Valid() || e.DeliveryID == "" || e.RemoteID == "" || e.EventType == "" || len(e.BodyDigest) != 64 || e.VerifiedAt.IsZero() {
		return false, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := pk(sc)
	if s.hooks[k] == nil {
		s.hooks[k] = map[string]WebhookEvidence{}
	}
	if _, ok := s.hooks[k][e.DeliveryID]; ok {
		return false, nil
	}
	s.hooks[k][e.DeliveryID] = e
	return true, nil
}
