// Package securitysettings defines the tenant-scoped settings security model.
package securitysettings

import (
	"context"
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

var (
	ErrInvalid        = errors.New("settings security: invalid value")
	ErrNotFound       = errors.New("settings security: session not found")
	ErrSessionRevoked = errors.New("settings security: session revoked")
)

// Session is a minimized application-visible OIDC session. Ref and SubjectRef
// are irreversible SHA-256 references; raw provider identifiers are excluded.
type Session struct {
	Ref             string     `json:"session_ref"`
	SubjectRef      string     `json:"-"`
	Status          string     `json:"status"`
	ClientKind      string     `json:"client_kind"`
	AuthenticatedAt time.Time  `json:"authenticated_at"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

// LoginEvent records when TORGNEXA first observed or revoked a validated OIDC
// session. It is not an identity-provider-wide login event.
type LoginEvent struct {
	ID         string    `json:"id"`
	SessionRef string    `json:"session_ref"`
	EventType  string    `json:"event_type"`
	ClientKind string    `json:"client_kind"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Observation contains only bounded, minimized identity evidence.
type Observation struct {
	EventID         string
	SessionRef      string
	SubjectRef      string
	ClientKind      string
	AuthenticatedAt time.Time
	ExpiresAt       time.Time
	ObservedAt      time.Time
}

// RevokeCommand carries append-only actor and correlation evidence.
type RevokeCommand struct {
	EventID       string
	SessionRef    string
	ActorID       string
	CorrelationID string
	OccurredAt    time.Time
}

// Store persists and enforces application session state.
type Store interface {
	Observe(context.Context, tenancy.Scope, Observation) error
	ListSessions(context.Context, tenancy.Scope, int, string) ([]Session, string, error)
	ListLoginEvents(context.Context, tenancy.Scope, int, string) ([]LoginEvent, string, error)
	Revoke(context.Context, tenancy.Scope, RevokeCommand) (Session, error)
}

// SettingsAuditReader returns only settings-related immutable audit evidence.
type SettingsAuditReader interface {
	ListSettings(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error)
}
