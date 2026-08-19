// Package approval defines TORGNEXA's provider-neutral approval policy and
// workflow state machine for sensitive mutations.
package approval

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

var (
	ErrInvalid      = errors.New("approval: invalid value")
	ErrDenied       = errors.New("approval: operation denied by policy")
	ErrNotEligible  = errors.New("approval: actor is not eligible for stage")
	ErrSeparation   = errors.New("approval: separation of duties violation")
	ErrAlreadyVoted = errors.New("approval: actor already decided this stage")
	ErrInvalidState = errors.New("approval: invalid state transition")
	ErrExpired      = errors.New("approval: request expired")
	ErrConflict     = errors.New("approval: optimistic version conflict")
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)

// RiskClass mirrors the canonical audit risk vocabulary.
type RiskClass string

const (
	RiskRead               RiskClass = "read"
	RiskWriteSafe          RiskClass = "write_safe"
	RiskWriteSensitive     RiskClass = "write_sensitive"
	RiskLegallySignificant RiskClass = "legally_significant"
)

func (r RiskClass) Valid() bool {
	return r == RiskRead || r == RiskWriteSafe || r == RiskWriteSensitive || r == RiskLegallySignificant
}
func (r RiskClass) rank() int {
	switch r {
	case RiskRead:
		return 1
	case RiskWriteSafe:
		return 2
	case RiskWriteSensitive:
		return 3
	case RiskLegallySignificant:
		return 4
	}
	return 0
}

// Decision is the pre-execution authorization result.
type Decision string

const (
	DecisionAllow   Decision = "allow"
	DecisionDeny    Decision = "deny"
	DecisionRequire Decision = "approval_required"
)

// GateDecision fails closed for sensitive writes when no matching policy exists.
func GateDecision(risk RiskClass, matchingPolicy bool) Decision {
	if !risk.Valid() {
		return DecisionDeny
	}
	if risk == RiskRead || risk == RiskWriteSafe {
		return DecisionAllow
	}
	if !matchingPolicy {
		return DecisionDeny
	}
	return DecisionRequire
}

type RequestState string

const (
	StatePending   RequestState = "pending"
	StateApproved  RequestState = "approved"
	StateRejected  RequestState = "rejected"
	StateExpired   RequestState = "expired"
	StateCancelled RequestState = "cancelled"
	StateExecuting RequestState = "executing"
	StateCompleted RequestState = "completed"
	StateFailed    RequestState = "failed"
)

func (s RequestState) Valid() bool {
	switch s {
	case StatePending, StateApproved, StateRejected, StateExpired, StateCancelled, StateExecuting, StateCompleted, StateFailed:
		return true
	}
	return false
}
func (s RequestState) Terminal() bool {
	return s == StateRejected || s == StateExpired || s == StateCancelled || s == StateCompleted || s == StateFailed
}

type Vote string

const (
	VoteApprove Vote = "approve"
	VoteReject  Vote = "reject"
)

func (v Vote) Valid() bool { return v == VoteApprove || v == VoteReject }

type Stage struct {
	Number            uint16   `json:"number"`
	Name              string   `json:"name"`
	RequiredApprovals uint16   `json:"required_approvals"`
	EligibleScopes    []string `json:"eligible_scopes"`
}

func (s Stage) Validate() error {
	if s.Number == 0 || !tokenPattern.MatchString(s.Name) || s.RequiredApprovals == 0 || s.RequiredApprovals > 32 || len(s.EligibleScopes) == 0 || len(s.EligibleScopes) > 64 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, scope := range s.EligibleScopes {
		if !tokenPattern.MatchString(scope) {
			return ErrInvalid
		}
		if _, ok := seen[scope]; ok {
			return ErrInvalid
		}
		seen[scope] = struct{}{}
	}
	return nil
}

type Policy struct {
	ID, OrganizationID, WorkspaceID string
	Name, Action, ResourceType      string
	MinimumRisk                     RiskClass
	Version                         int64
	RequestTTL                      time.Duration
	EscalateAfter                   time.Duration
	SeparationOfDuties              bool
	Stages                          []Stage
	Active                          bool
}

func (p Policy) Validate() error {
	if !validID(p.ID) || !validID(p.OrganizationID) || !validID(p.WorkspaceID) || !tokenPattern.MatchString(p.Name) || !tokenPattern.MatchString(p.Action) || !tokenPattern.MatchString(p.ResourceType) || !p.MinimumRisk.Valid() || p.Version < 1 || p.RequestTTL < time.Minute || p.RequestTTL > 30*24*time.Hour || p.EscalateAfter < 0 || p.EscalateAfter >= p.RequestTTL || len(p.Stages) == 0 || len(p.Stages) > 16 {
		return ErrInvalid
	}
	for i, s := range p.Stages {
		if s.Validate() != nil || int(s.Number) != i+1 {
			return ErrInvalid
		}
	}
	return nil
}
func (p Policy) Matches(action, resourceType string, risk RiskClass) bool {
	return p.Active && p.Validate() == nil && p.Action == action && p.ResourceType == resourceType && risk.Valid() && risk.rank() >= p.MinimumRisk.rank()
}

type Actor struct {
	ID     string
	Scopes []string
}

func (a Actor) Validate() error {
	if !validText(a.ID, 256) || len(a.Scopes) > 128 {
		return ErrInvalid
	}
	for _, s := range a.Scopes {
		if !tokenPattern.MatchString(s) {
			return ErrInvalid
		}
	}
	return nil
}
func (a Actor) Eligible(stage Stage) bool {
	have := map[string]struct{}{}
	for _, s := range a.Scopes {
		have[s] = struct{}{}
	}
	for _, s := range stage.EligibleScopes {
		if _, ok := have[s]; ok {
			return true
		}
	}
	return false
}

type Request struct {
	ID, OrganizationID, WorkspaceID                                      string
	PolicyID                                                             string
	PolicyVersion                                                        int64
	RequesterID, Source, Action, ResourceType, ResourceID, CorrelationID string
	Risk                                                                 RiskClass
	State                                                                RequestState
	CurrentStage                                                         uint16
	ExpiresAt                                                            time.Time
	NextEscalationAt                                                     *time.Time
	EscalationCount                                                      uint32
	Version                                                              int64
	RequestedAt                                                          time.Time
	ApprovedAt, RejectedAt, ExecutionStartedAt, CompletedAt              *time.Time
	FailureCode                                                          string
}

func (r Request) Validate() error {
	if !validID(r.ID) || !validID(r.OrganizationID) || !validID(r.WorkspaceID) || !validID(r.PolicyID) || r.PolicyVersion < 1 || !validText(r.RequesterID, 256) || !tokenPattern.MatchString(r.Source) || !tokenPattern.MatchString(r.Action) || !tokenPattern.MatchString(r.ResourceType) || !validText(r.ResourceID, 512) || !validText(r.CorrelationID, 256) || !r.Risk.Valid() || !r.State.Valid() || r.CurrentStage == 0 || r.Version < 1 || r.RequestedAt.IsZero() || r.ExpiresAt.IsZero() || !r.RequestedAt.Equal(r.RequestedAt.UTC()) || !r.ExpiresAt.Equal(r.ExpiresAt.UTC()) || !r.ExpiresAt.After(r.RequestedAt) {
		return ErrInvalid
	}
	if r.NextEscalationAt != nil && (!r.NextEscalationAt.Equal(r.NextEscalationAt.UTC()) || !r.NextEscalationAt.Before(r.ExpiresAt)) {
		return ErrInvalid
	}
	if r.FailureCode != "" && !tokenPattern.MatchString(r.FailureCode) {
		return ErrInvalid
	}
	return nil
}

type DecisionRecord struct {
	ID, RequestID, ActorID string
	Stage                  uint16
	Vote                   Vote
	ActorScopes            []string
	Comment                string
	DecidedAt              time.Time
}

func (d DecisionRecord) Validate() error { return validateDecision(d, true) }
func validateDecision(d DecisionRecord, persisted bool) error {
	if (persisted && !validID(d.ID)) || !validID(d.RequestID) || !validText(d.ActorID, 256) || d.Stage == 0 || !d.Vote.Valid() || len(d.ActorScopes) == 0 || d.DecidedAt.IsZero() || !d.DecidedAt.Equal(d.DecidedAt.UTC()) || !safeComment(d.Comment) {
		return ErrInvalid
	}
	for i, scope := range d.ActorScopes {
		if !tokenPattern.MatchString(scope) || (i > 0 && d.ActorScopes[i-1] >= scope) {
			return ErrInvalid
		}
	}
	return nil
}

// SanitizeComment prevents credentials from being stored in approval evidence.
func SanitizeComment(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if !validText(v, 1024) {
		return "", ErrInvalid
	}
	lower := strings.ToLower(v)
	if secrets.SensitiveString(v) || strings.Contains(lower, "authorization:") || strings.Contains(lower, "bearer ") || strings.Contains(lower, "password:") || strings.Contains(lower, "secret:") || strings.Contains(lower, "token:") {
		return secrets.RedactedValue, nil
	}
	redacted := secrets.RedactText(v)
	if len(redacted) > 1024 {
		return "", ErrInvalid
	}
	return redacted, nil
}
func safeComment(v string) bool {
	if v == "" {
		return true
	}
	s, err := SanitizeComment(v)
	return err == nil && s == v
}

func NewRequest(policy Policy, id, requester, source, resourceID, correlationID string, risk RiskClass, now time.Time) (Request, error) {
	if policy.Validate() != nil || !policy.Matches(policy.Action, policy.ResourceType, risk) || !validID(id) || !validText(requester, 256) || !tokenPattern.MatchString(source) || !validText(resourceID, 512) || !validText(correlationID, 256) || now.IsZero() {
		return Request{}, ErrInvalid
	}
	now = now.UTC()
	expires := now.Add(policy.RequestTTL)
	var escalation *time.Time
	if policy.EscalateAfter > 0 {
		t := now.Add(policy.EscalateAfter)
		escalation = &t
	}
	r := Request{ID: id, OrganizationID: policy.OrganizationID, WorkspaceID: policy.WorkspaceID, PolicyID: policy.ID, PolicyVersion: policy.Version, RequesterID: requester, Source: source, Action: policy.Action, ResourceType: policy.ResourceType, ResourceID: resourceID, CorrelationID: correlationID, Risk: risk, State: StatePending, CurrentStage: 1, ExpiresAt: expires, NextEscalationAt: escalation, Version: 1, RequestedAt: now}
	return r, r.Validate()
}

// ApplyDecision evaluates one immutable approver vote against current evidence.
func ApplyDecision(req Request, policy Policy, prior []DecisionRecord, actor Actor, decisionID string, vote Vote, comment string, now time.Time) (Request, DecisionRecord, error) {
	if req.Validate() != nil || policy.Validate() != nil || actor.Validate() != nil || !validID(decisionID) || !vote.Valid() || now.IsZero() {
		return Request{}, DecisionRecord{}, ErrInvalid
	}
	now = now.UTC()
	if req.State != StatePending {
		return Request{}, DecisionRecord{}, ErrInvalidState
	}
	if !now.Before(req.ExpiresAt) {
		return Request{}, DecisionRecord{}, ErrExpired
	}
	if int(req.CurrentStage) > len(policy.Stages) || req.PolicyID != policy.ID || req.PolicyVersion != policy.Version {
		return Request{}, DecisionRecord{}, ErrInvalid
	}
	stage := policy.Stages[req.CurrentStage-1]
	if !actor.Eligible(stage) {
		return Request{}, DecisionRecord{}, ErrNotEligible
	}
	if policy.SeparationOfDuties && actor.ID == req.RequesterID {
		return Request{}, DecisionRecord{}, ErrSeparation
	}
	approvals := 0
	for _, d := range prior {
		if d.Validate() != nil || d.RequestID != req.ID {
			return Request{}, DecisionRecord{}, ErrInvalid
		}
		if d.Stage == req.CurrentStage {
			if d.ActorID == actor.ID {
				return Request{}, DecisionRecord{}, ErrAlreadyVoted
			}
			if d.Vote == VoteApprove {
				approvals++
			}
			if d.Vote == VoteReject {
				return Request{}, DecisionRecord{}, ErrInvalidState
			}
		}
	}
	safe, err := SanitizeComment(comment)
	if err != nil {
		return Request{}, DecisionRecord{}, err
	}
	rec := DecisionRecord{ID: decisionID, RequestID: req.ID, ActorID: actor.ID, Stage: req.CurrentStage, Vote: vote, ActorScopes: CanonicalScopes(actor.Scopes), Comment: safe, DecidedAt: now}
	out := req
	out.Version++
	if vote == VoteReject {
		out.State = StateRejected
		out.RejectedAt = timePtr(now)
		out.NextEscalationAt = nil
		return out, rec, nil
	}
	approvals++
	if approvals >= int(stage.RequiredApprovals) {
		if int(req.CurrentStage) == len(policy.Stages) {
			out.State = StateApproved
			out.ApprovedAt = timePtr(now)
			out.NextEscalationAt = nil
		} else {
			out.CurrentStage++
			if policy.EscalateAfter > 0 {
				t := now.Add(policy.EscalateAfter)
				if t.Before(out.ExpiresAt) {
					out.NextEscalationAt = &t
				} else {
					out.NextEscalationAt = nil
				}
			}
		}
	}
	return out, rec, nil
}

func Expire(req Request, now time.Time) (Request, error) {
	if req.Validate() != nil || now.IsZero() {
		return Request{}, ErrInvalid
	}
	now = now.UTC()
	if req.State != StatePending && req.State != StateApproved {
		return Request{}, ErrInvalidState
	}
	if now.Before(req.ExpiresAt) {
		return Request{}, ErrInvalidState
	}
	out := req
	out.State = StateExpired
	out.Version++
	out.NextEscalationAt = nil
	return out, nil
}
func Escalate(req Request, policy Policy, now time.Time) (Request, error) {
	if req.Validate() != nil || policy.Validate() != nil || now.IsZero() {
		return Request{}, ErrInvalid
	}
	now = now.UTC()
	if req.State != StatePending || req.NextEscalationAt == nil || now.Before(*req.NextEscalationAt) || !now.Before(req.ExpiresAt) {
		return Request{}, ErrInvalidState
	}
	out := req
	out.Version++
	out.EscalationCount++
	if policy.EscalateAfter > 0 {
		n := now.Add(policy.EscalateAfter)
		if n.Before(out.ExpiresAt) {
			out.NextEscalationAt = &n
		} else {
			out.NextEscalationAt = nil
		}
	}
	return out, nil
}
func BeginExecution(req Request, now time.Time) (Request, error) {
	if req.Validate() != nil || now.IsZero() {
		return Request{}, ErrInvalid
	}
	if req.State != StateApproved {
		return Request{}, ErrInvalidState
	}
	now = now.UTC()
	if !now.Before(req.ExpiresAt) {
		return Request{}, ErrExpired
	}
	out := req
	out.State = StateExecuting
	out.Version++
	out.ExecutionStartedAt = timePtr(now)
	return out, nil
}
func CompleteExecution(req Request, success bool, failureCode string, now time.Time) (Request, error) {
	if req.Validate() != nil || now.IsZero() {
		return Request{}, ErrInvalid
	}
	if req.State != StateExecuting {
		return Request{}, ErrInvalidState
	}
	now = now.UTC()
	out := req
	out.Version++
	out.CompletedAt = timePtr(now)
	if success {
		if failureCode != "" {
			return Request{}, ErrInvalid
		}
		out.State = StateCompleted
	} else {
		if !tokenPattern.MatchString(failureCode) {
			return Request{}, ErrInvalid
		}
		out.State = StateFailed
		out.FailureCode = failureCode
	}
	return out, nil
}

func timePtr(t time.Time) *time.Time { return &t }
func validText(v string, max int) bool {
	return v != "" && v == strings.TrimSpace(v) && utf8.ValidString(v) && utf8.RuneCountInString(v) <= max && !strings.ContainsAny(v, "\r\n\x00")
}
func validID(v string) bool {
	if len(v) != 26 && len(v) != 36 {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}
func CanonicalScopes(scopes []string) []string {
	out := append([]string(nil), scopes...)
	sort.Strings(out)
	return out
}
func (p Policy) String() string { return fmt.Sprintf("%s@%d", p.ID, p.Version) }

// Mutation carries caller-generated immutable evidence identifiers so retries
// can remain idempotent across audit/outbox boundaries.
type Mutation struct {
	AuditID, EventID                                     string
	ActorID, Source, CorrelationID, CausationID, TraceID string
	OccurredAt                                           time.Time
}

func (m Mutation) Validate() error {
	if !validID(m.AuditID) || !validID(m.EventID) || !validText(m.ActorID, 256) || !tokenPattern.MatchString(m.Source) || !validText(m.CorrelationID, 256) || m.OccurredAt.IsZero() || !m.OccurredAt.Equal(m.OccurredAt.UTC()) {
		return ErrInvalid
	}
	for _, v := range []string{m.CausationID, m.TraceID} {
		if v != "" && !tokenPattern.MatchString(v) {
			return ErrInvalid
		}
	}
	return nil
}

type RequestCommand struct {
	RequestID, ResourceID string
	Risk                  RiskClass
	Mutation              Mutation
}

func (c RequestCommand) Validate() error {
	if !validID(c.RequestID) || !validText(c.ResourceID, 512) || !c.Risk.Valid() || c.Mutation.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

type DecideCommand struct {
	RequestID, DecisionID string
	ExpectedVersion       int64
	Actor                 Actor
	Vote                  Vote
	Comment               string
	Mutation              Mutation
}

func (c DecideCommand) Validate() error {
	if !validID(c.RequestID) || !validID(c.DecisionID) || c.ExpectedVersion < 1 || c.Actor.Validate() != nil || !c.Vote.Valid() || c.Mutation.Validate() != nil {
		return ErrInvalid
	}
	_, e := SanitizeComment(c.Comment)
	return e
}

type TransitionCommand struct {
	RequestID       string
	ExpectedVersion int64
	FailureCode     string
	Mutation        Mutation
}

func (c TransitionCommand) Validate() error {
	if !validID(c.RequestID) || c.ExpectedVersion < 1 || c.Mutation.Validate() != nil {
		return ErrInvalid
	}
	if c.FailureCode != "" && !tokenPattern.MatchString(c.FailureCode) {
		return ErrInvalid
	}
	return nil
}

type EscalateCommand struct {
	RequestID, EscalationID string
	ExpectedVersion         int64
	Mutation                Mutation
}

func (c EscalateCommand) Validate() error {
	if !validID(c.RequestID) || !validID(c.EscalationID) || c.ExpectedVersion < 1 || c.Mutation.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

// ExecutionGrant is the immutable authorization evidence a downstream
// sensitive operation may consume after BeginExecution won optimistic
// ownership of the request. It is bound to one exact action/resource.
type ExecutionGrant struct {
	RequestID, PolicyID                       string
	PolicyVersion                             int64
	Action, ResourceType, ResourceID          string
	Risk                                      RiskClass
	ApprovedAt, ExecutionStartedAt, ExpiresAt time.Time
}

func Grant(req Request, action, resourceType, resourceID string) (ExecutionGrant, error) {
	if req.Validate() != nil || req.State != StateExecuting || req.ApprovedAt == nil || req.ExecutionStartedAt == nil || !tokenPattern.MatchString(action) || !tokenPattern.MatchString(resourceType) || !validText(resourceID, 512) {
		return ExecutionGrant{}, ErrDenied
	}
	if req.Action != action || req.ResourceType != resourceType || req.ResourceID != resourceID || !req.ExecutionStartedAt.Before(req.ExpiresAt) {
		return ExecutionGrant{}, ErrDenied
	}
	return ExecutionGrant{RequestID: req.ID, PolicyID: req.PolicyID, PolicyVersion: req.PolicyVersion, Action: req.Action, ResourceType: req.ResourceType, ResourceID: req.ResourceID, Risk: req.Risk, ApprovedAt: req.ApprovedAt.UTC(), ExecutionStartedAt: req.ExecutionStartedAt.UTC(), ExpiresAt: req.ExpiresAt.UTC()}, nil
}
