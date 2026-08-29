// Package workflow defines the provider-neutral automation model.
//
// A workflow is a bounded, immutable-after-publish graph.  It intentionally
// contains no SQL, network, connector or provider types; execution is owned by
// the application/platform composition layers.
package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid          = errors.New("workflow: invalid value")
	ErrInvalidState     = errors.New("workflow: invalid state transition")
	ErrConflict         = errors.New("workflow: optimistic version conflict")
	ErrNotFound         = errors.New("workflow: not found")
	ErrQuotaExceeded    = errors.New("workflow: quota exceeded")
	ErrGraphCycle       = errors.New("workflow: graph contains a cycle")
	ErrGraphUnreachable = errors.New("workflow: graph contains unreachable node")
)

const (
	MaxNameLength        = 120
	MaxDescriptionLength = 4000
	MaxNodes             = 64
	MaxEdges             = 128
	MaxConfigBytes       = 16 * 1024
	MaxEventTypeLength   = 160
	MinScheduleMinutes   = 1
	MaxScheduleMinutes   = 7 * 24 * 60
)

var (
	idPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	namePattern      = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._:/()\-]{0,119}$`)
	eventTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$`)
	keyPattern       = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	conditionPattern = regexp.MustCompile(`^(always|true|false)$`)
)

// Scope is the mandatory organization/workspace boundary for workflow data.
type Scope struct{ organizationID, workspaceID string }

// ParseScope constructs a validated tenant scope.
func ParseScope(organizationID, workspaceID string) (Scope, error) {
	if !idPattern.MatchString(organizationID) || !idPattern.MatchString(workspaceID) {
		return Scope{}, ErrInvalid
	}
	return Scope{organizationID: organizationID, workspaceID: workspaceID}, nil
}

func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return idPattern.MatchString(s.organizationID) && idPattern.MatchString(s.workspaceID)
}

// DefinitionStatus is the immutable-version lifecycle of a workflow head.
type DefinitionStatus string

const (
	StatusDraft     DefinitionStatus = "draft"
	StatusPublished DefinitionStatus = "published"
	StatusPaused    DefinitionStatus = "paused"
	StatusArchived  DefinitionStatus = "archived"
)

func (s DefinitionStatus) Valid() bool {
	return s == StatusDraft || s == StatusPublished || s == StatusPaused || s == StatusArchived
}

// TriggerKind identifies how a workflow run is requested.
type TriggerKind string

const (
	TriggerEvent    TriggerKind = "event"
	TriggerSchedule TriggerKind = "schedule"
)

func (k TriggerKind) Valid() bool { return k == TriggerEvent || k == TriggerSchedule }

// Trigger is a typed event or bounded periodic trigger.
type Trigger struct {
	Kind            TriggerKind `json:"kind"`
	EventType       string      `json:"event_type,omitempty"`
	IntervalMinutes int         `json:"interval_minutes,omitempty"`
	Enabled         bool        `json:"enabled"`
	NextRunAt       *time.Time  `json:"next_run_at,omitempty"`
}

func (t Trigger) Validate() error {
	if !t.Kind.Valid() {
		return ErrInvalid
	}
	switch t.Kind {
	case TriggerEvent:
		if !eventTypePattern.MatchString(t.EventType) || len(t.EventType) > MaxEventTypeLength || t.IntervalMinutes != 0 || t.NextRunAt != nil {
			return ErrInvalid
		}
	case TriggerSchedule:
		if t.EventType != "" || t.IntervalMinutes < MinScheduleMinutes || t.IntervalMinutes > MaxScheduleMinutes {
			return ErrInvalid
		}
		if t.Enabled != (t.NextRunAt != nil) {
			return ErrInvalid
		}
		if t.NextRunAt != nil && !isUTC(*t.NextRunAt) {
			return ErrInvalid
		}
	}
	return nil
}

// NodeKind identifies a node in the declarative graph.
type NodeKind string

const (
	NodeCondition NodeKind = "condition"
	NodeAction    NodeKind = "action"
	NodeDelay     NodeKind = "delay"
	NodeApproval  NodeKind = "approval"
)

func (k NodeKind) Valid() bool {
	return k == NodeCondition || k == NodeAction || k == NodeDelay || k == NodeApproval
}

// Node is deliberately declarative. Config is a bounded JSON object consumed
// by a registered typed adapter, never evaluated as code.
type Node struct {
	ID     string          `json:"id"`
	Kind   NodeKind        `json:"kind"`
	Action string          `json:"action,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`
}

func (n Node) Validate() error {
	if !idPattern.MatchString(n.ID) || !n.Kind.Valid() {
		return ErrInvalid
	}
	if n.Kind == NodeAction || n.Kind == NodeApproval {
		if !AllowedAction(n.Action) {
			return ErrInvalid
		}
	} else if n.Action != "" {
		return ErrInvalid
	}
	if len(n.Config) > MaxConfigBytes {
		return ErrInvalid
	}
	if len(n.Config) > 0 {
		if err := validateConfig(n.Config); err != nil {
			return err
		}
	}
	return nil
}

// Edge connects two nodes. Conditions are intentionally finite in v1.
type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

func (e Edge) Validate() error {
	if !idPattern.MatchString(e.From) || !idPattern.MatchString(e.To) || e.From == e.To || (e.Condition != "" && !conditionPattern.MatchString(e.Condition)) {
		return ErrInvalid
	}
	return nil
}

// Definition is the mutable draft submitted by an operator. Publishing
// creates an immutable Version and its deterministic plan digest.
type Definition struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Trigger     Trigger `json:"trigger"`
	Nodes       []Node  `json:"nodes"`
	Edges       []Edge  `json:"edges"`
}

func (d Definition) Validate() error {
	if !validName(d.Name) || !validDescription(d.Description) || d.Trigger.Validate() != nil || len(d.Nodes) == 0 || len(d.Nodes) > MaxNodes || len(d.Edges) > MaxEdges {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(d.Nodes))
	for _, node := range d.Nodes {
		if node.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[node.ID]; ok {
			return ErrInvalid
		}
		seen[node.ID] = struct{}{}
	}
	for _, edge := range d.Edges {
		if edge.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[edge.From]; !ok {
			return ErrInvalid
		}
		if _, ok := seen[edge.To]; !ok {
			return ErrInvalid
		}
	}
	return nil
}

// Workflow is the current tenant-scoped definition head.
type Workflow struct {
	ID             string           `json:"id"`
	OrganizationID string           `json:"organization_id"`
	WorkspaceID    string           `json:"workspace_id"`
	Name           string           `json:"name"`
	Description    string           `json:"description,omitempty"`
	Status         DefinitionStatus `json:"status"`
	CurrentVersion int64            `json:"current_version"`
	Version        int64            `json:"version"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

func (w Workflow) Validate() error {
	if !idPattern.MatchString(w.ID) || !idPattern.MatchString(w.OrganizationID) || !idPattern.MatchString(w.WorkspaceID) || !validName(w.Name) || !validDescription(w.Description) || !w.Status.Valid() || w.CurrentVersion < 1 || w.Version < 1 || !isUTC(w.CreatedAt) || !isUTC(w.UpdatedAt) || w.UpdatedAt.Before(w.CreatedAt) {
		return ErrInvalid
	}
	return nil
}

// WorkflowVersion is immutable after publication. Draft versions may be
// replaced by creating a new version, never by mutating this value in place.
type WorkflowVersion struct {
	ID             string     `json:"id"`
	WorkflowID     string     `json:"workflow_id"`
	OrganizationID string     `json:"organization_id"`
	WorkspaceID    string     `json:"workspace_id"`
	Version        int64      `json:"version"`
	Definition     Definition `json:"definition"`
	PlanDigest     string     `json:"plan_digest"`
	CreatedAt      time.Time  `json:"created_at"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
}

func (v WorkflowVersion) Validate() error {
	if !idPattern.MatchString(v.ID) || !idPattern.MatchString(v.WorkflowID) || !idPattern.MatchString(v.OrganizationID) || !idPattern.MatchString(v.WorkspaceID) || v.Version < 1 || v.Definition.Validate() != nil || !hexDigest(v.PlanDigest) || !isUTC(v.CreatedAt) {
		return ErrInvalid
	}
	if v.PublishedAt != nil && (!isUTC(*v.PublishedAt) || v.PublishedAt.Before(v.CreatedAt)) {
		return ErrInvalid
	}
	return nil
}

// Plan is the deterministic topological execution plan for a definition.
type Plan struct {
	NodeIDs []string `json:"node_ids"`
	Digest  string   `json:"digest"`
}

// Compile validates and deterministically compiles a definition.
func Compile(definition Definition) (Plan, error) {
	if err := definition.Validate(); err != nil {
		return Plan{}, err
	}
	nodes := make(map[string]Node, len(definition.Nodes))
	indegree := make(map[string]int, len(definition.Nodes))
	originalIndegree := make(map[string]int, len(definition.Nodes))
	adjacency := make(map[string][]string, len(definition.Nodes))
	for _, node := range definition.Nodes {
		nodes[node.ID] = node
		indegree[node.ID] = 0
		originalIndegree[node.ID] = 0
	}
	for _, edge := range definition.Edges {
		indegree[edge.To]++
		originalIndegree[edge.To]++
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}
	for key := range adjacency {
		sort.Strings(adjacency[key])
	}
	queue := make([]string, 0, len(nodes))
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	order := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, next := range adjacency[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
				sort.Strings(queue)
			}
		}
	}
	if len(order) != len(nodes) {
		return Plan{}, ErrGraphCycle
	}
	// A definition has one trigger root.  Disconnected components would be
	// silently skipped by an executor, so reject them instead of inventing
	// implicit trigger semantics.
	roots := make([]string, 0, len(nodes))
	for id, degree := range originalIndegree {
		if degree == 0 {
			roots = append(roots, id)
		}
	}
	if len(roots) != 1 {
		return Plan{}, ErrGraphUnreachable
	}
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, next := range adjacency[id] {
			visit(next)
		}
	}
	visit(roots[0])
	if len(reachable) != len(nodes) {
		return Plan{}, ErrGraphUnreachable
	}
	canonical := struct {
		Definition Definition `json:"definition"`
		Order      []string   `json:"order"`
	}{Definition: definition, Order: order}
	data, err := json.Marshal(canonical)
	if err != nil {
		return Plan{}, fmt.Errorf("workflow: plan digest: %w", err)
	}
	digest := sha256.Sum256(data)
	return Plan{NodeIDs: order, Digest: hex.EncodeToString(digest[:])}, nil
}

// ActionRisk mirrors the approval/audit vocabulary without importing platform
// packages into Core.
type ActionRisk string

const (
	RiskRead               ActionRisk = "read"
	RiskWriteSafe          ActionRisk = "write_safe"
	RiskWriteSensitive     ActionRisk = "write_sensitive"
	RiskLegallySignificant ActionRisk = "legally_significant"
)

// ActionSpec is the allowlisted contract for one typed adapter.
type ActionSpec struct {
	Name       string
	Risk       ActionRisk
	Capability string
	Retryable  bool
	DryRun     bool
}

var actionCatalog = map[string]ActionSpec{
	"notification.create": {Name: "notification.create", Risk: RiskWriteSafe, Capability: "notifications.create", Retryable: true, DryRun: true},
	"reconciliation.run":  {Name: "reconciliation.run", Risk: RiskWriteSafe, Capability: "reconciliation.run", Retryable: true, DryRun: true},
	"approval.request":    {Name: "approval.request", Risk: RiskWriteSensitive, Capability: "approval.request", Retryable: false, DryRun: true},
	"sync.dry_run":        {Name: "sync.dry_run", Risk: RiskRead, Capability: "sync.preview", Retryable: true, DryRun: true},
}

// AllowedAction reports whether a typed action is in the v1 catalog.
func AllowedAction(name string) bool { _, ok := actionCatalog[name]; return ok }

// Action returns an immutable copy of an action specification.
func Action(name string) (ActionSpec, bool) { spec, ok := actionCatalog[name]; return spec, ok }

// ValidateTransition validates workflow head lifecycle transitions.
func ValidateTransition(from, to DefinitionStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case StatusDraft:
		if to == StatusPublished || to == StatusArchived {
			return nil
		}
	case StatusPublished:
		if to == StatusPaused || to == StatusArchived {
			return nil
		}
	case StatusPaused:
		if to == StatusPublished || to == StatusArchived {
			return nil
		}
	}
	return ErrInvalidState
}

// RunStatus is the durable state of one workflow execution.
type RunStatus string

const (
	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunWaitingRetry    RunStatus = "waiting_retry"
	RunCompleted       RunStatus = "completed"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
)

func (s RunStatus) Valid() bool {
	return s == RunQueued || s == RunRunning || s == RunWaitingApproval || s == RunWaitingRetry || s == RunCompleted || s == RunFailed || s == RunCancelled
}

// StepStatus is the durable state of one planned node.
type StepStatus string

const (
	StepQueued          StepStatus = "queued"
	StepRunning         StepStatus = "running"
	StepWaitingApproval StepStatus = "waiting_approval"
	StepWaitingRetry    StepStatus = "waiting_retry"
	StepCompleted       StepStatus = "completed"
	StepFailed          StepStatus = "failed"
	StepSkipped         StepStatus = "skipped"
)

func (s StepStatus) Valid() bool {
	return s == StepQueued || s == StepRunning || s == StepWaitingApproval || s == StepWaitingRetry || s == StepCompleted || s == StepFailed || s == StepSkipped
}

// Run is a tenant-scoped execution. TriggerRef is an opaque event or manual
// reference; the raw event payload is never stored in this model.
type Run struct {
	ID              string      `json:"id"`
	OrganizationID  string      `json:"organization_id"`
	WorkspaceID     string      `json:"workspace_id"`
	WorkflowID      string      `json:"workflow_id"`
	WorkflowVersion int64       `json:"workflow_version"`
	TriggerKind     TriggerKind `json:"trigger_kind"`
	TriggerRef      string      `json:"trigger_ref,omitempty"`
	IdempotencyKey  string      `json:"idempotency_key"`
	InputDigest     string      `json:"input_digest"`
	Status          RunStatus   `json:"status"`
	AttemptCount    int         `json:"attempt_count"`
	AvailableAt     time.Time   `json:"available_at"`
	StartedAt       *time.Time  `json:"started_at,omitempty"`
	CompletedAt     *time.Time  `json:"completed_at,omitempty"`
	LastErrorCode   string      `json:"last_error_code,omitempty"`
	Version         int64       `json:"version"`
}

// RunRequest is the bounded command used to enqueue a run.
type RunRequest struct {
	ID              string
	WorkflowID      string
	WorkflowVersion int64
	TriggerKind     TriggerKind
	TriggerRef      string
	IdempotencyKey  string
	InputDigest     string
}

func (r Run) Validate() error {
	if !idPattern.MatchString(r.ID) || !idPattern.MatchString(r.OrganizationID) || !idPattern.MatchString(r.WorkspaceID) || !idPattern.MatchString(r.WorkflowID) || r.WorkflowVersion < 1 || !r.TriggerKind.Valid() || !validKey(r.IdempotencyKey, 256) || !hexDigest(r.InputDigest) || !r.Status.Valid() || r.AttemptCount < 0 || r.AttemptCount > 64 || !isUTC(r.AvailableAt) || r.Version < 1 {
		return ErrInvalid
	}
	if r.TriggerRef != "" && !validKey(r.TriggerRef, 256) {
		return ErrInvalid
	}
	if r.StartedAt != nil && !isUTC(*r.StartedAt) {
		return ErrInvalid
	}
	if r.CompletedAt != nil && (!isUTC(*r.CompletedAt) || (r.StartedAt != nil && r.CompletedAt.Before(*r.StartedAt))) {
		return ErrInvalid
	}
	if r.LastErrorCode != "" && !validErrorCode(r.LastErrorCode) {
		return ErrInvalid
	}
	return nil
}

// StepRun contains bounded evidence for a single node execution.
type StepRun struct {
	ID             string     `json:"id"`
	RunID          string     `json:"run_id"`
	OrganizationID string     `json:"organization_id"`
	WorkspaceID    string     `json:"workspace_id"`
	NodeID         string     `json:"node_id"`
	Status         StepStatus `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	OutputDigest   string     `json:"output_digest,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Version        int64      `json:"version"`
}

func (s StepRun) Validate() error {
	if !idPattern.MatchString(s.ID) || !idPattern.MatchString(s.RunID) || !idPattern.MatchString(s.OrganizationID) || !idPattern.MatchString(s.WorkspaceID) || !idPattern.MatchString(s.NodeID) || !s.Status.Valid() || s.AttemptCount < 0 || s.AttemptCount > 64 || s.Version < 1 {
		return ErrInvalid
	}
	if s.OutputDigest != "" && !hexDigest(s.OutputDigest) || s.ErrorCode != "" && !validErrorCode(s.ErrorCode) {
		return ErrInvalid
	}
	if s.StartedAt != nil && !isUTC(*s.StartedAt) || s.CompletedAt != nil && !isUTC(*s.CompletedAt) {
		return ErrInvalid
	}
	return nil
}

// ValidateRunTransition keeps terminal history immutable and permits only
// explicit recovery transitions.
func ValidateRunTransition(from, to RunStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case RunQueued:
		if to == RunRunning || to == RunCancelled {
			return nil
		}
	case RunRunning:
		if to == RunWaitingApproval || to == RunWaitingRetry || to == RunCompleted || to == RunFailed || to == RunCancelled {
			return nil
		}
	case RunWaitingApproval:
		if to == RunRunning || to == RunFailed || to == RunCancelled {
			return nil
		}
	case RunWaitingRetry:
		if to == RunRunning || to == RunFailed || to == RunCancelled {
			return nil
		}
	}
	return ErrInvalidState
}

func validateConfig(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return ErrInvalid
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ErrInvalid
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ErrInvalid
	}
	return validateJSONValue(object, 0)
}

func validateJSONValue(value any, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") || strings.Contains(lower, "private_key") || !keyPattern.MatchString(key) {
				return ErrInvalid
			}
			if err := validateJSONValue(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) > 64 {
			return ErrInvalid
		}
		for _, child := range typed {
			if err := validateJSONValue(child, depth+1); err != nil {
				return err
			}
		}
	case string:
		if !utf8.ValidString(typed) || len(typed) > 4000 || strings.ContainsAny(typed, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f") {
			return ErrInvalid
		}
	}
	return nil
}

func validName(value string) bool {
	return value == strings.TrimSpace(value) && utf8.ValidString(value) && namePattern.MatchString(value)
}

func validDescription(value string) bool {
	return value == "" || (value == strings.TrimSpace(value) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= MaxDescriptionLength)
}

func isUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
func hexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validKey(value string, max int) bool {
	return len(value) >= 1 && len(value) <= max && idPattern.MatchString(value)
}

func validErrorCode(value string) bool {
	return len(value) >= 1 && len(value) <= 64 && regexp.MustCompile(`^[a-z][a-z0-9._-]*$`).MatchString(value)
}
