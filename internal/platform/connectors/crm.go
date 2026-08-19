package connectors

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidCRMRead  = errors.New("connectors: invalid crm read projection")
	ErrInvalidCRMWrite = errors.New("connectors: invalid crm write")
)

type CRMEntityKind string

const (
	CRMLead    CRMEntityKind = "lead"
	CRMDeal    CRMEntityKind = "deal"
	CRMContact CRMEntityKind = "contact"
	CRMCompany CRMEntityKind = "company"
)

func (kind CRMEntityKind) Valid() bool {
	switch kind {
	case CRMLead, CRMDeal, CRMContact, CRMCompany:
		return true
	default:
		return false
	}
}

type CRMEntity struct {
	RemoteID         string        `json:"remote_id"`
	Kind             CRMEntityKind `json:"kind"`
	ExternalID       string        `json:"external_id,omitempty"`
	Title            string        `json:"title,omitempty"`
	FirstName        string        `json:"first_name,omitempty"`
	MiddleName       string        `json:"middle_name,omitempty"`
	LastName         string        `json:"last_name,omitempty"`
	StageRemoteID    string        `json:"stage_remote_id,omitempty"`
	PipelineRemoteID string        `json:"pipeline_remote_id,omitempty"`
	CompanyRemoteID  string        `json:"company_remote_id,omitempty"`
	ContactRemoteIDs []string      `json:"contact_remote_ids,omitempty"`
	Opportunity      string        `json:"opportunity,omitempty"`
	Currency         string        `json:"currency,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

func (item CRMEntity) Validate() error {
	if !validRemoteReadID(item.RemoteID) || !item.Kind.Valid() || !validOptionalReadText(item.ExternalID, 300) ||
		!validOptionalReadText(item.Title, 500) || !validOptionalReadText(item.FirstName, 300) ||
		!validOptionalReadText(item.MiddleName, 300) || !validOptionalReadText(item.LastName, 300) ||
		!validOptionalRemoteReadID(item.StageRemoteID) || !validOptionalRemoteReadID(item.PipelineRemoteID) ||
		!validOptionalRemoteReadID(item.CompanyRemoteID) || item.CreatedAt.IsZero() || item.CreatedAt.Location() != time.UTC ||
		item.UpdatedAt.IsZero() || item.UpdatedAt.Location() != time.UTC || item.UpdatedAt.Before(item.CreatedAt) || len(item.ContactRemoteIDs) > 1000 {
		return ErrInvalidCRMRead
	}
	if item.Opportunity != "" && !validExactDecimal(item.Opportunity) {
		return ErrInvalidCRMRead
	}
	if item.Currency != "" && !validCurrency(item.Currency) {
		return ErrInvalidCRMRead
	}
	if item.Kind == CRMContact && item.FirstName == "" && item.LastName == "" {
		return ErrInvalidCRMRead
	}
	if item.Kind != CRMContact && item.Title == "" {
		return ErrInvalidCRMRead
	}
	seen := map[string]struct{}{}
	for _, id := range item.ContactRemoteIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidCRMRead
		}
		if _, ok := seen[id]; ok {
			return ErrInvalidCRMRead
		}
		seen[id] = struct{}{}
	}
	return nil
}

type CRMEntityQuery struct {
	Kind CRMEntityKind `json:"kind"`
	Page PageRequest   `json:"page"`
}

func (q CRMEntityQuery) Validate(max int) error {
	if !q.Kind.Valid() || q.Page.Validate(max) != nil {
		return ErrInvalidReadRequest
	}
	return nil
}

type CRMEntityPage struct {
	Items      []CRMEntity `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

func (page CRMEntityPage) Validate(max int) error {
	if max < 1 || len(page.Items) > max || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidCRMRead
	}
	seen := map[string]struct{}{}
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidCRMRead
		}
		key := string(item.Kind) + "\x00" + item.RemoteID
		if _, ok := seen[key]; ok {
			return ErrInvalidCRMRead
		}
		seen[key] = struct{}{}
	}
	return nil
}

type CRMEntityReader interface {
	ReadCRMEntities(context.Context, Account, Runtime, CRMEntityQuery) (CRMEntityPage, error)
}

type CRMEntityWriteRequest struct {
	RemoteID         string        `json:"remote_id,omitempty"`
	Kind             CRMEntityKind `json:"kind"`
	ExternalID       string        `json:"external_id"`
	Title            string        `json:"title,omitempty"`
	FirstName        string        `json:"first_name,omitempty"`
	MiddleName       string        `json:"middle_name,omitempty"`
	LastName         string        `json:"last_name,omitempty"`
	StageRemoteID    string        `json:"stage_remote_id,omitempty"`
	PipelineRemoteID string        `json:"pipeline_remote_id,omitempty"`
	CompanyRemoteID  string        `json:"company_remote_id,omitempty"`
	ContactRemoteIDs []string      `json:"contact_remote_ids,omitempty"`
	Opportunity      string        `json:"opportunity,omitempty"`
	Currency         string        `json:"currency,omitempty"`
	IdempotencyKey   string        `json:"idempotency_key"`
}

func (r CRMEntityWriteRequest) Validate() error {
	if r.RemoteID != "" && !validRemoteID(r.RemoteID) {
		return ErrInvalidCRMWrite
	}
	if !r.Kind.Valid() || !validReadText(r.ExternalID, 300) || !validIdempotencyKey(r.IdempotencyKey) ||
		!validOptionalWriteText(r.Title, 500) || !validOptionalWriteText(r.FirstName, 300) ||
		!validOptionalWriteText(r.MiddleName, 300) || !validOptionalWriteText(r.LastName, 300) ||
		!validOptionalRemoteReadID(r.StageRemoteID) || !validOptionalRemoteReadID(r.PipelineRemoteID) ||
		!validOptionalRemoteReadID(r.CompanyRemoteID) || len(r.ContactRemoteIDs) > 1000 {
		return ErrInvalidCRMWrite
	}
	if r.Kind == CRMContact && r.FirstName == "" && r.LastName == "" {
		return ErrInvalidCRMWrite
	}
	if r.Kind != CRMContact && r.Title == "" {
		return ErrInvalidCRMWrite
	}
	if r.Opportunity != "" && !validExactDecimal(r.Opportunity) {
		return ErrInvalidCRMWrite
	}
	if r.Currency != "" && !validCurrency(r.Currency) {
		return ErrInvalidCRMWrite
	}
	for _, id := range r.ContactRemoteIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidCRMWrite
		}
	}
	return nil
}

type CRMWriteReceipt struct {
	RemoteID   string `json:"remote_id"`
	Applied    bool   `json:"applied"`
	Duplicate  bool   `json:"duplicate"`
	Reconciled bool   `json:"reconciled"`
}

func (r CRMWriteReceipt) Validate() error {
	if !validRemoteID(r.RemoteID) || r.Applied == r.Duplicate || (r.Reconciled && !r.Applied && !r.Duplicate) {
		return ErrInvalidCRMWrite
	}
	return nil
}

type CRMEntityWriter interface {
	UpsertCRMEntity(context.Context, Account, Runtime, CRMEntityWriteRequest) (CRMWriteReceipt, error)
}

type CRMProductRow struct {
	RemoteID        string        `json:"remote_id"`
	OwnerKind       CRMEntityKind `json:"owner_kind"`
	OwnerRemoteID   string        `json:"owner_remote_id"`
	ProductRemoteID string        `json:"product_remote_id,omitempty"`
	Name            string        `json:"name"`
	Price           string        `json:"price"`
	Quantity        string        `json:"quantity"`
	TaxRate         string        `json:"tax_rate,omitempty"`
	TaxIncluded     bool          `json:"tax_included"`
}

func (r CRMProductRow) Validate() error {
	if !validRemoteReadID(r.RemoteID) || (r.OwnerKind != CRMLead && r.OwnerKind != CRMDeal) || !validRemoteReadID(r.OwnerRemoteID) ||
		!validOptionalRemoteReadID(r.ProductRemoteID) || !validReadText(r.Name, 500) || !validExactDecimal(r.Price) || !validExactDecimal(r.Quantity) ||
		(r.TaxRate != "" && !validExactDecimal(r.TaxRate)) {
		return ErrInvalidCRMRead
	}
	return nil
}

type CRMProductRowQuery struct {
	OwnerKind     CRMEntityKind `json:"owner_kind"`
	OwnerRemoteID string        `json:"owner_remote_id"`
	Page          PageRequest   `json:"page"`
}

func (q CRMProductRowQuery) Validate(max int) error {
	if (q.OwnerKind != CRMLead && q.OwnerKind != CRMDeal) || !validRemoteReadID(q.OwnerRemoteID) || q.Page.Validate(max) != nil {
		return ErrInvalidReadRequest
	}
	return nil
}

type CRMProductRowPage struct {
	Items      []CRMProductRow `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func (p CRMProductRowPage) Validate(max int) error {
	if max < 1 || len(p.Items) > max || len(p.NextCursor) > 4096 || !utf8.ValidString(p.NextCursor) {
		return ErrInvalidCRMRead
	}
	seen := map[string]struct{}{}
	for _, item := range p.Items {
		if item.Validate() != nil {
			return ErrInvalidCRMRead
		}
		if _, ok := seen[item.RemoteID]; ok {
			return ErrInvalidCRMRead
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

type CRMProductRowReader interface {
	ReadCRMProductRows(context.Context, Account, Runtime, CRMProductRowQuery) (CRMProductRowPage, error)
}

type CRMProductRowWrite struct {
	ProductRemoteID string `json:"product_remote_id,omitempty"`
	Name            string `json:"name"`
	Price           string `json:"price"`
	Quantity        string `json:"quantity"`
	TaxRate         string `json:"tax_rate,omitempty"`
	TaxIncluded     bool   `json:"tax_included"`
}

func (r CRMProductRowWrite) Validate() error {
	if !validOptionalRemoteReadID(r.ProductRemoteID) || !validReadText(r.Name, 500) || !validExactDecimal(r.Price) || !validExactDecimal(r.Quantity) || (r.TaxRate != "" && !validExactDecimal(r.TaxRate)) {
		return ErrInvalidCRMWrite
	}
	return nil
}

type CRMProductRowsWriteRequest struct {
	OwnerKind      CRMEntityKind        `json:"owner_kind"`
	OwnerRemoteID  string               `json:"owner_remote_id"`
	Rows           []CRMProductRowWrite `json:"rows"`
	IdempotencyKey string               `json:"idempotency_key"`
}

func (r CRMProductRowsWriteRequest) Validate() error {
	if (r.OwnerKind != CRMLead && r.OwnerKind != CRMDeal) || !validRemoteReadID(r.OwnerRemoteID) || len(r.Rows) > 1000 || !validIdempotencyKey(r.IdempotencyKey) {
		return ErrInvalidCRMWrite
	}
	for _, row := range r.Rows {
		if row.Validate() != nil {
			return ErrInvalidCRMWrite
		}
	}
	return nil
}

type CRMProductRowWriter interface {
	ReplaceCRMProductRows(context.Context, Account, Runtime, CRMProductRowsWriteRequest) (CRMWriteReceipt, error)
}

func parseNumericRemoteID(value string) (int64, error) {
	if !validRemoteID(value) {
		return 0, ErrInvalidCRMWrite
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 {
		return 0, ErrInvalidCRMWrite
	}
	return n, nil
}

func canonicalCRMText(value string) string { return strings.TrimSpace(value) }
