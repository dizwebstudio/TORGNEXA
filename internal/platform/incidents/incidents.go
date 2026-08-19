// Package incidents implements tenant-scoped incident state and executable safe runbooks.
package incidents

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalid = errors.New("incidents: invalid value")
var ErrNotFound = errors.New("incidents: not found")

type Severity string

const (
	SeverityP1 Severity = "p1"
	SeverityP2 Severity = "p2"
	SeverityP3 Severity = "p3"
	SeverityP4 Severity = "p4"
)

func (s Severity) Valid() bool {
	return s == SeverityP1 || s == SeverityP2 || s == SeverityP3 || s == SeverityP4
}

type State string

const (
	StateOpen         State = "open"
	StateAcknowledged State = "acknowledged"
	StateMitigated    State = "mitigated"
	StateResolved     State = "resolved"
)

func (s State) Valid() bool {
	return s == StateOpen || s == StateAcknowledged || s == StateMitigated || s == StateResolved
}

type Signal struct {
	Code        string
	Severity    Severity
	Source      string
	ObservedAt  time.Time
	EvidenceRef string
}

func (s Signal) Validate() error {
	if s.Code == "" || !s.Severity.Valid() || s.Source == "" || s.ObservedAt.IsZero() || s.ObservedAt.Location() != time.UTC || s.EvidenceRef == "" {
		return ErrInvalid
	}
	return nil
}

type Step struct{ ID, SafeAction, Validation, Rollback, Evidence string }

func (s Step) Validate() error {
	if s.ID == "" || s.SafeAction == "" || s.Validation == "" || s.Rollback == "" || s.Evidence == "" {
		return ErrInvalid
	}
	return nil
}

type Runbook struct {
	ID, Title    string
	TriggerCodes []string
	Steps        []Step
}

func (r Runbook) Validate() error {
	if r.ID == "" || r.Title == "" || len(r.TriggerCodes) == 0 || len(r.Steps) == 0 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, c := range r.TriggerCodes {
		if strings.TrimSpace(c) == "" || seen[c] {
			return ErrInvalid
		}
		seen[c] = true
	}
	for _, s := range r.Steps {
		if s.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

type Incident struct {
	ID, DedupeKey, RunbookID string
	Severity                 Severity
	State                    State
	OpenedAt, UpdatedAt      time.Time
	LastEvidenceRef          string
	Occurrences              int
}

func (i Incident) Validate() error {
	if i.ID == "" || i.DedupeKey == "" || i.RunbookID == "" || !i.Severity.Valid() || !i.State.Valid() || i.OpenedAt.IsZero() || i.UpdatedAt.Before(i.OpenedAt) || i.Occurrences < 1 {
		return ErrInvalid
	}
	return nil
}

type Evidence struct {
	IncidentID, StepID, ActionRef, ValidationRef, RollbackRef string
	OccurredAt                                                time.Time
}

type Executor interface {
	Action(context.Context, tenancy.Scope, string) (string, error)
	Validate(context.Context, tenancy.Scope, string) (string, error)
	Rollback(context.Context, tenancy.Scope, string) (string, error)
}

type Store struct {
	mu       sync.Mutex
	byScope  map[string]map[string]Incident
	evidence map[string][]Evidence
}

func NewStore() *Store {
	return &Store{byScope: map[string]map[string]Incident{}, evidence: map[string][]Evidence{}}
}
func sk(s tenancy.Scope) string { return s.OrganizationID().String() + "/" + s.WorkspaceID().String() }
func (s *Store) Open(scope tenancy.Scope, in Incident) (Incident, error) {
	if !scope.Valid() || in.Validate() != nil {
		return Incident{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := sk(scope)
	if s.byScope[k] == nil {
		s.byScope[k] = map[string]Incident{}
	}
	if old, ok := s.byScope[k][in.DedupeKey]; ok && old.State != StateResolved {
		old.Occurrences++
		old.UpdatedAt = in.UpdatedAt
		if severityRank(in.Severity) < severityRank(old.Severity) {
			old.Severity = in.Severity
		}
		s.byScope[k][in.DedupeKey] = old
		return old, nil
	}
	s.byScope[k][in.DedupeKey] = in
	return in, nil
}
func severityRank(s Severity) int {
	switch s {
	case SeverityP1:
		return 1
	case SeverityP2:
		return 2
	case SeverityP3:
		return 3
	default:
		return 4
	}
}
func (s *Store) Record(scope tenancy.Scope, e Evidence) error {
	if !scope.Valid() || e.IncidentID == "" || e.StepID == "" || e.OccurredAt.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evidence[sk(scope)] = append(s.evidence[sk(scope)], e)
	return nil
}
func (s *Store) Evidence(scope tenancy.Scope) []Evidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Evidence(nil), s.evidence[sk(scope)]...)
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.Before(out[j].OccurredAt) })
	return out
}

type Engine struct {
	exec  Executor
	store *Store
	now   func() time.Time
}

func NewEngine(exec Executor, store *Store, now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{exec: exec, store: store, now: now}
}
func (e *Engine) Execute(ctx context.Context, scope tenancy.Scope, incident Incident, rb Runbook) error {
	if ctx == nil || !scope.Valid() || e == nil || e.exec == nil || e.store == nil || incident.Validate() != nil || rb.Validate() != nil || incident.RunbookID != rb.ID {
		return ErrInvalid
	}
	completed := make([]Step, 0, len(rb.Steps))
	for _, step := range rb.Steps {
		a, err := e.exec.Action(ctx, scope, step.SafeAction)
		if err != nil {
			return e.rollback(ctx, scope, incident, completed)
		}
		v, err := e.exec.Validate(ctx, scope, step.Validation)
		if err != nil {
			completed = append(completed, step)
			_ = e.store.Record(scope, Evidence{IncidentID: incident.ID, StepID: step.ID, ActionRef: a, ValidationRef: v, OccurredAt: e.now().UTC()})
			return e.rollback(ctx, scope, incident, completed)
		}
		if err := e.store.Record(scope, Evidence{IncidentID: incident.ID, StepID: step.ID, ActionRef: a, ValidationRef: v, OccurredAt: e.now().UTC()}); err != nil {
			return err
		}
		completed = append(completed, step)
	}
	return nil
}
func (e *Engine) rollback(ctx context.Context, scope tenancy.Scope, incident Incident, steps []Step) error {
	for i := len(steps) - 1; i >= 0; i-- {
		ref, _ := e.exec.Rollback(ctx, scope, steps[i].Rollback)
		_ = e.store.Record(scope, Evidence{IncidentID: incident.ID, StepID: steps[i].ID, RollbackRef: ref, OccurredAt: e.now().UTC()})
	}
	return errors.New("incidents: runbook execution failed and rollback attempted")
}
