// Package agentgovernance implements fail-closed policy enforcement for AI agents.
// Model/tool input is never an authority source: tenant scope, agent identity,
// tool grants, kill switches and action limits come from trusted server-side state.
package agentgovernance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrInvalid   = errors.New("agent governance: invalid value")
	ErrDenied    = errors.New("agent governance: denied")
	ErrNotFound  = errors.New("agent governance: not found")
	ErrConflict  = errors.New("agent governance: conflict")
	ErrRateLimit = errors.New("agent governance: rate limit")
)

// Risk is an AI/tool risk class. Prohibited is intentionally stronger than
// audit.Risk: a prohibited capability cannot be made executable by approval.
type Risk string

const (
	RiskRead           Risk = "read"
	RiskSafeWrite      Risk = "safe_write"
	RiskSensitiveWrite Risk = "sensitive_write"
	RiskProhibited     Risk = "prohibited"
)

func (r Risk) Valid() bool {
	return r == RiskRead || r == RiskSafeWrite || r == RiskSensitiveWrite || r == RiskProhibited
}

// ContextTrust is server-derived. MCP calls use UntrustedExternal because the
// server cannot attest what prompt/review/message/product text influenced the model.
type ContextTrust string

const (
	TrustTrustedSystem     ContextTrust = "trusted_system"
	TrustUserInput         ContextTrust = "user_input"
	TrustUntrustedExternal ContextTrust = "untrusted_external"
	TrustModelGenerated    ContextTrust = "model_generated"
)

func (t ContextTrust) Valid() bool {
	return t == TrustTrustedSystem || t == TrustUserInput || t == TrustUntrustedExternal || t == TrustModelGenerated
}

// Agent is trusted identity metadata supplied by the authenticated integration,
// never by tool arguments or external content.
type Agent struct {
	ID            string `json:"agent_id"`
	ModelID       string `json:"model_id"`
	RunID         string `json:"run_id"`
	IntegrationID string `json:"integration_id"`
}

func (a Agent) Valid() bool {
	return validToken(a.ID, 160) && validToken(a.ModelID, 160) && validToken(a.RunID, 160) && validToken(a.IntegrationID, 160)
}

// Money is one bounded monetary action metric. Currency is ISO-4217 style and
// amount is integer minor units; floating-point money never enters governance.
type Money struct {
	Currency   string `json:"currency"`
	MinorUnits int64  `json:"minor_units"`
}

func (m Money) Validate() error {
	if !validCurrency(m.Currency) || m.MinorUnits < 0 {
		return ErrInvalid
	}
	return nil
}

// ActionMetrics are server-parsed semantic values used for policy enforcement.
// Nil means the dimension is not applicable to the tool, never "unknown".
type ActionMetrics struct {
	Money                 *Money `json:"money,omitempty"`
	Quantity              *int64 `json:"quantity,omitempty"`
	PercentageBasisPoints *int64 `json:"percentage_basis_points,omitempty"`
	BatchSize             *int64 `json:"batch_size,omitempty"`
}

func (m ActionMetrics) Validate() error {
	if m.Money != nil && m.Money.Validate() != nil {
		return ErrInvalid
	}
	for _, value := range []*int64{m.Quantity, m.PercentageBasisPoints, m.BatchSize} {
		if value != nil && *value < 0 {
			return ErrInvalid
		}
	}
	if m.PercentageBasisPoints != nil && *m.PercentageBasisPoints > 10_000 {
		return ErrInvalid
	}
	return nil
}

func (m ActionMetrics) HasAny() bool {
	return m.Money != nil || m.Quantity != nil || m.PercentageBasisPoints != nil || m.BatchSize != nil
}

// ActionLimits are hard maxima. Zero means that dimension is not permitted for
// a call that supplies it. Writes must configure at least one hard limit or a
// frequency limit, preventing an accidentally unbounded write policy.
type ActionLimits struct {
	Money                    []Money `json:"money,omitempty"`
	MaxQuantity              int64   `json:"max_quantity,omitempty"`
	MaxPercentageBasisPoints int64   `json:"max_percentage_basis_points,omitempty"`
	MaxBatchSize             int64   `json:"max_batch_size,omitempty"`
	MaxCalls                 int64   `json:"max_calls,omitempty"`
	WindowSeconds            int64   `json:"window_seconds,omitempty"`
}

func (l ActionLimits) Validate() error {
	if l.MaxQuantity < 0 || l.MaxPercentageBasisPoints < 0 || l.MaxPercentageBasisPoints > 10_000 || l.MaxBatchSize < 0 || l.MaxCalls < 0 || l.WindowSeconds < 0 {
		return ErrInvalid
	}
	if (l.MaxCalls == 0) != (l.WindowSeconds == 0) {
		return ErrInvalid
	}
	if l.WindowSeconds > int64((31*24*time.Hour)/time.Second) {
		return ErrInvalid
	}
	if len(l.Money) > 32 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, money := range l.Money {
		if money.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[money.Currency]; ok {
			return ErrInvalid
		}
		seen[money.Currency] = struct{}{}
	}
	return nil
}

func (l ActionLimits) HasAny() bool {
	return len(l.Money) > 0 || l.MaxQuantity > 0 || l.MaxPercentageBasisPoints > 0 || l.MaxBatchSize > 0 || l.MaxCalls > 0
}

// ToolRule is immutable policy evidence for one tool capability.
type ToolRule struct {
	Tool                string       `json:"tool"`
	Permission          string       `json:"permission"`
	Risk                Risk         `json:"risk"`
	ApprovalRequired    bool         `json:"approval_required"`
	AllowUntrustedWrite bool         `json:"allow_untrusted_write"`
	Limits              ActionLimits `json:"limits"`
}

func (r ToolRule) Validate() error {
	if !validTool(r.Tool) || !validToken(r.Permission, 160) || !r.Risk.Valid() || r.Limits.Validate() != nil {
		return ErrInvalid
	}
	if forbiddenTool(r.Tool) && r.Risk != RiskProhibited {
		return ErrInvalid
	}
	if r.Risk == RiskProhibited && (r.ApprovalRequired || r.AllowUntrustedWrite || r.Limits.HasAny()) {
		return ErrInvalid
	}
	if r.Risk == RiskSensitiveWrite && !r.ApprovalRequired {
		return ErrInvalid
	}
	if (r.Risk == RiskSafeWrite || r.Risk == RiskSensitiveWrite) && !r.Limits.HasAny() {
		return ErrInvalid
	}
	return nil
}

// Policy is an immutable versioned allowlist for one authenticated agent and
// integration pair. Repository adapters resolve only the active version.
type Policy struct {
	ID             string     `json:"id"`
	Version        uint64     `json:"version"`
	AgentID        string     `json:"agent_id"`
	IntegrationID  string     `json:"integration_id"`
	Rules          []ToolRule `json:"rules"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveUntil *time.Time `json:"effective_until,omitempty"`
}

func (p Policy) Validate() error {
	if !validToken(p.ID, 160) || p.Version == 0 || !validToken(p.AgentID, 160) || !validToken(p.IntegrationID, 160) || len(p.Rules) == 0 || len(p.Rules) > 256 || !utcNonZero(p.EffectiveFrom) {
		return ErrInvalid
	}
	if p.EffectiveUntil != nil && (!utcNonZero(*p.EffectiveUntil) || !p.EffectiveUntil.After(p.EffectiveFrom)) {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, rule := range p.Rules {
		if rule.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[rule.Tool]; ok {
			return ErrInvalid
		}
		seen[rule.Tool] = struct{}{}
	}
	return nil
}

func (p Policy) Effective(at time.Time) bool {
	if p.Validate() != nil || !utcNonZero(at) || at.Before(p.EffectiveFrom) {
		return false
	}
	return p.EffectiveUntil == nil || at.Before(*p.EffectiveUntil)
}

func (p Policy) Rule(tool string) (ToolRule, bool) {
	for _, rule := range p.Rules {
		if rule.Tool == tool {
			return rule, true
		}
	}
	return ToolRule{}, false
}

// KillState is operational state with tenant, agent and integration scopes.
type KillState struct {
	TenantDisabled      bool   `json:"tenant_disabled"`
	AgentDisabled       bool   `json:"agent_disabled"`
	IntegrationDisabled bool   `json:"integration_disabled"`
	Version             uint64 `json:"version"`
}

func (s KillState) Disabled() bool {
	return s.TenantDisabled || s.AgentDisabled || s.IntegrationDisabled
}

// KillScope identifies one immutable operational kill-switch stream.
type KillScope string

const (
	KillTenant      KillScope = "tenant"
	KillAgent       KillScope = "agent"
	KillIntegration KillScope = "integration"
)

func (s KillScope) Valid() bool {
	return s == KillTenant || s == KillAgent || s == KillIntegration
}

// Change identifies a trusted control-plane actor changing governance state.
// It deliberately contains no model prompt or arbitrary external content.
type Change struct {
	ActorID    string    `json:"actor_id"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (c Change) Validate() error {
	if !validToken(c.ActorID, 256) || !validText(c.Reason, 256) || !utcNonZero(c.OccurredAt) {
		return ErrInvalid
	}
	return nil
}

// KillChange is one append-only tenant/agent/integration kill-switch version.
type KillChange struct {
	Scope     KillScope `json:"scope"`
	SubjectID string    `json:"subject_id"`
	Version   uint64    `json:"version"`
	Disabled  bool      `json:"disabled"`
	Change    Change    `json:"change"`
}

func (k KillChange) Validate() error {
	if !k.Scope.Valid() || k.Version == 0 || k.Change.Validate() != nil {
		return ErrInvalid
	}
	if k.Scope == KillTenant {
		if k.SubjectID != "*" {
			return ErrInvalid
		}
		return nil
	}
	if !validToken(k.SubjectID, 160) {
		return ErrInvalid
	}
	return nil
}

// Request contains only server-derived authority and semantic metrics.
type Request struct {
	Agent            Agent
	Tool             string
	Permission       string
	Risk             Risk
	Trust            ContextTrust
	ApprovalBoundary bool
	Metrics          ActionMetrics
	CorrelationID    string
	InvocationID     string
	At               time.Time
}

func (r Request) Validate() error {
	if !r.Agent.Valid() || !validTool(r.Tool) || !validToken(r.Permission, 160) || !r.Risk.Valid() || !r.Trust.Valid() || r.Metrics.Validate() != nil || !validText(r.CorrelationID, 256) || !validToken(r.InvocationID, 256) || !utcNonZero(r.At) {
		return ErrInvalid
	}
	return nil
}

// Decision is safe provenance metadata for audit/tool-result surfaces.
type Decision struct {
	PolicyID         string       `json:"policy_id"`
	PolicyVersion    uint64       `json:"policy_version"`
	Risk             Risk         `json:"risk"`
	Trust            ContextTrust `json:"context_trust"`
	ApprovalRequired bool         `json:"approval_required"`
	KillVersion      uint64       `json:"kill_switch_version"`
}

// PolicySource, KillSwitch and FrequencyLimiter are trusted server-side ports.
type PolicySource interface {
	ResolveAgentPolicy(context.Context, tenancy.Scope, Agent, time.Time) (Policy, error)
}

type KillSwitch interface {
	AgentKillState(context.Context, tenancy.Scope, Agent) (KillState, error)
}

type FrequencyRequest struct {
	PolicyID      string
	PolicyVersion uint64
	AgentID       string
	IntegrationID string
	Tool          string
	InvocationID  string
	WindowStart   time.Time
	WindowEnd     time.Time
	MaxCalls      int64
	OccurredAt    time.Time
}

func (r FrequencyRequest) Validate() error {
	return validateFrequencyRequest(r)
}

type FrequencyLimiter interface {
	ConsumeAgentCall(context.Context, tenancy.Scope, FrequencyRequest) (bool, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	policies PolicySource
	kills    KillSwitch
	limiter  FrequencyLimiter
	clock    Clock
}

func NewService(policies PolicySource, kills KillSwitch, limiter FrequencyLimiter) (*Service, error) {
	return newService(policies, kills, limiter, systemClock{})
}

func newService(policies PolicySource, kills KillSwitch, limiter FrequencyLimiter, clock Clock) (*Service, error) {
	if policies == nil || kills == nil || clock == nil {
		return nil, ErrInvalid
	}
	return &Service{policies: policies, kills: kills, limiter: limiter, clock: clock}, nil
}

// Discover checks kill-switch and allowlist state without consuming frequency.
func (s *Service) Discover(ctx context.Context, scope tenancy.Scope, agent Agent, tool, permission string, risk Risk, approvalBoundary bool) (Decision, error) {
	at := s.now()
	if ctx == nil || !scope.Valid() || !agent.Valid() || !validTool(tool) || !validToken(permission, 160) || !risk.Valid() || !utcNonZero(at) {
		return Decision{}, ErrDenied
	}
	return s.evaluateBase(ctx, scope, agent, tool, permission, risk, TrustUntrustedExternal, approvalBoundary, at)
}

// AuthorizeCall enforces the complete fail-closed policy and atomically consumes
// frequency allowance after all static limits pass.
func (s *Service) AuthorizeCall(ctx context.Context, scope tenancy.Scope, req Request) (Decision, error) {
	if s == nil || ctx == nil || !scope.Valid() || req.Validate() != nil {
		return Decision{}, ErrDenied
	}
	decision, err := s.evaluateBase(ctx, scope, req.Agent, req.Tool, req.Permission, req.Risk, req.Trust, req.ApprovalBoundary, req.At)
	if err != nil {
		return Decision{}, err
	}
	policy, err := s.policies.ResolveAgentPolicy(ctx, scope, req.Agent, req.At)
	if err != nil || policy.Validate() != nil || !policy.Effective(req.At) {
		return Decision{}, ErrDenied
	}
	rule, ok := policy.Rule(req.Tool)
	if !ok || checkMetrics(rule.Limits, req.Metrics) != nil {
		return Decision{}, ErrDenied
	}
	if rule.Limits.MaxCalls > 0 {
		if s.limiter == nil {
			return Decision{}, ErrDenied
		}
		window := time.Duration(rule.Limits.WindowSeconds) * time.Second
		start := req.At.Truncate(window)
		frequency := FrequencyRequest{PolicyID: policy.ID, PolicyVersion: policy.Version, AgentID: req.Agent.ID, IntegrationID: req.Agent.IntegrationID, Tool: req.Tool, InvocationID: req.InvocationID, WindowStart: start, WindowEnd: start.Add(window), MaxCalls: rule.Limits.MaxCalls, OccurredAt: req.At}
		if frequency.Validate() != nil {
			return Decision{}, ErrDenied
		}
		allowed, limitErr := s.limiter.ConsumeAgentCall(ctx, scope, frequency)
		if limitErr != nil || !allowed {
			return Decision{}, ErrRateLimit
		}
	}
	return decision, nil
}

func (s *Service) evaluateBase(ctx context.Context, scope tenancy.Scope, agent Agent, tool, permission string, risk Risk, trust ContextTrust, approvalBoundary bool, at time.Time) (Decision, error) {
	if s == nil || s.policies == nil || s.kills == nil {
		return Decision{}, ErrDenied
	}
	if forbiddenTool(tool) || risk == RiskProhibited {
		return Decision{}, ErrDenied
	}
	kill, err := s.kills.AgentKillState(ctx, scope, agent)
	if err != nil || kill.Disabled() {
		return Decision{}, ErrDenied
	}
	policy, err := s.policies.ResolveAgentPolicy(ctx, scope, agent, at)
	if err != nil || policy.Validate() != nil || !policy.Effective(at) || policy.AgentID != agent.ID || policy.IntegrationID != agent.IntegrationID {
		return Decision{}, ErrDenied
	}
	rule, ok := policy.Rule(tool)
	if !ok || rule.Permission != permission || rule.Risk != risk || rule.Risk == RiskProhibited {
		return Decision{}, ErrDenied
	}
	if rule.ApprovalRequired != approvalBoundary {
		return Decision{}, ErrDenied
	}
	if risk == RiskSensitiveWrite && !approvalBoundary {
		return Decision{}, ErrDenied
	}
	// External/model-generated text may influence a read, but cannot cause a
	// direct write unless an administrator explicitly enabled that exact rule.
	// Sensitive writes remain approval-only even when explicitly enabled.
	if (trust == TrustUntrustedExternal || trust == TrustModelGenerated) && risk != RiskRead && !rule.AllowUntrustedWrite && !rule.ApprovalRequired {
		return Decision{}, ErrDenied
	}
	return Decision{PolicyID: policy.ID, PolicyVersion: policy.Version, Risk: risk, Trust: trust, ApprovalRequired: rule.ApprovalRequired, KillVersion: kill.Version}, nil
}

func checkMetrics(limits ActionLimits, metrics ActionMetrics) error {
	if metrics.Validate() != nil {
		return ErrDenied
	}
	if metrics.Money != nil {
		max := int64(-1)
		for _, allowed := range limits.Money {
			if allowed.Currency == metrics.Money.Currency {
				max = allowed.MinorUnits
				break
			}
		}
		if max < 0 || metrics.Money.MinorUnits > max {
			return ErrDenied
		}
	}
	if metrics.Quantity != nil && (limits.MaxQuantity == 0 || *metrics.Quantity > limits.MaxQuantity) {
		return ErrDenied
	}
	if metrics.PercentageBasisPoints != nil && (limits.MaxPercentageBasisPoints == 0 || *metrics.PercentageBasisPoints > limits.MaxPercentageBasisPoints) {
		return ErrDenied
	}
	if metrics.BatchSize != nil && (limits.MaxBatchSize == 0 || *metrics.BatchSize > limits.MaxBatchSize) {
		return ErrDenied
	}
	return nil
}

func validateFrequencyRequest(r FrequencyRequest) error {
	if !validToken(r.PolicyID, 160) || r.PolicyVersion == 0 || !validToken(r.AgentID, 160) || !validToken(r.IntegrationID, 160) || !validTool(r.Tool) || !validToken(r.InvocationID, 256) || r.MaxCalls <= 0 || !utcNonZero(r.WindowStart) || !utcNonZero(r.WindowEnd) || !utcNonZero(r.OccurredAt) || !r.WindowEnd.After(r.WindowStart) || r.OccurredAt.Before(r.WindowStart) || !r.OccurredAt.Before(r.WindowEnd) {
		return ErrInvalid
	}
	return nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Time{}
	}
	return s.clock.Now().UTC()
}

// CanonicalRules returns a stable copy suitable for persistence/contracts.
func CanonicalRules(rules []ToolRule) ([]ToolRule, error) {
	out := append([]ToolRule(nil), rules...)
	for _, rule := range out {
		if rule.Validate() != nil {
			return nil, ErrInvalid
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	for i := 1; i < len(out); i++ {
		if out[i-1].Tool == out[i].Tool {
			return nil, ErrInvalid
		}
	}
	return out, nil
}

func forbiddenTool(tool string) bool {
	parts := strings.FieldsFunc(strings.ToLower(tool), func(r rune) bool { return r == '.' || r == '/' || r == ':' || r == '-' })
	for _, part := range parts {
		switch part {
		case "secret", "secrets", "credential", "credentials", "private_key", "privatekey", "apikey", "tokenexport":
			return true
		}
	}
	lower := strings.ToLower(tool)
	return strings.Contains(lower, "private_key") || strings.Contains(lower, "private-key") || strings.Contains(lower, "api_key")
}

func validTool(v string) bool { return validToken(v, 160) }
func validToken(v string, max int) bool {
	if len(v) == 0 || len(v) > max || strings.TrimSpace(v) != v || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:/-", r) {
			continue
		}
		return false
	}
	return true
}
func validText(v string, max int) bool {
	if len(v) == 0 || len(v) > max || strings.TrimSpace(v) != v || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validCurrency(v string) bool {
	if len(v) != 3 {
		return false
	}
	for _, r := range v {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
func utcNonZero(v time.Time) bool { return !v.IsZero() && v.Location() == time.UTC }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (d Decision) String() string {
	return fmt.Sprintf("%s@%d:%s", d.PolicyID, d.PolicyVersion, d.Risk)
}
