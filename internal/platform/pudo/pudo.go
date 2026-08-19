// Package pudo implements own/external pickup-point registry, capacity and order lifecycle.
package pudo

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"time"
)

var (
	ErrInvalid    = errors.New("pudo: invalid value")
	ErrTransition = errors.New("pudo: invalid transition")
	ErrCapacity   = errors.New("pudo: capacity exceeded")
)

type PointKind string

const (
	Own      PointKind = "own"
	External PointKind = "external"
)

type Point struct {
	ID, ExternalRef, Name string
	Kind                  PointKind
	Capacity              int64
	Active                bool
	UpdatedAt             time.Time
}
type State string

const (
	Created       State = "created"
	Arrived       State = "arrived"
	Ready         State = "ready"
	Issued        State = "issued"
	Expired       State = "expired"
	ReturnPending State = "return_pending"
	Returned      State = "returned"
)

type Order struct {
	ID, PointID, ExternalOrderRef string
	State                         State
	PaymentRef, FiscalRef         string
	ExpiresAt, UpdatedAt          time.Time
	Version                       int64
}
type Hook interface {
	OnIssued(context.Context, tenancy.Scope, Order) error
	OnReturn(context.Context, tenancy.Scope, Order) error
}
type ReportSink interface {
	Record(context.Context, tenancy.Scope, Order, string) error
}
type Service struct {
	mu      sync.Mutex
	points  map[string]map[string]Point
	orders  map[string]map[string]Order
	hook    Hook
	reports ReportSink
}

func NewService(h Hook, r ReportSink) *Service {
	return &Service{points: map[string]map[string]Point{}, orders: map[string]map[string]Order{}, hook: h, reports: r}
}
func kk(sc tenancy.Scope) string {
	return sc.OrganizationID().String() + "/" + sc.WorkspaceID().String()
}
func (s *Service) Register(sc tenancy.Scope, p Point) error {
	if !sc.Valid() || p.ID == "" || p.Name == "" || (p.Kind != Own && p.Kind != External) || (p.Kind == External && p.ExternalRef == "") || p.Capacity < 1 || p.UpdatedAt.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := kk(sc)
	if s.points[k] == nil {
		s.points[k] = map[string]Point{}
	}
	s.points[k][p.ID] = p
	return nil
}
func (s *Service) Create(sc tenancy.Scope, o Order) (Order, error) {
	if !sc.Valid() || o.ID == "" || o.PointID == "" || o.ExternalOrderRef == "" || o.State != Created || o.Version != 1 || o.ExpiresAt.IsZero() || o.UpdatedAt.IsZero() {
		return Order{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := kk(sc)
	p, ok := s.points[k][o.PointID]
	if !ok || !p.Active {
		return Order{}, ErrInvalid
	}
	if s.orders[k] == nil {
		s.orders[k] = map[string]Order{}
	}
	if old, ok := s.orders[k][o.ID]; ok {
		return old, nil
	}
	count := int64(0)
	for _, x := range s.orders[k] {
		if x.PointID == o.PointID && x.State != Issued && x.State != Returned {
			count++
		}
	}
	if count >= p.Capacity {
		return Order{}, ErrCapacity
	}
	s.orders[k][o.ID] = o
	return o, nil
}
func allowed(a, b State) bool {
	switch a {
	case Created:
		return b == Arrived
	case Arrived:
		return b == Ready
	case Ready:
		return b == Issued || b == Expired
	case Expired:
		return b == ReturnPending
	case ReturnPending:
		return b == Returned
	}
	return false
}
func (s *Service) Transition(ctx context.Context, sc tenancy.Scope, id string, to State, at time.Time) (Order, error) {
	if !sc.Valid() || id == "" || at.IsZero() {
		return Order{}, ErrInvalid
	}
	s.mu.Lock()
	k := kk(sc)
	o, ok := s.orders[k][id]
	if !ok || !allowed(o.State, to) {
		s.mu.Unlock()
		return Order{}, ErrTransition
	}
	candidate := o
	candidate.State = to
	candidate.Version++
	candidate.UpdatedAt = at.UTC()
	// Issue/return hooks are critical regulated side effects. The state is not
	// advanced when they fail, avoiding a locally completed pickup without the
	// required payment/fiscal/logistics action.
	if s.hook != nil && to == Issued {
		if e := s.hook.OnIssued(ctx, sc, candidate); e != nil {
			s.mu.Unlock()
			return Order{}, e
		}
	}
	if s.hook != nil && to == Returned {
		if e := s.hook.OnReturn(ctx, sc, candidate); e != nil {
			s.mu.Unlock()
			return Order{}, e
		}
	}
	s.orders[k][id] = candidate
	s.mu.Unlock()
	if s.reports != nil {
		if e := s.reports.Record(ctx, sc, candidate, string(to)); e != nil {
			return candidate, e
		}
	}
	return candidate, nil
}
func (s *Service) ReconcileExpiry(ctx context.Context, sc tenancy.Scope, now time.Time) (int, error) {
	s.mu.Lock()
	ids := []string{}
	for id, o := range s.orders[kk(sc)] {
		if o.State == Ready && !now.Before(o.ExpiresAt) {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		if _, e := s.Transition(ctx, sc, id, Expired, now); e != nil {
			return 0, e
		}
	}
	return len(ids), nil
}
