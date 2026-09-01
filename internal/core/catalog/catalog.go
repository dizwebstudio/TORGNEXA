// Package catalog defines TORGNEXA's provider-neutral Product/Offer catalog core.
package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidRecord    = errors.New("catalog: invalid record")
	ErrInvalidScope     = errors.New("catalog: invalid tenant scope")
	ErrNotFound         = errors.New("catalog: record not found")
	ErrConflict         = errors.New("catalog: optimistic version conflict")
	ErrInvalidState     = errors.New("catalog: invalid lifecycle transition")
	ErrProductHasOffers = errors.New("catalog: product has non-archived offers")
)

var canonicalCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type ProductID string
type OfferID string

// Scope is the mandatory tenant boundary for catalog operations. It is kept in
// Core without importing another Core package so the frozen dependency rule
// remains acyclic; values use the same canonical sortable-ID contract.
type Scope struct {
	organizationID string
	workspaceID    string
}

func ParseScope(organizationID, workspaceID string) (Scope, error) {
	if !domain.ValidTenantScope(organizationID, workspaceID) {
		return Scope{}, ErrInvalidScope
	}
	return Scope{organizationID: organizationID, workspaceID: workspaceID}, nil
}
func (scope Scope) OrganizationID() string { return scope.organizationID }
func (scope Scope) WorkspaceID() string    { return scope.workspaceID }
func (scope Scope) Valid() bool {
	return domain.ValidTenantScope(scope.organizationID, scope.workspaceID)
}

func ParseProductID(value string) (ProductID, error) {
	if !domain.ValidSortableID(value) {
		return "", ErrInvalidRecord
	}
	return ProductID(value), nil
}
func ParseOfferID(value string) (OfferID, error) {
	if !domain.ValidSortableID(value) {
		return "", ErrInvalidRecord
	}
	return OfferID(value), nil
}
func (id ProductID) String() string { return string(id) }
func (id ProductID) Valid() bool    { return domain.ValidSortableID(string(id)) }
func (id OfferID) String() string   { return string(id) }
func (id OfferID) Valid() bool      { return domain.ValidSortableID(string(id)) }

type Status string

const (
	StatusDraft    Status = "draft"
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

func (status Status) Valid() bool {
	return status == StatusDraft || status == StatusActive || status == StatusArchived
}

// Product is the canonical descriptive master. Provider card identifiers and
// provider-specific fields are intentionally absent; those live in connector mappings/projections.
type Product struct {
	ID             ProductID
	OrganizationID string
	WorkspaceID    string
	Code           string
	Title          string
	Description    string
	Status         Status
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Offer is a canonical sellable variation. Price and inventory are separate
// domains (Task 005) and therefore do not appear here.
type Offer struct {
	ID             OfferID
	OrganizationID string
	WorkspaceID    string
	ProductID      ProductID
	SKU            string
	GTIN           string
	Status         Status
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (product Product) Validate() error {
	if !product.ID.Valid() || !domain.ValidSortableID(product.OrganizationID) || !domain.ValidSortableID(product.WorkspaceID) ||
		!validCode(product.Code) || !validTitle(product.Title) || !validDescription(product.Description) ||
		!product.Status.Valid() || !validMetadata(product.Version, product.CreatedAt, product.UpdatedAt) {
		return ErrInvalidRecord
	}
	return nil
}
func (offer Offer) Validate() error {
	if !offer.ID.Valid() || !domain.ValidSortableID(offer.OrganizationID) || !domain.ValidSortableID(offer.WorkspaceID) || !offer.ProductID.Valid() ||
		!validCode(offer.SKU) || !validGTIN(offer.GTIN) || !offer.Status.Valid() ||
		!validMetadata(offer.Version, offer.CreatedAt, offer.UpdatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type CreateProduct struct {
	ID                       ProductID
	Code, Title, Description string
}

func (command CreateProduct) Validate() error {
	if !command.ID.Valid() || !validCode(command.Code) || !validTitle(command.Title) || !validDescription(command.Description) {
		return ErrInvalidRecord
	}
	return nil
}

type UpdateProduct struct {
	ID                 ProductID
	ExpectedVersion    int64
	Title, Description string
}

func (command UpdateProduct) Validate() error {
	if !command.ID.Valid() || command.ExpectedVersion < 1 || !validTitle(command.Title) || !validDescription(command.Description) {
		return ErrInvalidRecord
	}
	return nil
}

type ChangeProductStatus struct {
	ID              ProductID
	ExpectedVersion int64
	Status          Status
}

func (command ChangeProductStatus) Validate() error {
	if !command.ID.Valid() || command.ExpectedVersion < 1 || !command.Status.Valid() {
		return ErrInvalidRecord
	}
	return nil
}

type CreateOffer struct {
	ID        OfferID
	ProductID ProductID
	SKU, GTIN string
}

func (command CreateOffer) Validate() error {
	if !command.ID.Valid() || !command.ProductID.Valid() || !validCode(command.SKU) || !validGTIN(command.GTIN) {
		return ErrInvalidRecord
	}
	return nil
}

type UpdateOffer struct {
	ID              OfferID
	ExpectedVersion int64
	GTIN            string
}

func (command UpdateOffer) Validate() error {
	if !command.ID.Valid() || command.ExpectedVersion < 1 || !validGTIN(command.GTIN) {
		return ErrInvalidRecord
	}
	return nil
}

type ChangeOfferStatus struct {
	ID              OfferID
	ExpectedVersion int64
	Status          Status
}

func (command ChangeOfferStatus) Validate() error {
	if !command.ID.Valid() || command.ExpectedVersion < 1 || !command.Status.Valid() {
		return ErrInvalidRecord
	}
	return nil
}

// Mutation supplies immutable event-envelope metadata. A repository mutation
// succeeds only when the aggregate row and its event intent commit together.
type Mutation struct {
	EventID       string
	OccurredAt    time.Time
	Source        string
	CorrelationID string
	CausationID   string
	ActorID       string
	TraceID       string
}

func (mutation Mutation) Validate() error {
	if !validEventID(mutation.EventID) || !isUTC(mutation.OccurredAt) || !validSource(mutation.Source) ||
		!validOptionalID(mutation.CorrelationID) || !validOptionalID(mutation.CausationID) ||
		!validOptionalID(mutation.ActorID) || !validOptionalID(mutation.TraceID) {
		return ErrInvalidRecord
	}
	return nil
}

// Repository is the canonical tenant-scoped catalog persistence port. Mutation
// methods include their durable event intent in the same transaction.
type Repository interface {
	Product(context.Context, Scope, ProductID) (Product, error)
	Offer(context.Context, Scope, OfferID) (Offer, error)
	OffersByProduct(context.Context, Scope, ProductID, int) ([]Offer, error)
	CreateProduct(context.Context, Scope, CreateProduct, Mutation) (Product, error)
	UpdateProduct(context.Context, Scope, UpdateProduct, Mutation) (Product, error)
	ChangeProductStatus(context.Context, Scope, ChangeProductStatus, Mutation) (Product, error)
	CreateOffer(context.Context, Scope, CreateOffer, Mutation) (Offer, error)
	UpdateOffer(context.Context, Scope, UpdateOffer, Mutation) (Offer, error)
	ChangeOfferStatus(context.Context, Scope, ChangeOfferStatus, Mutation) (Offer, error)
}

func ValidateProductTransition(from, to Status) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	if from == StatusDraft && (to == StatusActive || to == StatusArchived) {
		return nil
	}
	if from == StatusActive && to == StatusArchived {
		return nil
	}
	return ErrInvalidState
}
func ValidateOfferTransition(from, to Status) error { return ValidateProductTransition(from, to) }

func validCode(value string) bool { return canonicalCodePattern.MatchString(value) }
func validTitle(value string) bool {
	return domain.ValidText(value, 1, 300, false)
}
func validDescription(value string) bool {
	if value == "" {
		return true
	}
	return domain.ValidText(value, 1, 20000, true)
}
func validMetadata(version int64, createdAt, updatedAt time.Time) bool {
	return version >= 1 && isUTC(createdAt) && isUTC(updatedAt) && !updatedAt.Before(createdAt)
}
func isUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}
func validEventID(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && canonicalCodePattern.MatchString(value)
}
func validSource(value string) bool {
	if len(value) < 1 || len(value) > 128 || value != strings.ToLower(value) {
		return false
	}
	for i, c := range []byte(value) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '.' || c == '_' || c == '-')) {
			continue
		}
		return false
	}
	return true
}
func validOptionalID(value string) bool {
	return value == "" || (len(value) <= 128 && canonicalCodePattern.MatchString(value))
}

// validGTIN accepts an empty GTIN or a canonical GTIN-8/12/13/14 with a valid
// GS1 modulo-10 check digit. Formatting characters and whitespace are rejected.
func validGTIN(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 8 && len(value) != 12 && len(value) != 13 && len(value) != 14 {
		return false
	}
	for _, c := range []byte(value) {
		if c < '0' || c > '9' {
			return false
		}
	}
	sum := 0
	weight := 3
	for i := len(value) - 2; i >= 0; i-- {
		sum += int(value[i]-'0') * weight
		if weight == 3 {
			weight = 1
		} else {
			weight = 3
		}
	}
	check := (10 - (sum % 10)) % 10
	return int(value[len(value)-1]-'0') == check
}
