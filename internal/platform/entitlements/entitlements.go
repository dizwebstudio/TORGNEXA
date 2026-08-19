// Package entitlements defines TORGNEXA's provider-neutral tenant feature and quota model.
// It deliberately contains no subscription plan names or billing-provider branches.
package entitlements

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid       = errors.New("entitlements: invalid value")
	ErrDenied        = errors.New("entitlements: denied")
	ErrQuotaExceeded = errors.New("entitlements: quota exceeded")
	ErrConflict      = errors.New("entitlements: conflict")
	ErrNotFound      = errors.New("entitlements: not found")
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9._:/-]{0,127}$`)

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, ws string) (Scope, error) {
	if !validID(org) || !validID(ws) {
		return Scope{}, ErrInvalid
	}
	return Scope{organizationID: org, workspaceID: ws}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool            { return validID(s.organizationID) && validID(s.workspaceID) }

type FeatureKey string

func ParseFeatureKey(v string) (FeatureKey, error) {
	if !keyPattern.MatchString(v) {
		return "", ErrInvalid
	}
	return FeatureKey(v), nil
}
func (v FeatureKey) String() string { return string(v) }
func (v FeatureKey) Valid() bool    { return keyPattern.MatchString(string(v)) }

type MetricKey string

func ParseMetricKey(v string) (MetricKey, error) {
	if !keyPattern.MatchString(v) {
		return "", ErrInvalid
	}
	return MetricKey(v), nil
}
func (v MetricKey) String() string { return string(v) }
func (v MetricKey) Valid() bool    { return keyPattern.MatchString(string(v)) }

type Rule struct {
	ID, OrganizationID, WorkspaceID string
	Feature                         FeatureKey
	Enabled                         bool
	Source                          string
	Version                         int64
	EffectiveFrom                   time.Time
	EffectiveUntil                  *time.Time
	CreatedAt                       time.Time
}

func (r Rule) Validate() error {
	if !validID(r.ID) || !validID(r.OrganizationID) || !validID(r.WorkspaceID) || !r.Feature.Valid() || !keyPattern.MatchString(r.Source) || r.Version < 1 || !utc(r.EffectiveFrom) || !utc(r.CreatedAt) {
		return ErrInvalid
	}
	if r.EffectiveUntil != nil && (!utc(*r.EffectiveUntil) || !r.EffectiveUntil.After(r.EffectiveFrom)) {
		return ErrInvalid
	}
	return nil
}
func (r Rule) Effective(at time.Time) bool {
	return r.Validate() == nil && utc(at) && !at.Before(r.EffectiveFrom) && (r.EffectiveUntil == nil || at.Before(*r.EffectiveUntil))
}

type Evaluation struct {
	Feature     FeatureKey `json:"feature"`
	Allowed     bool       `json:"allowed"`
	ReasonCode  string     `json:"reason_code"`
	RuleID      string     `json:"rule_id,omitempty"`
	RuleVersion int64      `json:"rule_version,omitempty"`
	EvaluatedAt time.Time  `json:"evaluated_at"`
}

func (e Evaluation) Validate() error {
	if !e.Feature.Valid() || !keyPattern.MatchString(e.ReasonCode) || !utc(e.EvaluatedAt) {
		return ErrInvalid
	}
	if e.RuleID != "" && (!validID(e.RuleID) || e.RuleVersion < 1) {
		return ErrInvalid
	}
	if e.RuleID == "" && e.RuleVersion != 0 {
		return ErrInvalid
	}
	return nil
}

type Resolver interface {
	ResolveRule(context.Context, Scope, FeatureKey, time.Time) (Rule, error)
}

type Service struct{ resolver Resolver }

func NewService(r Resolver) (*Service, error) {
	if r == nil {
		return nil, ErrInvalid
	}
	return &Service{resolver: r}, nil
}
func (s *Service) Evaluate(ctx context.Context, scope Scope, feature FeatureKey, at time.Time) (Evaluation, error) {
	if s == nil || ctx == nil || !scope.Valid() || !feature.Valid() || !utc(at) {
		return Evaluation{}, ErrInvalid
	}
	rule, err := s.resolver.ResolveRule(ctx, scope, feature, at)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Evaluation{Feature: feature, Allowed: false, ReasonCode: "no_entitlement", EvaluatedAt: at}, nil
		}
		return Evaluation{}, err
	}
	if rule.Validate() != nil || rule.OrganizationID != scope.OrganizationID() || rule.WorkspaceID != scope.WorkspaceID() || rule.Feature != feature || !rule.Effective(at) {
		return Evaluation{}, ErrInvalid
	}
	reason := "entitlement_enabled"
	if !rule.Enabled {
		reason = "entitlement_disabled"
	}
	return Evaluation{Feature: feature, Allowed: rule.Enabled, ReasonCode: reason, RuleID: rule.ID, RuleVersion: rule.Version, EvaluatedAt: at}, nil
}

type WindowKind string

const (
	WindowLifetime WindowKind = "lifetime"
	WindowDayUTC   WindowKind = "calendar_day_utc"
	WindowMonthUTC WindowKind = "calendar_month_utc"
)

func (w WindowKind) Valid() bool {
	return w == WindowLifetime || w == WindowDayUTC || w == WindowMonthUTC
}
func (w WindowKind) Bucket(at time.Time) (time.Time, time.Time, error) {
	if !w.Valid() || !utc(at) {
		return time.Time{}, time.Time{}, ErrInvalid
	}
	switch w {
	case WindowLifetime:
		return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), nil
	case WindowDayUTC:
		start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), nil
	default:
		start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	}
}

type QuotaPolicy struct {
	ID, OrganizationID, WorkspaceID string
	Metric                          MetricKey
	Limit                           int64
	Window                          WindowKind
	Source                          string
	Version                         int64
	EffectiveFrom                   time.Time
	EffectiveUntil                  *time.Time
	CreatedAt                       time.Time
}

func (p QuotaPolicy) Validate() error {
	if !validID(p.ID) || !validID(p.OrganizationID) || !validID(p.WorkspaceID) || !p.Metric.Valid() || p.Limit < 0 || !p.Window.Valid() || !keyPattern.MatchString(p.Source) || p.Version < 1 || !utc(p.EffectiveFrom) || !utc(p.CreatedAt) {
		return ErrInvalid
	}
	if p.EffectiveUntil != nil && (!utc(*p.EffectiveUntil) || !p.EffectiveUntil.After(p.EffectiveFrom)) {
		return ErrInvalid
	}
	return nil
}
func (p QuotaPolicy) Effective(at time.Time) bool {
	return p.Validate() == nil && utc(at) && !at.Before(p.EffectiveFrom) && (p.EffectiveUntil == nil || at.Before(*p.EffectiveUntil))
}

type QuotaStatus struct {
	Metric        MetricKey `json:"metric"`
	Limit         int64     `json:"limit"`
	Used          int64     `json:"used"`
	Remaining     int64     `json:"remaining"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	PolicyID      string    `json:"policy_id"`
	PolicyVersion int64     `json:"policy_version"`
}

func (q QuotaStatus) Validate() error {
	if !q.Metric.Valid() || q.Limit < 0 || q.Used < 0 || q.Remaining < 0 || q.Used > q.Limit || q.Remaining != q.Limit-q.Used || !utc(q.WindowStart) || !utc(q.WindowEnd) || !q.WindowEnd.After(q.WindowStart) || !validID(q.PolicyID) || q.PolicyVersion < 1 {
		return ErrInvalid
	}
	return nil
}

type Consumption struct {
	ID            string
	Metric        MetricKey
	Amount        int64
	CorrelationID string
	OccurredAt    time.Time
}

func (c Consumption) Validate() error {
	if !validID(c.ID) || !c.Metric.Valid() || c.Amount <= 0 || !validText(c.CorrelationID, 256) || !utc(c.OccurredAt) {
		return ErrInvalid
	}
	return nil
}

type QuotaStore interface {
	ResolveQuotaPolicy(context.Context, Scope, MetricKey, time.Time) (QuotaPolicy, error)
	ConsumeQuota(context.Context, Scope, QuotaPolicy, Consumption) (QuotaStatus, error)
	QuotaStatus(context.Context, Scope, QuotaPolicy, time.Time) (QuotaStatus, error)
}

type QuotaService struct{ store QuotaStore }

func NewQuotaService(s QuotaStore) (*QuotaService, error) {
	if s == nil {
		return nil, ErrInvalid
	}
	return &QuotaService{store: s}, nil
}
func (s *QuotaService) Consume(ctx context.Context, scope Scope, c Consumption) (QuotaStatus, error) {
	if s == nil || ctx == nil || !scope.Valid() || c.Validate() != nil {
		return QuotaStatus{}, ErrInvalid
	}
	p, err := s.store.ResolveQuotaPolicy(ctx, scope, c.Metric, c.OccurredAt)
	if err != nil {
		return QuotaStatus{}, err
	}
	if p.Validate() != nil || p.OrganizationID != scope.OrganizationID() || p.WorkspaceID != scope.WorkspaceID() || p.Metric != c.Metric || !p.Effective(c.OccurredAt) {
		return QuotaStatus{}, ErrInvalid
	}
	return s.store.ConsumeQuota(ctx, scope, p, c)
}
func (s *QuotaService) Status(ctx context.Context, scope Scope, metric MetricKey, at time.Time) (QuotaStatus, error) {
	if s == nil || ctx == nil || !scope.Valid() || !metric.Valid() || !utc(at) {
		return QuotaStatus{}, ErrInvalid
	}
	p, err := s.store.ResolveQuotaPolicy(ctx, scope, metric, at)
	if err != nil {
		return QuotaStatus{}, err
	}
	return s.store.QuotaStatus(ctx, scope, p, at)
}

type Mutation struct {
	AuditID, EventID, ActorID, Source, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

func (m Mutation) Validate() error {
	if !validID(m.AuditID) || !validID(m.EventID) || !validText(m.ActorID, 256) || !keyPattern.MatchString(m.Source) || !validText(m.CorrelationID, 256) || !utc(m.OccurredAt) {
		return ErrInvalid
	}
	for _, v := range []string{m.CausationID, m.TraceID} {
		if v != "" && !keyPattern.MatchString(v) {
			return ErrInvalid
		}
	}
	return nil
}

func utc(t time.Time) bool { return !t.IsZero() && t.Location() == time.UTC }
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
