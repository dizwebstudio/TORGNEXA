// Package wmsexecution implements scanner-friendly idempotent warehouse workflows.
package wmsexecution

import (
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"time"
)

var (
	ErrInvalid      = errors.New("wmsexecution: invalid value")
	ErrInvalidState = errors.New("wmsexecution: invalid state transition")
)

type TaskType string

const (
	TaskReceiving       TaskType = "receiving"
	TaskPutAway         TaskType = "put_away"
	TaskPick            TaskType = "pick"
	TaskPack            TaskType = "pack"
	TaskCycleCount      TaskType = "cycle_count"
	TaskTransfer        TaskType = "transfer"
	TaskReturnReceiving TaskType = "return_receiving"
)

func (t TaskType) Valid() bool {
	switch t {
	case TaskReceiving, TaskPutAway, TaskPick, TaskPack, TaskCycleCount, TaskTransfer, TaskReturnReceiving:
		return true
	}
	return false
}

type State string

const (
	StatePending    State = "pending"
	StateInProgress State = "in_progress"
	StateCompleted  State = "completed"
	StateCancelled  State = "cancelled"
	StateException  State = "exception"
)

type Task struct {
	ID                                                   string
	Type                                                 TaskType
	State                                                State
	WarehouseID, SourceLocationID, TargetLocationID, SKU string
	ExpectedQuantity, ProcessedQuantity                  int64
	Version                                              int64
	UpdatedAt                                            time.Time
}
type ScanCommand struct {
	TaskID, Barcode, LocationCode, IdempotencyKey string
	Quantity                                      int64
	At                                            time.Time
}
type Event struct {
	ID, TaskID, IdempotencyKey, Kind string
	Quantity                         int64
	At                               time.Time
}
type executionTenant struct {
	tasks  map[string]Task
	seen   map[string]struct{}
	events []Event
}
type Service struct {
	mu      sync.Mutex
	tenants map[string]*executionTenant
}

func NewService() *Service { return &Service{tenants: map[string]*executionTenant{}} }
func exScope(scope tenancy.Scope) string {
	return scope.OrganizationID().String() + "/" + scope.WorkspaceID().String()
}
func (s *Service) state(scope tenancy.Scope) *executionTenant {
	k := exScope(scope)
	st := s.tenants[k]
	if st == nil {
		st = &executionTenant{tasks: map[string]Task{}, seen: map[string]struct{}{}}
		s.tenants[k] = st
	}
	return st
}
func (s *Service) Create(scope tenancy.Scope, t Task) error {
	if !scope.Valid() || t.ID == "" || !t.Type.Valid() || t.State != StatePending || t.WarehouseID == "" || t.SKU == "" || t.ExpectedQuantity <= 0 || t.Version != 1 || t.UpdatedAt.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(scope)
	if _, ok := st.tasks[t.ID]; ok {
		return ErrInvalid
	}
	st.tasks[t.ID] = t
	return nil
}
func (s *Service) Scan(scope tenancy.Scope, c ScanCommand) (Task, error) {
	if !scope.Valid() || c.TaskID == "" || c.Barcode == "" || c.IdempotencyKey == "" || c.Quantity <= 0 || c.At.IsZero() {
		return Task{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(scope)
	t, ok := st.tasks[c.TaskID]
	if !ok {
		return Task{}, ErrInvalid
	}
	if _, ok := st.seen[c.IdempotencyKey]; ok {
		return t, nil
	}
	if t.State == StatePending {
		t.State = StateInProgress
	} else if t.State != StateInProgress {
		return Task{}, ErrInvalidState
	}
	if t.ProcessedQuantity+c.Quantity > t.ExpectedQuantity {
		return Task{}, ErrInvalid
	}
	t.ProcessedQuantity += c.Quantity
	if t.ProcessedQuantity == t.ExpectedQuantity {
		t.State = StateCompleted
	}
	t.Version++
	t.UpdatedAt = c.At.UTC()
	st.tasks[t.ID] = t
	st.seen[c.IdempotencyKey] = struct{}{}
	st.events = append(st.events, Event{c.IdempotencyKey, t.ID, c.IdempotencyKey, "scan_applied", c.Quantity, c.At.UTC()})
	return t, nil
}
func (s *Service) Events(scope tenancy.Scope) []Event {
	if !scope.Valid() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tenants[exScope(scope)]
	if st == nil {
		return nil
	}
	return append([]Event(nil), st.events...)
}
