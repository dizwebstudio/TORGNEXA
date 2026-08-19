// Package procurement implements suppliers, offers and purchase-order lifecycle.
package procurement

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"time"
)

var (
	ErrInvalid      = errors.New("procurement: invalid value")
	ErrInvalidState = errors.New("procurement: invalid state transition")
)

type Supplier struct {
	ID, LegalPartyID, Name string
	Active                 bool
	Version                int64
}

func (s Supplier) Validate() error {
	if s.ID == "" || s.LegalPartyID == "" || s.Name == "" || s.Version < 1 {
		return ErrInvalid
	}
	return nil
}

type SupplierOffer struct {
	ID, SupplierID, SKU string
	UnitPrice           domain.Money
	MinQuantity         domain.Quantity
	LeadTimeDays        int
	ValidUntil          time.Time
	Version             int64
}
type POStatus string

const (
	PODraft             POStatus = "draft"
	POApproved          POStatus = "approved"
	POSent              POStatus = "sent"
	POPartiallyReceived POStatus = "partially_received"
	POReceived          POStatus = "received"
	POCancelled         POStatus = "cancelled"
)

func (s POStatus) Valid() bool {
	return s == PODraft || s == POApproved || s == POSent || s == POPartiallyReceived || s == POReceived || s == POCancelled
}

type Line struct {
	ID, OfferID, SKU string
	Quantity         domain.Quantity
	UnitPrice        domain.Money
}
type PurchaseOrder struct {
	ID, SupplierID       string
	Status               POStatus
	Lines                []Line
	Currency             domain.Currency
	Version              int64
	CreatedAt, UpdatedAt time.Time
}

func (po PurchaseOrder) Validate() error {
	if po.ID == "" || po.SupplierID == "" || !po.Status.Valid() || po.Version < 1 || po.Currency.Validate() != nil || len(po.Lines) == 0 || po.CreatedAt.IsZero() || po.UpdatedAt.Before(po.CreatedAt) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, l := range po.Lines {
		if l.ID == "" || l.OfferID == "" || l.SKU == "" || l.Quantity.Validate() != nil || l.UnitPrice.Validate() != nil || l.UnitPrice.MinorUnits() < 0 || l.UnitPrice.Currency() != po.Currency || seen[l.ID] {
			return ErrInvalid
		}
		seen[l.ID] = true
	}
	return nil
}
func CanTransition(from, to POStatus) bool {
	switch from {
	case PODraft:
		return to == POApproved || to == POCancelled
	case POApproved:
		return to == POSent || to == POCancelled
	case POSent:
		return to == POPartiallyReceived || to == POReceived || to == POCancelled
	case POPartiallyReceived:
		return to == POReceived || to == POCancelled
	}
	return false
}
func Transition(po PurchaseOrder, to POStatus, now time.Time) (PurchaseOrder, error) {
	if po.Validate() != nil || !CanTransition(po.Status, to) || now.IsZero() {
		return PurchaseOrder{}, ErrInvalidState
	}
	po.Status = to
	po.Version++
	po.UpdatedAt = now.UTC()
	return po, nil
}

type AuditRecord struct {
	POID, Action string
	Version      int64
	At           time.Time
}
type ImportLine struct {
	ID             string `json:"id"`
	OfferID        string `json:"offer_id"`
	SKU            string `json:"sku"`
	Quantity       string `json:"quantity"`
	Unit           string `json:"unit"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}
type ImportPO struct {
	ID         string       `json:"id"`
	SupplierID string       `json:"supplier_id"`
	Currency   string       `json:"currency"`
	Lines      []ImportLine `json:"lines"`
}

func ParseImport(data []byte, now time.Time) (PurchaseOrder, error) {
	var in ImportPO
	if err := json.Unmarshal(data, &in); err != nil {
		return PurchaseOrder{}, err
	}
	c, err := domain.NewCurrency(in.Currency)
	if err != nil {
		return PurchaseOrder{}, ErrInvalid
	}
	po := PurchaseOrder{ID: in.ID, SupplierID: in.SupplierID, Status: PODraft, Currency: c, Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	for _, x := range in.Lines {
		d, e := domain.ParseDecimal(x.Quantity)
		if e != nil {
			return PurchaseOrder{}, ErrInvalid
		}
		u, e := domain.NewUnitCode(x.Unit)
		if e != nil {
			return PurchaseOrder{}, ErrInvalid
		}
		q, e := domain.NewQuantity(d, u)
		if e != nil {
			return PurchaseOrder{}, ErrInvalid
		}
		m, e := domain.NewMoney(x.UnitPriceMinor, c)
		if e != nil || m.MinorUnits() < 0 {
			return PurchaseOrder{}, ErrInvalid
		}
		po.Lines = append(po.Lines, Line{x.ID, x.OfferID, x.SKU, q, m})
	}
	if po.Validate() != nil {
		return PurchaseOrder{}, ErrInvalid
	}
	return po, nil
}

type Service struct {
	Audit func(tenancy.Scope, AuditRecord) error
}

func (s Service) ChangeStatus(scope tenancy.Scope, po PurchaseOrder, to POStatus, now time.Time) (PurchaseOrder, error) {
	if !scope.Valid() {
		return PurchaseOrder{}, ErrInvalid
	}
	next, err := Transition(po, to, now)
	if err != nil {
		return PurchaseOrder{}, err
	}
	if s.Audit != nil {
		if err := s.Audit(scope, AuditRecord{po.ID, fmt.Sprintf("status.%s", to), next.Version, now.UTC()}); err != nil {
			return PurchaseOrder{}, err
		}
	}
	return next, nil
}
