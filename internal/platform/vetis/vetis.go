// Package vetis stores authoritative Mercury document/stock reconciliation evidence.
package vetis

import (
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"time"
)

var ErrInvalid = errors.New("vetis: invalid value")

type Evidence struct {
	RemoteID, Kind, RemoteStatus, StockRef, SourceRequestRef string
	ObservedAt                                               time.Time
}
type Store struct {
	mu      sync.Mutex
	tenants map[string][]Evidence
}

func NewStore() *Store { return &Store{tenants: map[string][]Evidence{}} }
func (s *Store) Append(sc tenancy.Scope, e Evidence) error {
	if !sc.Valid() || e.RemoteID == "" || e.Kind == "" || e.RemoteStatus == "" || e.SourceRequestRef == "" || e.ObservedAt.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := sc.OrganizationID().String() + "/" + sc.WorkspaceID().String()
	s.tenants[k] = append(s.tenants[k], e)
	return nil
}
func (s *Store) List(sc tenancy.Scope) []Evidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := sc.OrganizationID().String() + "/" + sc.WorkspaceID().String()
	return append([]Evidence(nil), s.tenants[k]...)
}
