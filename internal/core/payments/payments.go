// Package payments defines TORGNEXA's provider-neutral payment core: exact
// minor-unit amounts, remote-authoritative status, and idempotent
// create/refund/webhook semantics. No raw card data is modeled here or
// anywhere downstream (see ADR-0071).
package payments

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidRecord   = errors.New("payments: invalid record")
	ErrInvalidScope    = errors.New("payments: invalid tenant scope")
	ErrNotFound        = errors.New("payments: record not found")
	ErrConflict        = errors.New("payments: optimistic version conflict")
	ErrInvalidState    = errors.New("payments: invalid lifecycle transition")
	ErrRailUnavailable = errors.New("payments: rail account unavailable")
)

var (
	sortableIDPattern  = regexp.MustCompile(`^(?:[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}|[0-7][0-9A-HJKMNP-TV-Z]{25})$`)
	connectorIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	tokenPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	sourcePattern      = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	reasonPattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	refPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
	digestPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type PaymentID string
type RefundID string

func ParsePaymentID(v string) (PaymentID, error) {
	if !validSortableID(v) {
		return "", ErrInvalidRecord
	}
	return PaymentID(v), nil
}
func ParseRefundID(v string) (RefundID, error) {
	if !validSortableID(v) {
		return "", ErrInvalidRecord
	}
	return RefundID(v), nil
}
func (id PaymentID) String() string { return string(id) }
func (id RefundID) String() string  { return string(id) }
func (id PaymentID) Valid() bool    { return validSortableID(string(id)) }
func (id RefundID) Valid() bool     { return validSortableID(string(id)) }

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, ws string) (Scope, error) {
	if !validSortableID(org) || !validSortableID(ws) {
		return Scope{}, ErrInvalidScope
	}
	return Scope{organizationID: org, workspaceID: ws}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return validSortableID(s.organizationID) && validSortableID(s.workspaceID)
}

// Status is TORGNEXA's canonical payment lifecycle. RemoteStatus on Payment
// carries the provider's raw status string for audit; Status is what the
// rest of the system reasons about.
type Status string

const (
	StatusPending           Status = "pending"
	StatusCreated           Status = "created"
	StatusSucceeded         Status = "succeeded"
	StatusFailed            Status = "failed"
	StatusCanceled          Status = "canceled"
	StatusRefunded          Status = "refunded"
	StatusPartiallyRefunded Status = "partially_refunded"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusCreated, StatusSucceeded, StatusFailed, StatusCanceled, StatusRefunded, StatusPartiallyRefunded:
		return true
	default:
		return false
	}
}

func ValidatePaymentTransition(from, to Status) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case StatusPending:
		if to == StatusCreated || to == StatusFailed || to == StatusCanceled {
			return nil
		}
	case StatusCreated:
		if to == StatusSucceeded || to == StatusFailed || to == StatusCanceled {
			return nil
		}
	case StatusSucceeded:
		if to == StatusRefunded || to == StatusPartiallyRefunded {
			return nil
		}
	case StatusPartiallyRefunded:
		if to == StatusRefunded {
			return nil
		}
	}
	return ErrInvalidState
}

type Payment struct {
	ID                   PaymentID
	OrganizationID       string
	WorkspaceID          string
	ConnectorAccountID   string
	ExternalID           string
	RemoteID             string
	Purpose              string
	Amount               domain.Money
	CommissionMinorUnits int64
	Status               Status
	RemoteStatus         string
	ReasonCode           string
	Version              int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
	SucceededAt          *time.Time
}

func (p Payment) Validate() error {
	if !p.ID.Valid() || !validSortableID(p.OrganizationID) || !validSortableID(p.WorkspaceID) ||
		!connectorIDPattern.MatchString(p.ConnectorAccountID) || !refPattern.MatchString(p.ExternalID) ||
		(p.RemoteID != "" && !refPattern.MatchString(p.RemoteID)) ||
		!validOptionalText(p.Purpose, 210, false) || p.Amount.Validate() != nil ||
		p.CommissionMinorUnits < 0 || !p.Status.Valid() || !validOptionalToken(p.RemoteStatus) ||
		!validReason(p.ReasonCode) || !validMetadata(p.Version, p.CreatedAt, p.UpdatedAt) || !isUTC(p.ExpiresAt) {
		return ErrInvalidRecord
	}
	if p.Status == StatusPending && p.RemoteID != "" {
		return ErrInvalidRecord
	}
	if p.Status != StatusPending && p.RemoteID == "" {
		return ErrInvalidRecord
	}
	if p.Status == StatusFailed && p.ReasonCode == "" {
		return ErrInvalidRecord
	}
	if p.Status != StatusFailed && p.ReasonCode != "" {
		return ErrInvalidRecord
	}
	if isTerminalSuccess(p.Status) {
		if p.SucceededAt == nil || !isUTC(*p.SucceededAt) || p.SucceededAt.Before(p.CreatedAt) {
			return ErrInvalidRecord
		}
	} else if p.SucceededAt != nil {
		return ErrInvalidRecord
	}
	return nil
}

func isTerminalSuccess(s Status) bool {
	return s == StatusSucceeded || s == StatusRefunded || s == StatusPartiallyRefunded
}

type CreatePayment struct {
	ID                 PaymentID
	ConnectorAccountID string
	ExternalID         string
	Purpose            string
	Amount             domain.Money
	ExpiresAt          time.Time
}

func (c CreatePayment) Validate() error {
	if !c.ID.Valid() || !connectorIDPattern.MatchString(c.ConnectorAccountID) || !refPattern.MatchString(c.ExternalID) ||
		!validOptionalText(c.Purpose, 210, false) || c.Amount.Validate() != nil || !isUTC(c.ExpiresAt) || !c.ExpiresAt.After(time.Now().UTC()) {
		return ErrInvalidRecord
	}
	return nil
}

// ChangePaymentStatus applies one remote-authoritative status observation.
// RemoteID is set exactly once (on the pending->created transition);
// CommissionMinorUnits and RemoteStatus are optional refinements the
// transport may not always know.
type ChangePaymentStatus struct {
	ID                   PaymentID
	ExpectedVersion      int64
	Status               Status
	RemoteID             string
	RemoteStatus         string
	CommissionMinorUnits int64
	ReasonCode           string
	SucceededAt          *time.Time
}

func (c ChangePaymentStatus) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !c.Status.Valid() || !validOptionalToken(c.RemoteStatus) ||
		c.CommissionMinorUnits < 0 || !validReason(c.ReasonCode) {
		return ErrInvalidRecord
	}
	if c.RemoteID != "" && !refPattern.MatchString(c.RemoteID) {
		return ErrInvalidRecord
	}
	if c.Status == StatusFailed && c.ReasonCode == "" {
		return ErrInvalidRecord
	}
	if c.Status != StatusFailed && c.ReasonCode != "" {
		return ErrInvalidRecord
	}
	if isTerminalSuccess(c.Status) {
		if c.Status == StatusSucceeded && (c.SucceededAt == nil || !isUTC(*c.SucceededAt)) {
			return ErrInvalidRecord
		}
	} else if c.SucceededAt != nil {
		return ErrInvalidRecord
	}
	return nil
}

type RefundStatus string

const (
	RefundPending   RefundStatus = "pending"
	RefundAccepted  RefundStatus = "accepted"
	RefundSucceeded RefundStatus = "succeeded"
	RefundFailed    RefundStatus = "failed"
)

func (s RefundStatus) Valid() bool {
	switch s {
	case RefundPending, RefundAccepted, RefundSucceeded, RefundFailed:
		return true
	default:
		return false
	}
}

func ValidateRefundTransition(from, to RefundStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case RefundPending:
		if to == RefundAccepted || to == RefundFailed {
			return nil
		}
	case RefundAccepted:
		if to == RefundSucceeded || to == RefundFailed {
			return nil
		}
	}
	return ErrInvalidState
}

type Refund struct {
	ID             RefundID
	OrganizationID string
	WorkspaceID    string
	PaymentID      PaymentID
	ExternalID     string
	RemoteRefundID string
	Amount         domain.Money
	Status         RefundStatus
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (r Refund) Validate() error {
	if !r.ID.Valid() || !validSortableID(r.OrganizationID) || !validSortableID(r.WorkspaceID) || !r.PaymentID.Valid() ||
		!refPattern.MatchString(r.ExternalID) || (r.RemoteRefundID != "" && !refPattern.MatchString(r.RemoteRefundID)) ||
		r.Amount.Validate() != nil || !r.Status.Valid() || !validMetadata(r.Version, r.CreatedAt, r.UpdatedAt) {
		return ErrInvalidRecord
	}
	if r.Status == RefundPending && r.RemoteRefundID != "" {
		return ErrInvalidRecord
	}
	if r.Status != RefundPending && r.RemoteRefundID == "" {
		return ErrInvalidRecord
	}
	return nil
}

type CreateRefund struct {
	ID         RefundID
	PaymentID  PaymentID
	ExternalID string
	Amount     domain.Money
}

func (c CreateRefund) Validate() error {
	if !c.ID.Valid() || !c.PaymentID.Valid() || !refPattern.MatchString(c.ExternalID) || c.Amount.Validate() != nil {
		return ErrInvalidRecord
	}
	return nil
}

type ChangeRefundStatus struct {
	ID              RefundID
	ExpectedVersion int64
	Status          RefundStatus
	RemoteRefundID  string
}

func (c ChangeRefundStatus) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !c.Status.Valid() {
		return ErrInvalidRecord
	}
	if c.RemoteRefundID != "" && !refPattern.MatchString(c.RemoteRefundID) {
		return ErrInvalidRecord
	}
	return nil
}

// WebhookEvidence is append-only, verified-delivery proof used only for
// replay dedup and audit. It never carries an unverified body.
type WebhookEvidence struct {
	DeliveryID         string
	ConnectorAccountID string
	RemotePaymentID    string
	EventType          string
	BodyDigest         string
	VerifiedAt         time.Time
}

func (e WebhookEvidence) Validate() error {
	if !refPattern.MatchString(e.DeliveryID) || !connectorIDPattern.MatchString(e.ConnectorAccountID) ||
		!refPattern.MatchString(e.RemotePaymentID) || !tokenPattern.MatchString(e.EventType) ||
		!digestPattern.MatchString(e.BodyDigest) || !isUTC(e.VerifiedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

func (m Mutation) Validate() error {
	if !validToken(m.EventID) || !validSortableID(m.AuditID) || !validToken(m.ActorID) || !sourcePattern.MatchString(m.Source) ||
		!validToken(m.CorrelationID) || !validOptionalToken(m.CausationID) || !validOptionalToken(m.TraceID) || !isUTC(m.OccurredAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Repository interface {
	Payment(context.Context, Scope, PaymentID) (Payment, error)
	PaymentByExternalID(context.Context, Scope, string) (Payment, error)
	// PaymentByRemoteID looks up a payment by (connectorAccountID, remoteID) —
	// the pair a verified webhook delivery actually carries, since the
	// provider knows nothing about our ExternalID.
	PaymentByRemoteID(context.Context, Scope, string, string) (Payment, error)
	ListPayments(context.Context, Scope, int) ([]Payment, error)
	CreatePayment(context.Context, Scope, CreatePayment, Mutation) (Payment, error)
	ChangePaymentStatus(context.Context, Scope, ChangePaymentStatus, Mutation) (Payment, error)
	Refund(context.Context, Scope, RefundID) (Refund, error)
	CreateRefund(context.Context, Scope, CreateRefund, Mutation) (Refund, error)
	ChangeRefundStatus(context.Context, Scope, ChangeRefundStatus, Mutation) (Refund, error)
	RecordWebhookEvidence(context.Context, Scope, WebhookEvidence) (bool, error)
}

func validMetadata(version int64, createdAt, updatedAt time.Time) bool {
	return version >= 1 && isUTC(createdAt) && isUTC(updatedAt) && !updatedAt.Before(createdAt)
}
func isUTC(v time.Time) bool           { return !v.IsZero() && v.Location() == time.UTC }
func validSortableID(v string) bool    { return sortableIDPattern.MatchString(v) }
func validToken(v string) bool         { return tokenPattern.MatchString(v) }
func validOptionalToken(v string) bool { return v == "" || validToken(v) }
func validReason(v string) bool        { return v == "" || reasonPattern.MatchString(v) }
func validOptionalText(v string, max int, layout bool) bool {
	if v == "" {
		return true
	}
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	if utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			if layout && (r == '\n' || r == '\r' || r == '\t') {
				continue
			}
			return false
		}
	}
	return true
}
