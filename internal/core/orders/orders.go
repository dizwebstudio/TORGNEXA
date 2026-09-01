// Package orders defines TORGNEXA's provider-neutral canonical Order aggregate.
package orders

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidRecord = errors.New("orders: invalid record")
	ErrInvalidScope  = errors.New("orders: invalid tenant scope")
	ErrNotFound      = errors.New("orders: order not found")
	ErrConflict      = errors.New("orders: optimistic version conflict")
	ErrInvalidState  = errors.New("orders: invalid lifecycle transition")
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
var unitPattern = regexp.MustCompile(`^[A-Z][A-Z0-9._-]{0,15}$`)
var jurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}(?:-[A-Z0-9]{1,12})?$`)

const MaxDecimalScale = 9

type OrderID string
type OrderItemID string
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

func ParseOrderID(v string) (OrderID, error) {
	if !domain.ValidSortableID(v) {
		return "", ErrInvalidRecord
	}
	return OrderID(v), nil
}
func ParseOrderItemID(v string) (OrderItemID, error) {
	if !domain.ValidSortableID(v) {
		return "", ErrInvalidRecord
	}
	return OrderItemID(v), nil
}
func ParseOfferID(v string) (OfferID, error) {
	if !domain.ValidSortableID(v) {
		return "", ErrInvalidRecord
	}
	return OfferID(v), nil
}
func (v OrderID) String() string     { return string(v) }
func (v OrderID) Valid() bool        { return domain.ValidSortableID(string(v)) }
func (v OrderItemID) String() string { return string(v) }
func (v OrderItemID) Valid() bool    { return domain.ValidSortableID(string(v)) }
func (v OfferID) String() string     { return string(v) }
func (v OfferID) Valid() bool        { return domain.ValidSortableID(string(v)) }

type Currency string

func NewCurrency(v string) (Currency, error) {
	if len(v) != 3 {
		return "", ErrInvalidRecord
	}
	for _, c := range []byte(v) {
		if c < 'A' || c > 'Z' {
			return "", ErrInvalidRecord
		}
	}
	return Currency(v), nil
}
func (c Currency) String() string  { return string(c) }
func (c Currency) Validate() error { _, e := NewCurrency(string(c)); return e }

type Money struct {
	minorUnits int64
	currency   Currency
}

func NewMoney(v int64, c Currency) (Money, error) {
	if v < 0 || c.Validate() != nil {
		return Money{}, ErrInvalidRecord
	}
	return Money{v, c}, nil
}
func (m Money) MinorUnits() int64  { return m.minorUnits }
func (m Money) Currency() Currency { return m.currency }
func (m Money) Validate() error    { _, e := NewMoney(m.minorUnits, m.currency); return e }

type Decimal struct {
	coefficient int64
	scale       uint8
}

func NewDecimal(c int64, s uint8) (Decimal, error) {
	if s > MaxDecimalScale {
		return Decimal{}, ErrInvalidRecord
	}
	return normalizeDecimal(Decimal{c, s}), nil
}
func ParseDecimal(v string) (Decimal, error) {
	if v == "" || strings.TrimSpace(v) != v || strings.HasPrefix(v, "+") {
		return Decimal{}, ErrInvalidRecord
	}
	neg := strings.HasPrefix(v, "-")
	digits := v
	if neg {
		digits = digits[1:]
	}
	parts := strings.Split(digits, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return Decimal{}, ErrInvalidRecord
	}
	for _, p := range parts {
		for _, r := range p {
			if r < '0' || r > '9' {
				return Decimal{}, ErrInvalidRecord
			}
		}
	}
	if len(parts[0]) > 1 && parts[0][0] == '0' {
		return Decimal{}, ErrInvalidRecord
	}
	scale := 0
	all := parts[0]
	if len(parts) == 2 {
		scale = len(parts[1])
		if scale > MaxDecimalScale {
			return Decimal{}, ErrInvalidRecord
		}
		all += parts[1]
	}
	n := new(big.Int)
	if _, ok := n.SetString(all, 10); !ok {
		return Decimal{}, ErrInvalidRecord
	}
	if neg {
		n.Neg(n)
	}
	if !n.IsInt64() {
		return Decimal{}, ErrInvalidRecord
	}
	return NewDecimal(n.Int64(), uint8(scale))
}
func normalizeDecimal(d Decimal) Decimal {
	for d.scale > 0 && d.coefficient%10 == 0 {
		d.coefficient /= 10
		d.scale--
	}
	return d
}
func (d Decimal) Coefficient() int64 { return d.coefficient }
func (d Decimal) Scale() uint8       { return d.scale }
func (d Decimal) Validate() error {
	if d.scale > MaxDecimalScale || normalizeDecimal(d) != d {
		return ErrInvalidRecord
	}
	return nil
}
func (d Decimal) Positive() bool { return d.coefficient > 0 }
func (d Decimal) String() string {
	if d.coefficient == 0 {
		return "0"
	}
	neg := d.coefficient < 0
	var mag uint64
	if neg {
		mag = uint64(-(d.coefficient + 1)) + 1
	} else {
		mag = uint64(d.coefficient)
	}
	s := strconv.FormatUint(mag, 10)
	if d.scale > 0 {
		for len(s) <= int(d.scale) {
			s = "0" + s
		}
		cut := len(s) - int(d.scale)
		s = s[:cut] + "." + s[cut:]
	}
	if neg {
		return "-" + s
	}
	return s
}

type UnitCode string

func NewUnitCode(v string) (UnitCode, error) {
	if !unitPattern.MatchString(v) {
		return "", ErrInvalidRecord
	}
	return UnitCode(v), nil
}
func (u UnitCode) String() string  { return string(u) }
func (u UnitCode) Validate() error { _, e := NewUnitCode(string(u)); return e }

type Quantity struct {
	Value Decimal
	Unit  UnitCode
}

func NewQuantity(v Decimal, u UnitCode) (Quantity, error) {
	if v.Validate() != nil || !v.Positive() || u.Validate() != nil {
		return Quantity{}, ErrInvalidRecord
	}
	return Quantity{v, u}, nil
}
func (q Quantity) Validate() error { _, e := NewQuantity(q.Value, q.Unit); return e }

type Status string

const (
	StatusPending    Status = "pending"
	StatusConfirmed  Status = "confirmed"
	StatusProcessing Status = "processing"
	StatusFulfilled  Status = "fulfilled"
	StatusCancelled  Status = "cancelled"
)

func (s Status) Valid() bool {
	return s == StatusPending || s == StatusConfirmed || s == StatusProcessing || s == StatusFulfilled || s == StatusCancelled
}
func (s Status) Terminal() bool { return s == StatusFulfilled || s == StatusCancelled }
func ValidateTransition(from, to Status) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	ok := (from == StatusPending && (to == StatusConfirmed || to == StatusCancelled)) || (from == StatusConfirmed && (to == StatusProcessing || to == StatusCancelled)) || (from == StatusProcessing && (to == StatusFulfilled || to == StatusCancelled))
	if !ok {
		return ErrInvalidState
	}
	return nil
}

type TaxSnapshot struct {
	Jurisdiction, Category string
	Rate                   Decimal
	PriceIncludesTax       bool
}

func (t TaxSnapshot) Validate() error {
	if !jurisdictionPattern.MatchString(t.Jurisdiction) || (t.Category != "standard" && t.Category != "reduced" && t.Category != "zero") || t.Rate.Validate() != nil || t.Rate.Coefficient() < 0 {
		return ErrInvalidRecord
	}
	one, _ := NewDecimal(1, 0)
	if decimalCompare(t.Rate, one) > 0 {
		return ErrInvalidRecord
	}
	if t.Category == "zero" && t.Rate.Coefficient() != 0 {
		return ErrInvalidRecord
	}
	return nil
}

type OrderItem struct {
	ID            OrderItemID
	OrderID       OrderID
	OfferID       OfferID
	Position      int
	SKU           string
	Quantity      Quantity
	UnitPrice     Money
	Subtotal      Money
	DiscountTotal Money
	TaxTotal      Money
	LineTotal     Money
	Tax           TaxSnapshot
	CreatedAt     time.Time
}

func (i OrderItem) Validate() error {
	if !i.ID.Valid() || !i.OrderID.Valid() || !i.OfferID.Valid() || i.Position < 1 || !codePattern.MatchString(i.SKU) || i.Quantity.Validate() != nil || i.UnitPrice.Validate() != nil || i.Subtotal.Validate() != nil || i.DiscountTotal.Validate() != nil || i.TaxTotal.Validate() != nil || i.LineTotal.Validate() != nil || i.Tax.Validate() != nil || !isUTC(i.CreatedAt) {
		return ErrInvalidRecord
	}
	c := i.UnitPrice.Currency()
	for _, m := range []Money{i.Subtotal, i.DiscountTotal, i.TaxTotal, i.LineTotal} {
		if m.Currency() != c {
			return ErrInvalidRecord
		}
	}
	if i.DiscountTotal.MinorUnits() > i.Subtotal.MinorUnits() {
		return ErrInvalidRecord
	}
	expected, ok := safeSub(i.Subtotal.MinorUnits(), i.DiscountTotal.MinorUnits())
	if !ok {
		return ErrInvalidRecord
	}
	if !i.Tax.PriceIncludesTax {
		expected, ok = safeAdd(expected, i.TaxTotal.MinorUnits())
		if !ok {
			return ErrInvalidRecord
		}
	}
	if i.LineTotal.MinorUnits() != expected {
		return ErrInvalidRecord
	}
	return nil
}

type Order struct {
	ID                                                           OrderID
	OrganizationID, WorkspaceID                                  string
	Number                                                       string
	Status                                                       Status
	Currency                                                     Currency
	Items                                                        []OrderItem
	Subtotal, DiscountTotal, TaxTotal, ShippingTotal, GrandTotal Money
	PlacedAt                                                     time.Time
	Version                                                      int64
	CreatedAt, UpdatedAt                                         time.Time
}

func (o Order) Validate() error {
	if !o.ID.Valid() || !domain.ValidSortableID(o.OrganizationID) || !domain.ValidSortableID(o.WorkspaceID) || !codePattern.MatchString(o.Number) || !o.Status.Valid() || o.Currency.Validate() != nil || len(o.Items) < 1 || len(o.Items) > 1000 || o.Version < 1 || !isUTC(o.PlacedAt) || !isUTC(o.CreatedAt) || !isUTC(o.UpdatedAt) || o.UpdatedAt.Before(o.CreatedAt) {
		return ErrInvalidRecord
	}
	for _, m := range []Money{o.Subtotal, o.DiscountTotal, o.TaxTotal, o.ShippingTotal, o.GrandTotal} {
		if m.Validate() != nil || m.Currency() != o.Currency {
			return ErrInvalidRecord
		}
	}
	var subtotal, discount, tax, lines int64
	positions := map[int]bool{}
	ids := map[OrderItemID]bool{}
	for _, item := range o.Items {
		if item.Validate() != nil || item.OrderID != o.ID || item.UnitPrice.Currency() != o.Currency || positions[item.Position] || ids[item.ID] {
			return ErrInvalidRecord
		}
		positions[item.Position] = true
		ids[item.ID] = true
		var ok bool
		subtotal, ok = safeAdd(subtotal, item.Subtotal.MinorUnits())
		if !ok {
			return ErrInvalidRecord
		}
		discount, ok = safeAdd(discount, item.DiscountTotal.MinorUnits())
		if !ok {
			return ErrInvalidRecord
		}
		tax, ok = safeAdd(tax, item.TaxTotal.MinorUnits())
		if !ok {
			return ErrInvalidRecord
		}
		lines, ok = safeAdd(lines, item.LineTotal.MinorUnits())
		if !ok {
			return ErrInvalidRecord
		}
	}
	grand, ok := safeAdd(lines, o.ShippingTotal.MinorUnits())
	if !ok {
		return ErrInvalidRecord
	}
	if subtotal != o.Subtotal.MinorUnits() || discount != o.DiscountTotal.MinorUnits() || tax != o.TaxTotal.MinorUnits() || grand != o.GrandTotal.MinorUnits() {
		return ErrInvalidRecord
	}
	return nil
}

type CreateItem struct {
	ID                                                      OrderItemID
	OfferID                                                 OfferID
	Position                                                int
	SKU                                                     string
	Quantity                                                Quantity
	UnitPrice, Subtotal, DiscountTotal, TaxTotal, LineTotal Money
	Tax                                                     TaxSnapshot
}

func (i CreateItem) Validate() error {
	dummy := OrderItem{ID: i.ID, OrderID: OrderID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a9999"), OfferID: i.OfferID, Position: i.Position, SKU: i.SKU, Quantity: i.Quantity, UnitPrice: i.UnitPrice, Subtotal: i.Subtotal, DiscountTotal: i.DiscountTotal, TaxTotal: i.TaxTotal, LineTotal: i.LineTotal, Tax: i.Tax, CreatedAt: time.Unix(0, 0).UTC()}
	return dummy.Validate()
}

type CreateOrder struct {
	ID            OrderID
	Number        string
	Currency      Currency
	Items         []CreateItem
	ShippingTotal Money
	PlacedAt      time.Time
}

func (c CreateOrder) Validate() error {
	if !c.ID.Valid() || !codePattern.MatchString(c.Number) || c.Currency.Validate() != nil || len(c.Items) < 1 || len(c.Items) > 1000 || c.ShippingTotal.Validate() != nil || c.ShippingTotal.Currency() != c.Currency || !isUTC(c.PlacedAt) {
		return ErrInvalidRecord
	}
	positions := map[int]bool{}
	ids := map[OrderItemID]bool{}
	for _, i := range c.Items {
		if i.Validate() != nil || positions[i.Position] || ids[i.ID] || i.UnitPrice.Currency() != c.Currency {
			return ErrInvalidRecord
		}
		positions[i.Position] = true
		ids[i.ID] = true
	}
	return nil
}

type ChangeStatus struct {
	ID              OrderID
	ExpectedVersion int64
	Status          Status
}

func (c ChangeStatus) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !c.Status.Valid() {
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
	Order(context.Context, Scope, OrderID) (Order, error)
	Create(context.Context, Scope, CreateOrder, Mutation) (Order, error)
	ChangeStatus(context.Context, Scope, ChangeStatus, Mutation) (Order, error)
}

func BuildCreate(cmd CreateOrder, scope Scope, at time.Time) (Order, error) {
	if !scope.Valid() || cmd.Validate() != nil || !isUTC(at) {
		return Order{}, ErrInvalidRecord
	}
	order := Order{ID: cmd.ID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Number: cmd.Number, Status: StatusPending, Currency: cmd.Currency, ShippingTotal: cmd.ShippingTotal, PlacedAt: cmd.PlacedAt, Version: 1, CreatedAt: at, UpdatedAt: at}
	var sub, disc, tax, lines int64
	for _, ci := range cmd.Items {
		item := OrderItem{ID: ci.ID, OrderID: cmd.ID, OfferID: ci.OfferID, Position: ci.Position, SKU: ci.SKU, Quantity: ci.Quantity, UnitPrice: ci.UnitPrice, Subtotal: ci.Subtotal, DiscountTotal: ci.DiscountTotal, TaxTotal: ci.TaxTotal, LineTotal: ci.LineTotal, Tax: ci.Tax, CreatedAt: at}
		order.Items = append(order.Items, item)
		var ok bool
		sub, ok = safeAdd(sub, ci.Subtotal.MinorUnits())
		if !ok {
			return Order{}, ErrInvalidRecord
		}
		disc, ok = safeAdd(disc, ci.DiscountTotal.MinorUnits())
		if !ok {
			return Order{}, ErrInvalidRecord
		}
		tax, ok = safeAdd(tax, ci.TaxTotal.MinorUnits())
		if !ok {
			return Order{}, ErrInvalidRecord
		}
		lines, ok = safeAdd(lines, ci.LineTotal.MinorUnits())
		if !ok {
			return Order{}, ErrInvalidRecord
		}
	}
	var err error
	if order.Subtotal, err = NewMoney(sub, cmd.Currency); err != nil {
		return Order{}, err
	}
	if order.DiscountTotal, err = NewMoney(disc, cmd.Currency); err != nil {
		return Order{}, err
	}
	if order.TaxTotal, err = NewMoney(tax, cmd.Currency); err != nil {
		return Order{}, err
	}
	grand, ok := safeAdd(lines, cmd.ShippingTotal.MinorUnits())
	if !ok {
		return Order{}, ErrInvalidRecord
	}
	if order.GrandTotal, err = NewMoney(grand, cmd.Currency); err != nil {
		return Order{}, err
	}
	if err = order.Validate(); err != nil {
		return Order{}, err
	}
	return order, nil
}

func decimalCompare(a, b Decimal) int {
	scale := a.scale
	if b.scale > scale {
		scale = b.scale
	}
	aa := new(big.Int).SetInt64(a.coefficient)
	bb := new(big.Int).SetInt64(b.coefficient)
	aa.Mul(aa, pow10(int(scale-a.scale)))
	bb.Mul(bb, pow10(int(scale-b.scale)))
	return aa.Cmp(bb)
}
func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
func safeAdd(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}
func safeSub(a, b int64) (int64, bool) {
	if b == math.MinInt64 {
		return 0, false
	}
	return safeAdd(a, -b)
}
func isUTC(v time.Time) bool { return !v.IsZero() && v.Location() == time.UTC }
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

var _ = fmt.Sprintf
