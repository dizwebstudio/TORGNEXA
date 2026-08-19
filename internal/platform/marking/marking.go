// Package marking keeps tenant-scoped, privacy-minimized reconciliation evidence for regulated marking state.
package marking

import (
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"time"
)

var ErrInvalid = errors.New("marking: invalid value")

type StatusFact struct {
	CodeFingerprint, GTIN, RemoteStatus, SourceRef string
	ObservedAt                                     time.Time
}
type Reconciliation struct {
	ID, CodeFingerprint, ExpectedStatus, RemoteStatus, Outcome string
	ObservedAt                                                 time.Time
}
type Store struct {
	mu              sync.Mutex
	tenants         map[string][]StatusFact
	reconciliations map[string][]Reconciliation
}

func NewStore() *Store {
	return &Store{tenants: map[string][]StatusFact{}, reconciliations: map[string][]Reconciliation{}}
}
func key(s tenancy.Scope) string { return s.OrganizationID().String() + "/" + s.WorkspaceID().String() }
func validFact(f StatusFact) bool {
	return len(f.CodeFingerprint) == 64 && f.RemoteStatus != "" && f.SourceRef != "" && !f.ObservedAt.IsZero() && f.ObservedAt.Equal(f.ObservedAt.UTC())
}
func (s *Store) Append(scope tenancy.Scope, fact StatusFact) error {
	if !scope.Valid() || !validFact(fact) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(scope)
	for _, x := range s.tenants[k] {
		if x.CodeFingerprint == fact.CodeFingerprint && x.SourceRef == fact.SourceRef && x.ObservedAt.Equal(fact.ObservedAt) {
			return nil
		}
	}
	s.tenants[k] = append(s.tenants[k], fact)
	return nil
}
func (s *Store) Reconcile(scope tenancy.Scope, id, fp, expected string, at time.Time) (Reconciliation, error) {
	if !scope.Valid() || id == "" || len(fp) != 64 || expected == "" || at.IsZero() {
		return Reconciliation{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	facts := s.tenants[key(scope)]
	var latest *StatusFact
	for i := range facts {
		f := &facts[i]
		if f.CodeFingerprint == fp && (latest == nil || f.ObservedAt.After(latest.ObservedAt)) {
			latest = f
		}
	}
	if latest == nil {
		return Reconciliation{}, ErrInvalid
	}
	out := "match"
	if latest.RemoteStatus != expected {
		out = "drift"
	}
	r := Reconciliation{id, fp, expected, latest.RemoteStatus, out, at.UTC()}
	s.reconciliations[key(scope)] = append(s.reconciliations[key(scope)], r)
	return r, nil
}
func (s *Store) Facts(scope tenancy.Scope) []StatusFact {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StatusFact(nil), s.tenants[key(scope)]...)
}
