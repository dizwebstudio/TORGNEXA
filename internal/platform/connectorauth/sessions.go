package connectorauth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrSessionNotFound = errors.New("connector auth: oauth session not found")
	ErrSessionConflict = errors.New("connector auth: oauth session conflict")
)

// Session is the non-secret, tenant-scoped one-time OAuth authorization record.
type Session struct {
	ID               string
	AccountID        string
	AccountVersion   int64
	ActorID          string
	StateDigest      string
	PendingSecretRef string
	CallbackURL      string
	CorrelationID    string
	Status           string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	ConsumedAt       *time.Time
}

// Validate verifies that the row contains no secret material and represents a bounded transition.
func (session Session) Validate() error {
	if !validRecordText(session.ID, 128) || !validRecordText(session.AccountID, 128) || session.AccountVersion < 1 || !validRecordText(session.ActorID, 512) || len(session.StateDigest) != 64 || !validRecordText(session.PendingSecretRef, 64) || len(session.CallbackURL) > 2048 || !validRecordText(session.CorrelationID, 128) || session.CreatedAt.IsZero() || session.ExpiresAt.IsZero() || !session.ExpiresAt.After(session.CreatedAt) || session.ExpiresAt.Sub(session.CreatedAt) > OAuthSessionTTL {
		return ErrInvalid
	}
	if session.Status != "pending" && session.Status != "consumed" {
		return ErrInvalid
	}
	if (session.Status == "consumed") != (session.ConsumedAt != nil) {
		return ErrInvalid
	}
	return nil
}

// SessionStore provides durable one-time state and idempotent start replay.
type SessionStore interface {
	CreateOrReplay(context.Context, tenancy.Scope, Session) (Session, bool, error)
	Consume(context.Context, tenancy.Scope, string, string, string, time.Time) (Session, error)
}

func validRecordText(value string, max int) bool {
	return value != "" && len(value) <= max && value == strings.TrimSpace(value) && strings.IndexFunc(value, func(character rune) bool { return character < 0x20 }) < 0
}
