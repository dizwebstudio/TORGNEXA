// Package enterpriseiam implements explicit federation/provisioning mappings above the OIDC boundary.
package enterpriseiam

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalid = errors.New("enterpriseiam: invalid value")
var ErrDenied = errors.New("enterpriseiam: unmapped identity denied")

type Protocol string

const (
	ProtocolLDAP Protocol = "ldap"
	ProtocolAD   Protocol = "active_directory"
	ProtocolSAML Protocol = "saml"
	ProtocolSCIM Protocol = "scim"
	ProtocolJIT  Protocol = "jit"
)

func (p Protocol) Valid() bool {
	return p == ProtocolLDAP || p == ProtocolAD || p == ProtocolSAML || p == ProtocolSCIM || p == ProtocolJIT
}

type ExternalIdentity struct {
	Issuer, Subject, Email string
	Groups                 []string
	Claims                 map[string]string
	Disabled               bool
}
type MappingRule struct {
	ID                                     string
	Protocol                               Protocol
	Issuer, Group, Claim, ClaimValue, Role string
	OrganizationID, WorkspaceID            string
	Privileged                             bool
	Version                                int64
	UpdatedAt                              time.Time
}

func (r MappingRule) Validate() error {
	if r.ID == "" || !r.Protocol.Valid() || r.Issuer == "" || r.Role == "" || r.OrganizationID == "" || r.WorkspaceID == "" || r.Version < 1 || r.UpdatedAt.IsZero() {
		return ErrInvalid
	}
	if r.Group == "" && (r.Claim == "" || r.ClaimValue == "") {
		return ErrInvalid
	}
	return nil
}

type Grant struct {
	Role, RuleID string
	Privileged   bool
}

func Evaluate(scope tenancy.Scope, id ExternalIdentity, rules []MappingRule) ([]Grant, error) {
	if !scope.Valid() || id.Issuer == "" || id.Subject == "" || id.Disabled {
		return nil, ErrDenied
	}
	out := []Grant{}
	for _, r := range rules {
		if r.Validate() != nil {
			return nil, ErrInvalid
		}
		if r.OrganizationID != scope.OrganizationID().String() || r.WorkspaceID != scope.WorkspaceID().String() || r.Issuer != id.Issuer {
			continue
		}
		match := false
		if r.Group != "" {
			for _, g := range id.Groups {
				if g == r.Group {
					match = true
					break
				}
			}
		} else {
			match = id.Claims[r.Claim] == r.ClaimValue
		}
		if match {
			out = append(out, Grant{Role: r.Role, RuleID: r.ID, Privileged: r.Privileged})
		}
	}
	if len(out) == 0 {
		return nil, ErrDenied
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out, nil
}

type Revoker interface {
	RevokeSessions(context.Context, tenancy.Scope, string) error
	RevokeAPIKeys(context.Context, tenancy.Scope, string) error
	RevokeDelegations(context.Context, tenancy.Scope, string) error
}
type AuditSink interface {
	SecurityAudit(context.Context, tenancy.Scope, string, string, time.Time) error
}

func Offboard(ctx context.Context, scope tenancy.Scope, subject string, r Revoker, a AuditSink, now time.Time) error {
	if ctx == nil || !scope.Valid() || strings.TrimSpace(subject) == "" || r == nil || a == nil || now.IsZero() {
		return ErrInvalid
	}
	if err := r.RevokeSessions(ctx, scope, subject); err != nil {
		return err
	}
	if err := r.RevokeAPIKeys(ctx, scope, subject); err != nil {
		return err
	}
	if err := r.RevokeDelegations(ctx, scope, subject); err != nil {
		return err
	}
	return a.SecurityAudit(ctx, scope, "iam.deprovisioned", subject, now.UTC())
}

type ServiceAccount struct {
	ID, ClientID, SecretRef string
	Roles                   []string
	Disabled                bool
	Version                 int64
	RotatedAt               time.Time
}

func (s ServiceAccount) Validate() error {
	if s.ID == "" || s.ClientID == "" || s.SecretRef == "" || len(s.Roles) == 0 || s.Version < 1 || s.RotatedAt.IsZero() {
		return ErrInvalid
	}
	return nil
}

type Store struct {
	mu    sync.Mutex
	rules map[string]map[string]MappingRule
}

func NewStore() *Store { return &Store{rules: map[string]map[string]MappingRule{}} }
func (s *Store) put(scope tenancy.Scope, r MappingRule) error {
	if s == nil || !scope.Valid() || r.Validate() != nil || r.OrganizationID != scope.OrganizationID().String() || r.WorkspaceID != scope.WorkspaceID().String() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := r.OrganizationID + "/" + r.WorkspaceID
	if s.rules[k] == nil {
		s.rules[k] = map[string]MappingRule{}
	}
	if old, ok := s.rules[k][r.ID]; ok && old.Version >= r.Version {
		return ErrInvalid
	}
	s.rules[k][r.ID] = r
	return nil
}

// Put accepts non-privileged mappings. Privileged mappings must use PutReviewed so
// a security audit cannot be accidentally omitted.
func (s *Store) Put(scope tenancy.Scope, r MappingRule) error {
	if r.Privileged {
		return ErrInvalid
	}
	return s.put(scope, r)
}
func (s *Store) PutReviewed(ctx context.Context, scope tenancy.Scope, r MappingRule, audit AuditSink) error {
	if !r.Privileged || audit == nil || ctx == nil {
		return ErrInvalid
	}
	if err := audit.SecurityAudit(ctx, scope, "iam.privileged_mapping_changed", r.ID, r.UpdatedAt.UTC()); err != nil {
		return err
	}
	return s.put(scope, r)
}

type CredentialState struct {
	Subject                                          string
	Disabled                                         bool
	ActiveSessions, ActiveAPIKeys, ActiveDelegations int
}
type Drift struct {
	Subject, Code string
	Count         int
}

// ReconcileCredentialDrift reports disabled identities that still retain active credentials.
// It is intentionally read-only; Offboard performs the explicit revocation workflow.
func ReconcileCredentialDrift(states []CredentialState) ([]Drift, error) {
	out := make([]Drift, 0)
	for _, state := range states {
		if strings.TrimSpace(state.Subject) == "" || state.ActiveSessions < 0 || state.ActiveAPIKeys < 0 || state.ActiveDelegations < 0 {
			return nil, ErrInvalid
		}
		if !state.Disabled {
			continue
		}
		if state.ActiveSessions > 0 {
			out = append(out, Drift{Subject: state.Subject, Code: "active_sessions", Count: state.ActiveSessions})
		}
		if state.ActiveAPIKeys > 0 {
			out = append(out, Drift{Subject: state.Subject, Code: "active_api_keys", Count: state.ActiveAPIKeys})
		}
		if state.ActiveDelegations > 0 {
			out = append(out, Drift{Subject: state.Subject, Code: "active_delegations", Count: state.ActiveDelegations})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject == out[j].Subject {
			return out[i].Code < out[j].Code
		}
		return out[i].Subject < out[j].Subject
	})
	return out, nil
}
