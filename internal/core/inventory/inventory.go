// Package inventory defines canonical warehouses and exact inventory positions.
package inventory

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidRecord         = errors.New("inventory: invalid record")
	ErrInvalidScope          = errors.New("inventory: invalid tenant scope")
	ErrNotFound              = errors.New("inventory: record not found")
	ErrConflict              = errors.New("inventory: optimistic version conflict")
	ErrInsufficientAvailable = errors.New("inventory: insufficient available quantity")
	ErrInsufficientReserved  = errors.New("inventory: insufficient reserved quantity")
	ErrWarehouseInactive     = errors.New("inventory: warehouse is inactive")
)
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

var unitPattern = regexp.MustCompile(`^[A-Z][A-Z0-9._-]{0,15}$`)

const MaxDecimalScale = 9

// Decimal is the exact fixed-point representation used by inventory. Its
// coefficient+scale invariants mirror the shared 076a wire primitive without
// introducing a cross-Core dependency.
type Decimal struct {
	coefficient int64
	scale       uint8
}

func NewDecimal(coefficient int64, scale uint8) (Decimal, error) {
	if scale > MaxDecimalScale {
		return Decimal{}, fmt.Errorf("inventory: decimal scale %d exceeds maximum %d", scale, MaxDecimalScale)
	}
	return normalizeDecimal(Decimal{coefficient: coefficient, scale: scale}), nil
}

func ParseDecimal(input string) (Decimal, error) {
	if input == "" || strings.TrimSpace(input) != input || strings.HasPrefix(input, "+") {
		return Decimal{}, ErrInvalidRecord
	}
	negative := false
	digits := input
	if strings.HasPrefix(digits, "-") {
		negative = true
		digits = digits[1:]
	}
	if digits == "" {
		return Decimal{}, ErrInvalidRecord
	}
	parts := strings.Split(digits, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return Decimal{}, ErrInvalidRecord
	}
	for _, part := range parts {
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return Decimal{}, ErrInvalidRecord
			}
		}
	}
	if len(parts[0]) > 1 && parts[0][0] == '0' {
		return Decimal{}, ErrInvalidRecord
	}
	scale := 0
	allDigits := parts[0]
	if len(parts) == 2 {
		scale = len(parts[1])
		if scale > MaxDecimalScale {
			return Decimal{}, ErrInvalidRecord
		}
		allDigits += parts[1]
	}
	if negative && strings.Trim(allDigits, "0") == "" {
		return Decimal{}, ErrInvalidRecord
	}
	value := new(big.Int)
	if _, ok := value.SetString(allDigits, 10); !ok {
		return Decimal{}, ErrInvalidRecord
	}
	if negative {
		value.Neg(value)
	}
	if !value.IsInt64() {
		return Decimal{}, ErrInvalidRecord
	}
	return NewDecimal(value.Int64(), uint8(scale))
}

func normalizeDecimal(v Decimal) Decimal {
	for v.scale > 0 && v.coefficient%10 == 0 {
		v.coefficient /= 10
		v.scale--
	}
	return v
}

func (d Decimal) Coefficient() int64 { return d.coefficient }
func (d Decimal) Scale() uint8       { return d.scale }
func (d Decimal) IsZero() bool       { return d.coefficient == 0 }

func (d Decimal) Validate() error {
	if d.scale > MaxDecimalScale || normalizeDecimal(d) != d {
		return ErrInvalidRecord
	}
	return nil
}

func (d Decimal) String() string {
	if d.coefficient == 0 {
		return "0"
	}
	negative := d.coefficient < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(d.coefficient + 1)) + 1
	} else {
		magnitude = uint64(d.coefficient)
	}
	digits := strconv.FormatUint(magnitude, 10)
	if d.scale > 0 {
		for len(digits) <= int(d.scale) {
			digits = "0" + digits
		}
		cut := len(digits) - int(d.scale)
		digits = digits[:cut] + "." + digits[cut:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func alignDecimals(left, right Decimal) (int64, int64, uint8, error) {
	if left.Validate() != nil || right.Validate() != nil {
		return 0, 0, 0, ErrInvalidRecord
	}
	target := left.scale
	if right.scale > target {
		target = right.scale
	}
	scaleValue := func(v Decimal) (int64, error) {
		coefficient := big.NewInt(v.coefficient)
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(target-v.scale)), nil)
		coefficient.Mul(coefficient, factor)
		if !coefficient.IsInt64() {
			return 0, ErrInvalidRecord
		}
		return coefficient.Int64(), nil
	}
	l, err := scaleValue(left)
	if err != nil {
		return 0, 0, 0, err
	}
	r, err := scaleValue(right)
	if err != nil {
		return 0, 0, 0, err
	}
	return l, r, target, nil
}

func (d Decimal) Add(other Decimal) (Decimal, error) {
	left, right, scale, err := alignDecimals(d, other)
	if err != nil {
		return Decimal{}, err
	}
	sum := new(big.Int).Add(big.NewInt(left), big.NewInt(right))
	if !sum.IsInt64() {
		return Decimal{}, ErrInvalidRecord
	}
	return NewDecimal(sum.Int64(), scale)
}

func (d Decimal) Sub(other Decimal) (Decimal, error) {
	left, right, scale, err := alignDecimals(d, other)
	if err != nil {
		return Decimal{}, err
	}
	difference := new(big.Int).Sub(big.NewInt(left), big.NewInt(right))
	if !difference.IsInt64() {
		return Decimal{}, ErrInvalidRecord
	}
	return NewDecimal(difference.Int64(), scale)
}

func (d Decimal) Cmp(other Decimal) (int, error) {
	left, right, _, err := alignDecimals(d, other)
	if err != nil {
		return 0, err
	}
	switch {
	case left < right:
		return -1, nil
	case left > right:
		return 1, nil
	default:
		return 0, nil
	}
}

type UnitCode string

func NewUnitCode(code string) (UnitCode, error) {
	if !unitPattern.MatchString(code) {
		return "", ErrInvalidRecord
	}
	return UnitCode(code), nil
}
func (u UnitCode) Validate() error { _, err := NewUnitCode(string(u)); return err }
func (u UnitCode) String() string  { return string(u) }

type Quantity struct {
	Value Decimal
	Unit  UnitCode
}

func NewQuantity(value Decimal, unit UnitCode) (Quantity, error) {
	if value.Validate() != nil || unit.Validate() != nil {
		return Quantity{}, ErrInvalidRecord
	}
	return Quantity{Value: value, Unit: unit}, nil
}
func (q Quantity) Validate() error { _, err := NewQuantity(q.Value, q.Unit); return err }

type WarehouseID string
type PositionID string
type OfferID string
type Scope struct{ organizationID, workspaceID string }

func ParseScope(o, w string) (Scope, error) {
	if !domain.ValidTenantScope(o, w) {
		return Scope{}, ErrInvalidScope
	}
	return Scope{o, w}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return domain.ValidTenantScope(s.organizationID, s.workspaceID)
}
func (id WarehouseID) String() string { return string(id) }
func (id WarehouseID) Valid() bool    { return domain.ValidSortableID(string(id)) }
func (id PositionID) String() string  { return string(id) }
func (id PositionID) Valid() bool     { return domain.ValidSortableID(string(id)) }
func (id OfferID) String() string     { return string(id) }
func (id OfferID) Valid() bool        { return domain.ValidSortableID(string(id)) }

type WarehouseStatus string

const (
	WarehouseActive   WarehouseStatus = "active"
	WarehouseDisabled WarehouseStatus = "disabled"
)

func (s WarehouseStatus) Valid() bool { return s == WarehouseActive || s == WarehouseDisabled }

type Warehouse struct {
	ID                                      WarehouseID
	OrganizationID, WorkspaceID, Code, Name string
	Status                                  WarehouseStatus
	Version                                 int64
	CreatedAt, UpdatedAt                    time.Time
}

func (w Warehouse) Validate() error {
	if !w.ID.Valid() || !domain.ValidSortableID(w.OrganizationID) || !domain.ValidSortableID(w.WorkspaceID) || !validCode(w.Code) || !validName(w.Name) || !w.Status.Valid() || w.Version < 1 || !isUTC(w.CreatedAt) || !isUTC(w.UpdatedAt) || w.UpdatedAt.Before(w.CreatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Position struct {
	ID                          PositionID
	OrganizationID, WorkspaceID string
	OfferID                     OfferID
	WarehouseID                 WarehouseID
	OnHand, Reserved            Quantity
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (p Position) Validate() error {
	if !p.ID.Valid() || !domain.ValidSortableID(p.OrganizationID) || !domain.ValidSortableID(p.WorkspaceID) || !p.OfferID.Valid() || !p.WarehouseID.Valid() || p.OnHand.Validate() != nil || p.Reserved.Validate() != nil || p.OnHand.Unit != p.Reserved.Unit || p.Version < 1 || !isUTC(p.CreatedAt) || !isUTC(p.UpdatedAt) || p.UpdatedAt.Before(p.CreatedAt) {
		return ErrInvalidRecord
	}
	oh, _ := p.OnHand.Value.Cmp(mustZero())
	rv, _ := p.Reserved.Value.Cmp(mustZero())
	cmp, _ := p.Reserved.Value.Cmp(p.OnHand.Value)
	if oh < 0 || rv < 0 || cmp > 0 {
		return ErrInvalidRecord
	}
	return nil
}
func (p Position) Available() (Quantity, error) {
	v, err := p.OnHand.Value.Sub(p.Reserved.Value)
	if err != nil {
		return Quantity{}, err
	}
	return NewQuantity(v, p.OnHand.Unit)
}

type CreateWarehouse struct {
	ID         WarehouseID
	Code, Name string
}

func (c CreateWarehouse) Validate() error {
	if !c.ID.Valid() || !validCode(c.Code) || !validName(c.Name) {
		return ErrInvalidRecord
	}
	return nil
}

type ChangeWarehouseStatus struct {
	ID              WarehouseID
	ExpectedVersion int64
	Status          WarehouseStatus
}

func (c ChangeWarehouseStatus) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !c.Status.Valid() {
		return ErrInvalidRecord
	}
	return nil
}

type CreatePosition struct {
	ID          PositionID
	OfferID     OfferID
	WarehouseID WarehouseID
	Unit        UnitCode
}

func (c CreatePosition) Validate() error {
	if !c.ID.Valid() || !c.OfferID.Valid() || !c.WarehouseID.Valid() || c.Unit.Validate() != nil {
		return ErrInvalidRecord
	}
	return nil
}

type ChangeQuantity struct {
	ID              PositionID
	ExpectedVersion int64
	Quantity        Quantity
	Reason          string
}

func (c ChangeQuantity) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || c.Quantity.Validate() != nil || !validReason(c.Reason) {
		return ErrInvalidRecord
	}
	cmp, _ := c.Quantity.Value.Cmp(mustZero())
	if cmp < 0 {
		return ErrInvalidRecord
	}
	return nil
}

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
	Warehouse(context.Context, Scope, WarehouseID) (Warehouse, error)
	Position(context.Context, Scope, PositionID) (Position, error)
	CreateWarehouse(context.Context, Scope, CreateWarehouse, Mutation) (Warehouse, error)
	ChangeWarehouseStatus(context.Context, Scope, ChangeWarehouseStatus, Mutation) (Warehouse, error)
	CreatePosition(context.Context, Scope, CreatePosition, Mutation) (Position, error)
	SetOnHand(context.Context, Scope, ChangeQuantity, Mutation) (Position, error)
	Reserve(context.Context, Scope, ChangeQuantity, Mutation) (Position, error)
	Release(context.Context, Scope, ChangeQuantity, Mutation) (Position, error)
	ConsumeReserved(context.Context, Scope, ChangeQuantity, Mutation) (Position, error)
}

func mustZero() Decimal { v, _ := NewDecimal(0, 0); return v }
func validCode(v string) bool {
	return len(v) >= 1 && len(v) <= 128 && identifierPattern.MatchString(v)
}
func validName(v string) bool { return v == strings.TrimSpace(v) && len(v) >= 1 && len(v) <= 300 }
func validReason(v string) bool {
	return len(v) >= 1 && len(v) <= 128 && identifierPattern.MatchString(v)
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

// OperationalState is independent from the administrative warehouse status.
// UNAVAILABLE and LOST are hard allocation stops; DEGRADED stays eligible for
// explicitly configured failover when stock exists.
type OperationalState string

const (
	OperationalActive      OperationalState = "active"
	OperationalDegraded    OperationalState = "degraded"
	OperationalUnavailable OperationalState = "unavailable"
	OperationalLost        OperationalState = "lost"
)

func (s OperationalState) Valid() bool {
	return s == OperationalActive || s == OperationalDegraded || s == OperationalUnavailable || s == OperationalLost
}

type WarehouseOperationalState struct {
	WarehouseID WarehouseID
	State       OperationalState
	ReasonCode  string
	Version     int64
	ChangedAt   time.Time
}
type FailoverRoute struct {
	SourceWarehouseID, DestinationWarehouseID WarehouseID
	Priority                                  int
	Enabled                                   bool
	Version                                   int64
	UpdatedAt                                 time.Time
}
type FailoverDecision struct {
	ID                     string
	SourceWarehouseID      WarehouseID
	DestinationWarehouseID WarehouseID
	OfferID                OfferID
	Routed                 bool
	ExecutionStatus        string
	ExecutionReason        string
	ReroutedAllocations    int
	OccurredAt             time.Time
}

// WarehouseIncident tracks automated operational recovery for a warehouse that
// became unavailable or was physically lost. It is metadata-only evidence: it
// never implies that stock moved between warehouses.
type WarehouseIncidentStatus string

const (
	WarehouseIncidentOpen           WarehouseIncidentStatus = "open"
	WarehouseIncidentProcessing     WarehouseIncidentStatus = "processing"
	WarehouseIncidentCompleted      WarehouseIncidentStatus = "completed"
	WarehouseIncidentNeedsAttention WarehouseIncidentStatus = "needs_attention"
	WarehouseIncidentResolved       WarehouseIncidentStatus = "resolved"
)

func (s WarehouseIncidentStatus) Valid() bool {
	return s == WarehouseIncidentOpen || s == WarehouseIncidentProcessing || s == WarehouseIncidentCompleted || s == WarehouseIncidentNeedsAttention || s == WarehouseIncidentResolved
}

type WarehouseIncident struct {
	ID                      string
	WarehouseID             WarehouseID
	OperationalState        OperationalState
	ReasonCode              string
	Status                  WarehouseIncidentStatus
	CursorOfferID           OfferID
	RoutedCount             int
	NoRouteCount            int
	ReroutedAllocationCount int
	ExecutionAttentionCount int
	OpenedAt                time.Time
	UpdatedAt               time.Time
	CompletedAt             *time.Time
}

// WarehouseIncidentDecision is append-only per incident+offer. Routed means a
// pre-approved healthy destination with positive ATP was found; it does not
// reserve, transfer or fabricate inventory.
type WarehouseIncidentDecision struct {
	IncidentID             string
	OfferID                OfferID
	DestinationWarehouseID WarehouseID
	Routed                 bool
	ExecutionStatus        string
	ExecutionReason        string
	ReroutedAllocations    int
	OccurredAt             time.Time
}

// FulfillmentAllocation is the durable ownership of one immutable order item
// reservation. Warehouse identity is immutable: failover releases the source
// allocation and creates a replacement allocation at the destination.
type FulfillmentAllocationStatus string

const (
	FulfillmentReserved  FulfillmentAllocationStatus = "reserved"
	FulfillmentReleased  FulfillmentAllocationStatus = "released"
	FulfillmentConsumed  FulfillmentAllocationStatus = "consumed"
	FulfillmentCancelled FulfillmentAllocationStatus = "cancelled"
)

func (s FulfillmentAllocationStatus) Valid() bool {
	return s == FulfillmentReserved || s == FulfillmentReleased || s == FulfillmentConsumed || s == FulfillmentCancelled
}

type FulfillmentAllocation struct {
	ID, IdempotencyKey, OrderID, OrderItemID string
	OfferID                                  OfferID
	WarehouseID                              WarehouseID
	Quantity                                 Quantity
	Status                                   FulfillmentAllocationStatus
	ReasonCode, IncidentID, ReplacesID       string
	Version                                  int64
	CreatedAt, UpdatedAt                     time.Time
}

func (a FulfillmentAllocation) Validate() error {
	if !domain.ValidSortableID(a.ID) || !domain.ValidToken(a.IdempotencyKey) || !domain.ValidSortableID(a.OrderID) || !domain.ValidSortableID(a.OrderItemID) || !a.OfferID.Valid() || !a.WarehouseID.Valid() || a.Quantity.Validate() != nil || !a.Status.Valid() || a.Version < 1 || !isUTC(a.CreatedAt) || !isUTC(a.UpdatedAt) || a.UpdatedAt.Before(a.CreatedAt) {
		return ErrInvalidRecord
	}
	if a.ReasonCode != "" && !validReason(a.ReasonCode) {
		return ErrInvalidRecord
	}
	if a.IncidentID != "" && !domain.ValidToken(a.IncidentID) {
		return ErrInvalidRecord
	}
	if a.ReplacesID != "" && (!domain.ValidSortableID(a.ReplacesID) || a.ReplacesID == a.ID) {
		return ErrInvalidRecord
	}
	return nil
}

type ReserveOrderItem struct {
	AllocationID, OrderItemID, IdempotencyKey string
	WarehouseID                               WarehouseID
}

func (c ReserveOrderItem) Validate() error {
	if !domain.ValidSortableID(c.AllocationID) || !domain.ValidSortableID(c.OrderItemID) || !domain.ValidToken(c.IdempotencyKey) || !c.WarehouseID.Valid() {
		return ErrInvalidRecord
	}
	return nil
}
