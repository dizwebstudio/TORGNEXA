// Package pricing defines provider-neutral, money-safe canonical prices.
package pricing

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidRecord = errors.New("pricing: invalid record")
	ErrInvalidScope  = errors.New("pricing: invalid tenant scope")
	ErrNotFound      = errors.New("pricing: price not found")
	ErrConflict      = errors.New("pricing: optimistic version conflict")
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type PriceID string
type OfferID string

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, ws string) (Scope, error) {
	if !domain.ValidTenantScope(org, ws) {
		return Scope{}, ErrInvalidScope
	}
	return Scope{org, ws}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return domain.ValidTenantScope(s.organizationID, s.workspaceID)
}
func ParsePriceID(v string) (PriceID, error) {
	if !domain.ValidSortableID(v) {
		return "", ErrInvalidRecord
	}
	return PriceID(v), nil
}
func ParseOfferID(v string) (OfferID, error) {
	if !domain.ValidSortableID(v) {
		return "", ErrInvalidRecord
	}
	return OfferID(v), nil
}
func (id PriceID) String() string { return string(id) }
func (id PriceID) Valid() bool    { return domain.ValidSortableID(string(id)) }
func (id OfferID) String() string { return string(id) }
func (id OfferID) Valid() bool    { return domain.ValidSortableID(string(id)) }

type Currency string

func NewCurrency(code string) (Currency, error) {
	if len(code) != 3 {
		return "", ErrInvalidRecord
	}
	for _, c := range []byte(code) {
		if c < 'A' || c > 'Z' {
			return "", ErrInvalidRecord
		}
	}
	return Currency(code), nil
}
func (c Currency) String() string  { return string(c) }
func (c Currency) Validate() error { _, e := NewCurrency(string(c)); return e }

type Money struct {
	minorUnits int64
	currency   Currency
}

func NewMoney(minorUnits int64, currency Currency) (Money, error) {
	if currency.Validate() != nil {
		return Money{}, ErrInvalidRecord
	}
	return Money{minorUnits: minorUnits, currency: currency}, nil
}
func (m Money) MinorUnits() int64  { return m.minorUnits }
func (m Money) Currency() Currency { return m.currency }
func (m Money) Validate() error    { _, e := NewMoney(m.minorUnits, m.currency); return e }

type Kind string

const (
	KindRegular   Kind = "regular"
	KindCompareAt Kind = "compare_at"
	KindCost      Kind = "cost"
)

func (k Kind) Valid() bool { return k == KindRegular || k == KindCompareAt || k == KindCost }

type Price struct {
	ID             PriceID
	OrganizationID string
	WorkspaceID    string
	OfferID        OfferID
	Kind           Kind
	Amount         Money
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (p Price) Validate() error {
	if !p.ID.Valid() || !domain.ValidSortableID(p.OrganizationID) || !domain.ValidSortableID(p.WorkspaceID) || !p.OfferID.Valid() || !p.Kind.Valid() || p.Amount.Validate() != nil || p.Amount.MinorUnits() < 0 || p.Version < 1 || !isUTC(p.CreatedAt) || !isUTC(p.UpdatedAt) || p.UpdatedAt.Before(p.CreatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type CreatePrice struct {
	ID      PriceID
	OfferID OfferID
	Kind    Kind
	Amount  Money
}

func (c CreatePrice) Validate() error {
	if !c.ID.Valid() || !c.OfferID.Valid() || !c.Kind.Valid() || c.Amount.Validate() != nil || c.Amount.MinorUnits() < 0 {
		return ErrInvalidRecord
	}
	return nil
}

type UpdatePrice struct {
	ID              PriceID
	ExpectedVersion int64
	Amount          Money
}

func (c UpdatePrice) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || c.Amount.Validate() != nil || c.Amount.MinorUnits() < 0 {
		return ErrInvalidRecord
	}
	return nil
}

// Mutation carries one event id and one audit id so both intents can commit with the price row.
type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

func (m Mutation) Validate() error {
	if !domain.ValidToken(m.EventID) || !domain.ValidSortableID(m.AuditID) || !domain.ValidToken(m.ActorID) || !validSource(m.Source) || !domain.ValidToken(m.CorrelationID) || !validOptionalToken(m.CausationID) || !validOptionalToken(m.TraceID) || !isUTC(m.OccurredAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Repository interface {
	Price(context.Context, Scope, PriceID) (Price, error)
	PricesByOffer(context.Context, Scope, OfferID, int) ([]Price, error)
	Create(context.Context, Scope, CreatePrice, Mutation) (Price, error)
	Update(context.Context, Scope, UpdatePrice, Mutation) (Price, error)
}

func validSource(v string) bool {
	if v == "" || len(v) > 128 || v != strings.ToLower(v) {
		return false
	}
	for i, c := range []byte(v) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '.' || c == '_' || c == '-')) {
			continue
		}
		return false
	}
	return true
}
func validOptionalToken(v string) bool { return v == "" || domain.ValidToken(v) }
func isUTC(v time.Time) bool           { return !v.IsZero() && v.Location() == time.UTC }
