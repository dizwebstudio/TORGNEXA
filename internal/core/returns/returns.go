// Package returns defines the provider-neutral cancellation, return and
// refund-allocation contracts.  It deliberately keeps these lifecycles
// separate from the immutable order snapshot and from payments.Refund.
package returns

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidRecord = errors.New("returns: invalid record")
	ErrInvalidScope  = errors.New("returns: invalid tenant scope")
	ErrNotFound      = errors.New("returns: record not found")
	ErrConflict      = errors.New("returns: optimistic version conflict")
	ErrInvalidState  = errors.New("returns: invalid lifecycle transition")
	ErrOverAllocated = errors.New("returns: allocation exceeds source quantity or amount")
	ErrQuotaExceeded = errors.New("returns: quota exceeded")
)

var sortableIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}|[0-7][0-9A-HJKMNP-TV-Z]{25})$`)
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type CancellationID string
type ReturnID string
type ReturnItemID string
type RefundAllocationID string
type EvidenceID string

func parseID[T ~string](value string) (T, error) {
	var zero T
	if !sortableIDPattern.MatchString(value) {
		return zero, ErrInvalidRecord
	}
	return T(value), nil
}
func ParseCancellationID(v string) (CancellationID, error) { return parseID[CancellationID](v) }
func ParseReturnID(v string) (ReturnID, error)             { return parseID[ReturnID](v) }
func ParseReturnItemID(v string) (ReturnItemID, error)     { return parseID[ReturnItemID](v) }
func ParseRefundAllocationID(v string) (RefundAllocationID, error) {
	return parseID[RefundAllocationID](v)
}
func ParseEvidenceID(v string) (EvidenceID, error) { return parseID[EvidenceID](v) }

func (v CancellationID) String() string     { return string(v) }
func (v ReturnID) String() string           { return string(v) }
func (v ReturnItemID) String() string       { return string(v) }
func (v RefundAllocationID) String() string { return string(v) }
func (v EvidenceID) String() string         { return string(v) }
func (v CancellationID) Valid() bool        { return sortableIDPattern.MatchString(string(v)) }
func (v ReturnID) Valid() bool              { return sortableIDPattern.MatchString(string(v)) }
func (v ReturnItemID) Valid() bool          { return sortableIDPattern.MatchString(string(v)) }
func (v RefundAllocationID) Valid() bool    { return sortableIDPattern.MatchString(string(v)) }
func (v EvidenceID) Valid() bool            { return sortableIDPattern.MatchString(string(v)) }

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, workspace string) (Scope, error) {
	if !sortableIDPattern.MatchString(org) || !sortableIDPattern.MatchString(workspace) {
		return Scope{}, ErrInvalidScope
	}
	return Scope{organizationID: org, workspaceID: workspace}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return sortableIDPattern.MatchString(s.organizationID) && sortableIDPattern.MatchString(s.workspaceID)
}

type CancellationStatus string

const (
	CancellationRequested CancellationStatus = "requested"
	CancellationApproved  CancellationStatus = "approved"
	CancellationExecuting CancellationStatus = "executing"
	CancellationCancelled CancellationStatus = "cancelled"
	CancellationRejected  CancellationStatus = "rejected"
	CancellationFailed    CancellationStatus = "failed"
	CancellationUnknown   CancellationStatus = "unknown"
)

func (s CancellationStatus) Valid() bool {
	switch s {
	case CancellationRequested, CancellationApproved, CancellationExecuting, CancellationCancelled, CancellationRejected, CancellationFailed, CancellationUnknown:
		return true
	default:
		return false
	}
}

func ValidateCancellationTransition(from, to CancellationStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case CancellationRequested:
		if to == CancellationApproved || to == CancellationExecuting || to == CancellationCancelled || to == CancellationRejected || to == CancellationFailed || to == CancellationUnknown {
			return nil
		}
	case CancellationApproved:
		if to == CancellationExecuting || to == CancellationCancelled || to == CancellationFailed || to == CancellationUnknown {
			return nil
		}
	case CancellationExecuting:
		if to == CancellationCancelled || to == CancellationFailed || to == CancellationUnknown {
			return nil
		}
	}
	return ErrInvalidState
}

type ReturnStatus string

const (
	ReturnRequested         ReturnStatus = "requested"
	ReturnApproved          ReturnStatus = "approved"
	ReturnAuthorized        ReturnStatus = "authorized"
	ReturnInTransit         ReturnStatus = "in_transit"
	ReturnReceived          ReturnStatus = "received"
	ReturnInspecting        ReturnStatus = "inspecting"
	ReturnAccepted          ReturnStatus = "accepted"
	ReturnPartiallyAccepted ReturnStatus = "partially_accepted"
	ReturnRejected          ReturnStatus = "rejected"
	ReturnClosed            ReturnStatus = "closed"
	ReturnCancelled         ReturnStatus = "cancelled"
	ReturnExpired           ReturnStatus = "expired"
)

func (s ReturnStatus) Valid() bool {
	switch s {
	case ReturnRequested, ReturnApproved, ReturnAuthorized, ReturnInTransit, ReturnReceived, ReturnInspecting, ReturnAccepted, ReturnPartiallyAccepted, ReturnRejected, ReturnClosed, ReturnCancelled, ReturnExpired:
		return true
	default:
		return false
	}
}

func ValidateReturnTransition(from, to ReturnStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case ReturnRequested:
		if to == ReturnApproved || to == ReturnAuthorized || to == ReturnCancelled || to == ReturnExpired || to == ReturnRejected {
			return nil
		}
	case ReturnApproved:
		if to == ReturnAuthorized || to == ReturnCancelled || to == ReturnExpired {
			return nil
		}
	case ReturnAuthorized:
		if to == ReturnInTransit || to == ReturnCancelled {
			return nil
		}
	case ReturnInTransit:
		if to == ReturnReceived || to == ReturnCancelled {
			return nil
		}
	case ReturnReceived:
		if to == ReturnInspecting {
			return nil
		}
	case ReturnInspecting:
		if to == ReturnAccepted || to == ReturnPartiallyAccepted || to == ReturnRejected {
			return nil
		}
	case ReturnAccepted, ReturnPartiallyAccepted, ReturnRejected:
		if to == ReturnClosed {
			return nil
		}
	}
	return ErrInvalidState
}

type Disposition string

const (
	DispositionRestock    Disposition = "restock"
	DispositionQuarantine Disposition = "quarantine"
	DispositionScrap      Disposition = "scrap"
	DispositionReplace    Disposition = "replace"
)

func (d Disposition) Valid() bool {
	return d == DispositionRestock || d == DispositionQuarantine || d == DispositionScrap || d == DispositionReplace
}

// Quantity is exact decimal quantity. Zero is allowed for received/accepted
// snapshots; requested quantities must be positive.
type Quantity struct {
	Coefficient int64
	Scale       uint8
	Unit        string
}

func NewQuantity(coefficient int64, scale uint8, unit string) (Quantity, error) {
	if scale > 9 || !validUnit(unit) {
		return Quantity{}, ErrInvalidRecord
	}
	q := Quantity{Coefficient: coefficient, Scale: scale, Unit: unit}
	return q.normalized(), nil
}
func (q Quantity) normalized() Quantity {
	for q.Scale > 0 && q.Coefficient%10 == 0 {
		q.Coefficient /= 10
		q.Scale--
	}
	return q
}
func (q Quantity) Validate() error {
	if q.Scale > 9 || !validUnit(q.Unit) || q.normalized() != q {
		return ErrInvalidRecord
	}
	return nil
}
func (q Quantity) Positive() bool { return q.Coefficient > 0 }
func (q Quantity) Zero() bool     { return q.Coefficient == 0 }
func (q Quantity) Compare(other Quantity) (int, error) {
	if err := q.Validate(); err != nil {
		return 0, err
	}
	if err := other.Validate(); err != nil || q.Unit != other.Unit {
		return 0, ErrInvalidRecord
	}
	scale := q.Scale
	if other.Scale > scale {
		scale = other.Scale
	}
	a := new(big.Int).SetInt64(q.Coefficient)
	b := new(big.Int).SetInt64(other.Coefficient)
	a.Mul(a, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale-q.Scale)), nil))
	b.Mul(b, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale-other.Scale)), nil))
	return a.Cmp(b), nil
}

type CancellationRequest struct {
	ID             CancellationID
	OrganizationID string
	WorkspaceID    string
	OrderID        string
	Status         CancellationStatus
	ReasonCode     string
	Source         string
	Version        int64
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (c CancellationRequest) Validate() error {
	if !c.ID.Valid() || !sortableIDPattern.MatchString(c.OrganizationID) || !sortableIDPattern.MatchString(c.WorkspaceID) || !refPattern.MatchString(c.OrderID) || !c.Status.Valid() || !reasonPattern.MatchString(c.ReasonCode) || !refPattern.MatchString(c.Source) || c.Version < 1 || !utc(c.CreatedAt) || !utc(c.UpdatedAt) || c.UpdatedAt.Before(c.CreatedAt) || !refPattern.MatchString(c.IdempotencyKey) {
		return ErrInvalidRecord
	}
	return nil
}

type ReturnRequest struct {
	ID                     ReturnID
	OrganizationID         string
	WorkspaceID            string
	OrderID                string
	Status                 ReturnStatus
	ReasonCode             string
	Source                 string
	Currency               domain.Currency
	RequestedShippingMinor int64
	RequestedTaxMinor      int64
	Version                int64
	IdempotencyKey         string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (r ReturnRequest) Validate() error {
	if !r.ID.Valid() || !sortableIDPattern.MatchString(r.OrganizationID) || !sortableIDPattern.MatchString(r.WorkspaceID) || !refPattern.MatchString(r.OrderID) || !r.Status.Valid() || !reasonPattern.MatchString(r.ReasonCode) || !refPattern.MatchString(r.Source) || r.Currency.Validate() != nil || r.RequestedShippingMinor < 0 || r.RequestedTaxMinor < 0 || r.Version < 1 || !utc(r.CreatedAt) || !utc(r.UpdatedAt) || r.UpdatedAt.Before(r.CreatedAt) || !refPattern.MatchString(r.IdempotencyKey) {
		return ErrInvalidRecord
	}
	return nil
}

type ReturnItem struct {
	ID          ReturnItemID
	ReturnID    ReturnID
	OrderItemID string
	Requested   Quantity
	Received    Quantity
	Accepted    Quantity
	Disposition Disposition
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (i ReturnItem) Validate() error {
	if !i.ID.Valid() || !i.ReturnID.Valid() || !refPattern.MatchString(i.OrderItemID) || i.Requested.Validate() != nil || !i.Requested.Positive() || i.Received.Validate() != nil || i.Accepted.Validate() != nil || i.Received.Coefficient < 0 || i.Accepted.Coefficient < 0 || i.Requested.Unit != i.Received.Unit || i.Requested.Unit != i.Accepted.Unit || !i.Disposition.Valid() || i.Version < 1 || !utc(i.CreatedAt) || !utc(i.UpdatedAt) || i.UpdatedAt.Before(i.CreatedAt) {
		return ErrInvalidRecord
	}
	if cmp, _ := i.Accepted.Compare(i.Received); cmp > 0 {
		return ErrOverAllocated
	}
	if cmp, _ := i.Received.Compare(i.Requested); cmp > 0 {
		return ErrOverAllocated
	}
	return nil
}

// ValidateLineAllocation enforces the order-item and shipment boundaries.
func ValidateLineAllocation(orderQuantity, requested, received, accepted Quantity) error {
	for _, q := range []Quantity{orderQuantity, requested, received, accepted} {
		if err := q.Validate(); err != nil {
			return err
		}
	}
	if !requested.Positive() || received.Coefficient < 0 || accepted.Coefficient < 0 {
		return ErrInvalidRecord
	}
	if cmp, _ := requested.Compare(orderQuantity); cmp > 0 {
		return ErrOverAllocated
	}
	if cmp, _ := received.Compare(requested); cmp > 0 {
		return ErrOverAllocated
	}
	if cmp, _ := accepted.Compare(received); cmp > 0 {
		return ErrOverAllocated
	}
	return nil
}

type RefundComponent string

const (
	RefundComponentLine     RefundComponent = "line"
	RefundComponentShipping RefundComponent = "shipping"
	RefundComponentTax      RefundComponent = "tax"
	RefundComponentDiscount RefundComponent = "discount"
)

func (c RefundComponent) Valid() bool {
	return c == RefundComponentLine || c == RefundComponentShipping || c == RefundComponentTax || c == RefundComponentDiscount
}

type RefundAllocation struct {
	ID             RefundAllocationID
	OrganizationID string
	WorkspaceID    string
	PaymentID      string
	RefundID       string
	ReturnID       ReturnID
	OrderItemID    string
	Component      RefundComponent
	Amount         domain.Money
	Currency       domain.Currency
	IdempotencyKey string
	Version        int64
	CreatedAt      time.Time
}

func (a RefundAllocation) Validate() error {
	if !a.ID.Valid() || !sortableIDPattern.MatchString(a.OrganizationID) || !sortableIDPattern.MatchString(a.WorkspaceID) || !refPattern.MatchString(a.PaymentID) || !refPattern.MatchString(a.RefundID) || !a.ReturnID.Valid() || (a.OrderItemID != "" && !refPattern.MatchString(a.OrderItemID)) || !a.Component.Valid() || a.Amount.Validate() != nil || a.Amount.MinorUnits() <= 0 || a.Currency.Validate() != nil || a.Amount.Currency() != a.Currency || !refPattern.MatchString(a.IdempotencyKey) || a.Version < 1 || !utc(a.CreatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type InspectionResult struct {
	ID              EvidenceID
	ReturnID        ReturnID
	ReturnItemID    ReturnItemID
	Outcome         ReturnStatus
	ConditionCode   string
	DiscrepancyCode string
	Quantity        Quantity
	Disposition     Disposition
	ArtifactRef     string
	OccurredAt      time.Time
}

func (i InspectionResult) Validate() error {
	if !i.ID.Valid() || !i.ReturnID.Valid() || !i.ReturnItemID.Valid() || (i.Outcome != ReturnAccepted && i.Outcome != ReturnPartiallyAccepted && i.Outcome != ReturnRejected) || !reasonPattern.MatchString(i.ConditionCode) || (i.DiscrepancyCode != "" && !reasonPattern.MatchString(i.DiscrepancyCode)) || i.Quantity.Validate() != nil || !i.Disposition.Valid() || (i.ArtifactRef != "" && !refPattern.MatchString(i.ArtifactRef)) || !utc(i.OccurredAt) {
		return ErrInvalidRecord
	}
	// An accepted or partially accepted inspection must account for a positive
	// quantity. A rejected inspection may legitimately carry zero accepted
	// quantity while still recording the condition/disposition evidence.
	if i.Outcome != ReturnRejected && !i.Quantity.Positive() {
		return ErrInvalidRecord
	}
	return nil
}

type OperationEvidence struct {
	ID             EvidenceID
	OrganizationID string
	WorkspaceID    string
	OperationType  string
	OperationID    string
	Outcome        string
	ReasonCode     string
	RemoteID       string
	Digest         string
	CorrelationID  string
	CausationID    string
	OccurredAt     time.Time
}

func (e OperationEvidence) Validate() error {
	if !e.ID.Valid() || !sortableIDPattern.MatchString(e.OrganizationID) || !sortableIDPattern.MatchString(e.WorkspaceID) || !reasonPattern.MatchString(e.OperationType) || !refPattern.MatchString(e.OperationID) || !reasonPattern.MatchString(e.Outcome) || (e.ReasonCode != "" && !reasonPattern.MatchString(e.ReasonCode)) || (e.RemoteID != "" && !refPattern.MatchString(e.RemoteID)) || len(e.Digest) != 64 || !hexDigest(e.Digest) || !refPattern.MatchString(e.CorrelationID) || (e.CausationID != "" && !refPattern.MatchString(e.CausationID)) || !utc(e.OccurredAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID, CausationID string
	OccurredAt                                                    time.Time
}

func (m Mutation) Validate() error {
	if !refPattern.MatchString(m.EventID) || !sortableIDPattern.MatchString(m.AuditID) || !refPattern.MatchString(m.ActorID) || !refPattern.MatchString(m.Source) || !refPattern.MatchString(m.CorrelationID) || (m.CausationID != "" && !refPattern.MatchString(m.CausationID)) || !utc(m.OccurredAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Repository interface {
	Cancellation(context.Context, Scope, CancellationID) (CancellationRequest, error)
	CreateCancellation(context.Context, Scope, CancellationRequest, Mutation) (CancellationRequest, error)
	ChangeCancellationStatus(context.Context, Scope, CancellationID, CancellationStatus, int64, Mutation) (CancellationRequest, error)
	Return(context.Context, Scope, ReturnID) (ReturnRequest, error)
	CreateReturn(context.Context, Scope, ReturnRequest, Mutation) (ReturnRequest, error)
	ChangeReturnStatus(context.Context, Scope, ReturnID, ReturnStatus, int64, Mutation) (ReturnRequest, error)
	ListReturns(context.Context, Scope, int) ([]ReturnRequest, error)
	ReturnItems(context.Context, Scope, ReturnID, int) ([]ReturnItem, error)
	CreateReturnItem(context.Context, Scope, ReturnItem, Mutation) (ReturnItem, error)
	RecordInspection(context.Context, Scope, InspectionResult, Mutation) error
	CreateRefundAllocation(context.Context, Scope, RefundAllocation, Mutation) (RefundAllocation, error)
	ListEvidence(context.Context, Scope, string, int) ([]OperationEvidence, error)
}

func utc(t time.Time) bool { return !t.IsZero() && t.Location() == time.UTC }

func hexDigest(value string) bool {
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validUnit(unit string) bool {
	if unit == "" || len(unit) > 16 || strings.ToUpper(unit) != unit {
		return false
	}
	for _, c := range unit {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}
