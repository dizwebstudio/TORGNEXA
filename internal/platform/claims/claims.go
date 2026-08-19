// Package claims implements auditable claims/disputes with released evidence and SLA escalation.
package claims

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"time"
)

var (
	ErrInvalid             = errors.New("claims: invalid value")
	ErrEvidenceNotReleased = errors.New("claims: evidence is not released")
)

type Context string

const (
	ContextMarketplace Context = "marketplace"
	ContextCarrier     Context = "carrier"
	ContextSupplier    Context = "supplier"
)

func (c Context) Valid() bool {
	return c == ContextMarketplace || c == ContextCarrier || c == ContextSupplier
}

type State string

const (
	StateOpen      State = "open"
	StateSubmitted State = "submitted"
	StateWaiting   State = "waiting"
	StateWon       State = "won"
	StateLost      State = "lost"
	StateClosed    State = "closed"
)

type Evidence struct {
	ID, UploadID, ObjectRef, MediaType string
	AddedAt                            time.Time
}
type Compensation struct {
	Amount                        domain.Money
	SettlementEntryID, PaymentRef string
}
type Claim struct {
	ID                                           string
	Context                                      Context
	State                                        State
	OrderID, ProviderRef, CarrierRef, SupplierID string
	Evidence                                     []Evidence
	Deadline                                     time.Time
	EscalationAt                                 time.Time
	Compensation                                 *Compensation
	Version                                      int64
	UpdatedAt                                    time.Time
}
type ReleaseVerifier interface {
	Released(context.Context, tenancy.Scope, string) bool
}
type AuditPort interface {
	Record(context.Context, tenancy.Scope, string, string, time.Time) error
}
type Service struct {
	Verifier ReleaseVerifier
	Audit    AuditPort
}

func (s Service) AddEvidence(ctx context.Context, scope tenancy.Scope, c Claim, e Evidence, now time.Time) (Claim, error) {
	if !scope.Valid() || c.ID == "" || !c.Context.Valid() || c.Version < 1 || e.ID == "" || e.UploadID == "" || e.ObjectRef == "" || e.MediaType == "" || now.IsZero() || s.Verifier == nil {
		return Claim{}, ErrInvalid
	}
	if !s.Verifier.Released(ctx, scope, e.UploadID) {
		return Claim{}, ErrEvidenceNotReleased
	}
	e.AddedAt = now.UTC()
	c.Evidence = append(c.Evidence, e)
	c.Version++
	c.UpdatedAt = now.UTC()
	if s.Audit != nil {
		if err := s.Audit.Record(ctx, scope, c.ID, "evidence.added", now.UTC()); err != nil {
			return Claim{}, err
		}
	}
	return c, nil
}
func DueForEscalation(c Claim, now time.Time) bool {
	return c.ID != "" && !c.EscalationAt.IsZero() && !now.Before(c.EscalationAt) && (c.State == StateOpen || c.State == StateSubmitted || c.State == StateWaiting)
}
