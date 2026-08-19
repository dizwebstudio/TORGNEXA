// Package edo coordinates signed electronic documents while treating remote provider status as authoritative.
package edo

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"time"
)

var ErrInvalid = errors.New("edo: invalid value")

type Document struct {
	ID, AdapterID, AccountID, ExternalID, RemoteID, Kind, Status, ArtifactRef, SignatureRef, MChDRef, CounterpartyRef string
	Version                                                                                                           int64
	ObservedAt                                                                                                        time.Time
}
type Sender interface {
	Send(context.Context, tenancy.Scope, Document, string) (string, error)
	Status(context.Context, tenancy.Scope, string) (string, time.Time, error)
}
type Service struct {
	mu      sync.Mutex
	tenants map[string]map[string]Document
	sender  Sender
}

func NewService(s Sender) *Service {
	return &Service{tenants: map[string]map[string]Document{}, sender: s}
}
func k(sc tenancy.Scope) string {
	return sc.OrganizationID().String() + "/" + sc.WorkspaceID().String()
}
func (d Document) validForSend() bool {
	return d.ID != "" && d.AdapterID != "" && d.AccountID != "" && d.ExternalID != "" && d.Kind != "" && d.ArtifactRef != "" && d.SignatureRef != "" && d.CounterpartyRef != ""
}
func (s *Service) Send(ctx context.Context, sc tenancy.Scope, d Document, idempotency string) (Document, error) {
	if !sc.Valid() || !d.validForSend() || idempotency == "" || s.sender == nil {
		return Document{}, ErrInvalid
	}
	s.mu.Lock()
	if m := s.tenants[k(sc)]; m != nil {
		if old, ok := m[d.ID]; ok {
			s.mu.Unlock()
			return old, nil
		}
	}
	s.mu.Unlock()
	remote, err := s.sender.Send(ctx, sc, d, idempotency)
	if err != nil {
		return Document{}, err
	}
	if remote == "" {
		return Document{}, ErrInvalid
	}
	d.RemoteID = remote
	d.Status = "submitted"
	d.Version = 1
	d.ObservedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tenants[k(sc)] == nil {
		s.tenants[k(sc)] = map[string]Document{}
	}
	s.tenants[k(sc)][d.ID] = d
	return d, nil
}
func (s *Service) Refresh(ctx context.Context, sc tenancy.Scope, id string) (Document, error) {
	if !sc.Valid() || id == "" || s.sender == nil {
		return Document{}, ErrInvalid
	}
	s.mu.Lock()
	m := s.tenants[k(sc)]
	d, ok := m[id]
	s.mu.Unlock()
	if !ok {
		return Document{}, ErrInvalid
	}
	status, at, err := s.sender.Status(ctx, sc, d.RemoteID)
	if err != nil {
		return Document{}, err
	}
	if status == "" || at.IsZero() {
		return Document{}, ErrInvalid
	}
	d.Status = status
	d.ObservedAt = at.UTC()
	d.Version++
	s.mu.Lock()
	m[id] = d
	s.mu.Unlock()
	return d, nil
}
