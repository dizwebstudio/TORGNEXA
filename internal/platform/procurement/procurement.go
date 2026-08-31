// Package procurement implements suppliers, offers and purchase-order lifecycle.
package procurement

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"regexp"
	"strings"
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
	ID        string          `json:"id"`
	OfferID   string          `json:"offer_id"`
	SKU       string          `json:"sku"`
	Quantity  domain.Quantity `json:"quantity"`
	UnitPrice domain.Money    `json:"unit_price"`
}
type PurchaseOrder struct {
	ID         string          `json:"id"`
	SupplierID string          `json:"supplier_id"`
	Status     POStatus        `json:"status"`
	Lines      []Line          `json:"lines"`
	Currency   domain.Currency `json:"currency"`
	Version    int64           `json:"version"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
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

// Mutation is the common idempotent write context used by procurement
// repositories. Its identifiers are stored in audit/outbox records and never
// contain credentials or supplier payloads.
type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

// Validate checks the bounded metadata required for an auditable mutation.
func (m Mutation) Validate() error {
	if !safeReference(m.EventID) || !safeReference(m.AuditID) || !safeReference(m.ActorID) || !safeReference(m.Source) || !safeReference(m.CorrelationID) || (m.CausationID != "" && !safeReference(m.CausationID)) || (m.TraceID != "" && !safeReference(m.TraceID)) || m.OccurredAt.IsZero() || m.OccurredAt.Location() != time.UTC {
		return ErrInvalid
	}
	return nil
}

// SupplierStatus is the operator lifecycle for a supplier relationship.
type SupplierStatus string

const (
	SupplierActive   SupplierStatus = "active"
	SupplierBlocked  SupplierStatus = "blocked"
	SupplierArchived SupplierStatus = "archived"
)

// Valid reports whether the supplier status is supported.
func (s SupplierStatus) Valid() bool {
	return s == SupplierActive || s == SupplierBlocked || s == SupplierArchived
}

// SupplierRecord is the application-facing supplier profile. Legal data stays
// in the canonical LegalParty record; LegalPartyID is the only legal identity
// stored here.
type SupplierRecord struct {
	ID                   string             `json:"id"`
	LegalPartyID         string             `json:"legal_party_id"`
	Name                 string             `json:"name"`
	PaymentTerms         string             `json:"payment_terms"`
	Status               SupplierStatus     `json:"status"`
	Currency             domain.Currency    `json:"currency"`
	LeadTimeDays         int                `json:"lead_time_days"`
	MinimumOrderMinor    int64              `json:"minimum_order_minor"`
	MinimumOrderCurrency domain.Currency    `json:"minimum_order_currency"`
	Contacts             []SupplierContact  `json:"contacts"`
	Contracts            []SupplierContract `json:"contracts"`
	Version              int64              `json:"version"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
}

// SupplierContact is a bounded operational contact, not a credentials store.
type SupplierContact struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
	Role  string `json:"role,omitempty"`
}

// SupplierContract links procurement terms to a canonical contract reference.
type SupplierContract struct {
	ID           string    `json:"id,omitempty"`
	ContractID   string    `json:"contract_id,omitempty"`
	Number       string    `json:"number,omitempty"`
	PaymentTerms string    `json:"payment_terms,omitempty"`
	ValidFrom    time.Time `json:"valid_from,omitempty"`
	ValidUntil   time.Time `json:"valid_until,omitempty"`
}

// SupplierOfferRecord is a versioned supplier quote for one canonical offer.
// Price changes create a new history row; they never rewrite old evidence.
type SupplierOfferRecord struct {
	ID                   string          `json:"id"`
	SupplierID           string          `json:"supplier_id"`
	CanonicalOfferID     string          `json:"canonical_offer_id"`
	SupplierSKU          string          `json:"supplier_sku"`
	GTIN                 string          `json:"gtin"`
	SKU                  string          `json:"sku"`
	Unit                 string          `json:"unit"`
	UnitPriceMinor       int64           `json:"unit_price_minor"`
	MinimumOrderMinor    int64           `json:"minimum_order_minor"`
	Currency             domain.Currency `json:"currency"`
	MinimumOrderCurrency domain.Currency `json:"minimum_order_currency"`
	MOQ                  domain.Quantity `json:"moq"`
	CasePack             domain.Quantity `json:"case_pack"`
	LeadTimeDays         int             `json:"lead_time_days"`
	Priority             int             `json:"priority"`
	ValidFrom            time.Time       `json:"valid_from"`
	ValidUntil           time.Time       `json:"valid_until"`
	Version              int64           `json:"version"`
}

// PriceListRow is the normalized, non-secret result of parsing one released
// price-list row.
type PriceListRow struct {
	Row                  int    `json:"row"`
	LeadTimeDays         int    `json:"lead_time_days"`
	Priority             int    `json:"priority"`
	UnitPriceMinor       int64  `json:"unit_price_minor"`
	MinimumOrderMinor    int64  `json:"minimum_order_minor"`
	MOQ                  string `json:"moq"`
	CasePack             string `json:"case_pack"`
	SupplierSKU          string `json:"supplier_sku"`
	GTIN                 string `json:"gtin"`
	SKU                  string `json:"sku"`
	Unit                 string `json:"unit"`
	Currency             string `json:"currency"`
	MinimumOrderCurrency string `json:"minimum_order_currency"`
	CanonicalOfferID     string `json:"canonical_offer_id"`
	MatchMethod          string `json:"match_method"`
}

// PriceListPreview is persisted before an import can mutate supplier offers.
type PriceListPreview struct {
	ID                 string         `json:"id"`
	SupplierID         string         `json:"supplier_id"`
	UploadID           string         `json:"upload_id"`
	SourceSHA256       string         `json:"source_sha256"`
	MappingFingerprint string         `json:"mapping_fingerprint"`
	Status             string         `json:"status"`
	TotalRows          int            `json:"total_rows"`
	ValidRows          int            `json:"valid_rows"`
	InvalidRows        int            `json:"invalid_rows"`
	UnresolvedRows     int            `json:"unresolved_rows"`
	Errors             []ImportError  `json:"errors"`
	Rows               []PriceListRow `json:"rows"`
	Version            int64          `json:"version"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// ImportError identifies a row that needs operator attention.
type ImportError struct {
	Row    int    `json:"row"`
	Field  string `json:"field,omitempty"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// PurchaseOrderRecord is the existing PurchaseOrder lifecycle enriched with
// warehouse, recommendation snapshot and delivery metadata.
type PurchaseOrderRecord struct {
	PurchaseOrder
	OrganizationID       string     `json:"organization_id"`
	WorkspaceID          string     `json:"workspace_id"`
	WarehouseID          string     `json:"warehouse_id"`
	RecommendationID     string     `json:"recommendation_id,omitempty"`
	RecommendationDigest string     `json:"recommendation_digest,omitempty"`
	IdempotencyKey       string     `json:"idempotency_key,omitempty"`
	ApprovalRequestID    string     `json:"approval_request_id,omitempty"`
	SendState            string     `json:"send_state"`
	ErrorCode            string     `json:"error_code,omitempty"`
	ExpectedReceiptAt    *time.Time `json:"expected_receipt_at,omitempty"`
	CreatorID            string     `json:"creator_id,omitempty"`
}

// ReceivingRecord records a partial or complete receipt without changing stock
// directly. WMS consumes this fact and writes the inventory ledger.
type ReceivingRecord struct {
	ID              string          `json:"id"`
	PurchaseOrderID string          `json:"purchase_order_id"`
	WarehouseID     string          `json:"warehouse_id"`
	LineID          string          `json:"line_id"`
	Status          string          `json:"status"`
	DiscrepancyCode string          `json:"discrepancy_code,omitempty"`
	Note            string          `json:"note,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	Quantity        domain.Quantity `json:"quantity"`
	OccurredAt      time.Time       `json:"occurred_at"`
}

// ReconciliationFinding is a redacted procurement drift projection.
type ReconciliationFinding struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	PurchaseOrderID string    `json:"purchase_order_id,omitempty"`
	SupplierOfferID string    `json:"supplier_offer_id,omitempty"`
	Expected        string    `json:"expected"`
	Observed        string    `json:"observed"`
	Status          string    `json:"status"`
	DetectedAt      time.Time `json:"detected_at"`
}

var safeReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

func safeReference(value string) bool {
	return safeReferencePattern.MatchString(strings.TrimSpace(value))
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
