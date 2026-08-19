package pudo

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

type hooks struct{ issued, returned int }

func (h *hooks) OnIssued(context.Context, tenancy.Scope, Order) error { h.issued++; return nil }
func (h *hooks) OnReturn(context.Context, tenancy.Scope, Order) error { h.returned++; return nil }
func TestStateMachineCapacityHooksAndExpiry(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	h := &hooks{}
	s := NewService(h, nil)
	now := time.Now().UTC()
	_ = s.Register(sc, Point{ID: "p1", Name: "Point", Kind: Own, Capacity: 1, Active: true, UpdatedAt: now})
	o, _ := s.Create(sc, Order{ID: "o1", PointID: "p1", ExternalOrderRef: "e1", State: Created, Version: 1, ExpiresAt: now.Add(time.Hour), UpdatedAt: now})
	if _, e := s.Create(sc, Order{ID: "o2", PointID: "p1", ExternalOrderRef: "e2", State: Created, Version: 1, ExpiresAt: now.Add(time.Hour), UpdatedAt: now}); e != ErrCapacity {
		t.Fatalf("%v", e)
	}
	o, _ = s.Transition(context.Background(), sc, o.ID, Arrived, now)
	o, _ = s.Transition(context.Background(), sc, o.ID, Ready, now)
	o, _ = s.Transition(context.Background(), sc, o.ID, Issued, now)
	if o.State != Issued || h.issued != 1 {
		t.Fatal("issue hook")
	}
}

type failingHooks struct{}

func (failingHooks) OnIssued(context.Context, tenancy.Scope, Order) error {
	return errors.New("hook failed")
}
func (failingHooks) OnReturn(context.Context, tenancy.Scope, Order) error { return nil }

type reports struct{ events []string }

func (r *reports) Record(_ context.Context, _ tenancy.Scope, _ Order, event string) error {
	r.events = append(r.events, event)
	return nil
}

func TestDuplicateCreateIsIdempotentBeforeCapacityCheck(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Now().UTC()
	s := NewService(nil, nil)
	if err := s.Register(sc, Point{ID: "p1", Name: "Point", Kind: Own, Capacity: 1, Active: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	o := Order{ID: "o1", PointID: "p1", ExternalOrderRef: "e1", State: Created, Version: 1, ExpiresAt: now.Add(time.Hour), UpdatedAt: now}
	first, err := s.Create(sc, o)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(sc, o)
	if err != nil || second != first {
		t.Fatalf("%+v %v", second, err)
	}
}

func TestIssueHookFailureDoesNotAdvanceState(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Now().UTC()
	s := NewService(failingHooks{}, nil)
	_ = s.Register(sc, Point{ID: "p1", Name: "Point", Kind: Own, Capacity: 1, Active: true, UpdatedAt: now})
	o, _ := s.Create(sc, Order{ID: "o1", PointID: "p1", ExternalOrderRef: "e1", State: Created, Version: 1, ExpiresAt: now.Add(time.Hour), UpdatedAt: now})
	o, _ = s.Transition(context.Background(), sc, o.ID, Arrived, now)
	o, _ = s.Transition(context.Background(), sc, o.ID, Ready, now)
	if _, err := s.Transition(context.Background(), sc, o.ID, Issued, now); err == nil {
		t.Fatal("expected critical hook failure")
	}
	// A second Ready->Expired transition proves the failed issue did not advance state.
	x, err := s.Transition(context.Background(), sc, o.ID, Expired, now.Add(time.Hour))
	if err != nil || x.State != Expired {
		t.Fatalf("%+v %v", x, err)
	}
}

func TestExpiryReturnAndReportHooks(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Now().UTC()
	h := &hooks{}
	r := &reports{}
	s := NewService(h, r)
	_ = s.Register(sc, Point{ID: "p1", Name: "External", Kind: External, ExternalRef: "remote:point", Capacity: 2, Active: true, UpdatedAt: now})
	o, _ := s.Create(sc, Order{ID: "o1", PointID: "p1", ExternalOrderRef: "e1", State: Created, Version: 1, ExpiresAt: now.Add(time.Minute), UpdatedAt: now})
	o, _ = s.Transition(context.Background(), sc, o.ID, Arrived, now)
	o, _ = s.Transition(context.Background(), sc, o.ID, Ready, now)
	if n, err := s.ReconcileExpiry(context.Background(), sc, now.Add(2*time.Minute)); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	o, _ = s.Transition(context.Background(), sc, o.ID, ReturnPending, now.Add(3*time.Minute))
	o, err := s.Transition(context.Background(), sc, o.ID, Returned, now.Add(4*time.Minute))
	if err != nil || o.State != Returned || h.returned != 1 {
		t.Fatalf("%+v %v", o, err)
	}
	if len(r.events) != 5 {
		t.Fatalf("expected 5 report events, got %d", len(r.events))
	}
}
