// Package search defines TORGNEXA's provider-neutral product and order search port.
// PostgreSQL is the MVP adapter; callers depend only on this contract.
package search

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalid = errors.New("search: invalid value")
)

const (
	MaxQueryRunes = 256
	MaxPageSize   = 100
	MaxCursorSize = 2048
)

type ProductQuery struct {
	Text   string
	Status string
	Limit  int
	Cursor string
}

func (q ProductQuery) Validate() error {
	if !validQueryText(q.Text) || (q.Status != "" && q.Status != "draft" && q.Status != "active" && q.Status != "archived") || q.Limit < 1 || q.Limit > MaxPageSize || !validCursorText(q.Cursor) {
		return ErrInvalid
	}
	return nil
}

type OrderQuery struct {
	Text       string
	Status     string
	PlacedFrom *time.Time
	PlacedTo   *time.Time
	Limit      int
	Cursor     string
}

func (q OrderQuery) Validate() error {
	if !validQueryText(q.Text) || (q.Status != "" && q.Status != "pending" && q.Status != "confirmed" && q.Status != "processing" && q.Status != "fulfilled" && q.Status != "cancelled") || q.Limit < 1 || q.Limit > MaxPageSize || !validCursorText(q.Cursor) {
		return ErrInvalid
	}
	if q.PlacedFrom != nil && !isUTC(*q.PlacedFrom) {
		return ErrInvalid
	}
	if q.PlacedTo != nil && !isUTC(*q.PlacedTo) {
		return ErrInvalid
	}
	if q.PlacedFrom != nil && q.PlacedTo != nil && !q.PlacedTo.After(*q.PlacedFrom) {
		return ErrInvalid
	}
	return nil
}

type ProductHit struct {
	ID          string        `json:"id"`
	Code        string        `json:"code"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Status      string        `json:"status"`
	UpdatedAt   time.Time     `json:"updated_at"`
	ImageURL    string        `json:"image_url,omitempty"`
	Price       *ProductPrice `json:"price,omitempty"`
}

func (h ProductHit) Validate() error {
	if !domain.ValidSortableID(h.ID) || !validCode(h.Code) || !validTitle(h.Title) || !validDescription(h.Description) || (h.Status != "draft" && h.Status != "active" && h.Status != "archived") || !isUTC(h.UpdatedAt) {
		return ErrInvalid
	}
	if h.Price != nil && h.Price.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

// ProductPrice is the representative regular price of the first active offer
// returned in a product search projection. It remains optional because a
// product may not have an active offer or a regular price yet.
type ProductPrice struct {
	MinorUnits int64  `json:"minor_units"`
	Currency   string `json:"currency"`
}

func (p ProductPrice) Validate() error {
	if p.MinorUnits < 0 || !domain.ValidCurrencyCode(p.Currency) {
		return ErrInvalid
	}
	return nil
}

type ProductPage struct {
	Items      []ProductHit `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (p ProductPage) Validate() error {
	if len(p.Items) > MaxPageSize || !validCursorText(p.NextCursor) {
		return ErrInvalid
	}
	for _, item := range p.Items {
		if item.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

type OrderHit struct {
	ID              string    `json:"id"`
	OrderNumber     string    `json:"order_number"`
	Status          string    `json:"status"`
	Currency        string    `json:"currency"`
	GrandMinorUnits int64     `json:"grand_minor_units"`
	PlacedAt        time.Time `json:"placed_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ProductTitle    string    `json:"product_title,omitempty"`
	ProductSKU      string    `json:"product_sku,omitempty"`
	ProductImageURL string    `json:"product_image_url,omitempty"`
}

func (h OrderHit) Validate() error {
	if !domain.ValidSortableID(h.ID) || !validCode(h.OrderNumber) || (h.Status != "pending" && h.Status != "confirmed" && h.Status != "processing" && h.Status != "fulfilled" && h.Status != "cancelled") || !domain.ValidCurrencyCode(h.Currency) || h.GrandMinorUnits < 0 || !isUTC(h.PlacedAt) || !isUTC(h.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type OrderPage struct {
	Items      []OrderHit `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func (p OrderPage) Validate() error {
	if len(p.Items) > MaxPageSize || !validCursorText(p.NextCursor) {
		return ErrInvalid
	}
	for _, item := range p.Items {
		if item.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// Provider is the only search dependency exposed to application/domain callers.
// Implementations must enforce the authenticated tenant/workspace scope in addition
// to any database-level row security.
type Provider interface {
	SearchProducts(context.Context, tenancy.Scope, ProductQuery) (ProductPage, error)
	SearchOrders(context.Context, tenancy.Scope, OrderQuery) (OrderPage, error)
}

type Cursor struct {
	Priority    int
	UpdatedAt   time.Time
	ID          string
	Fingerprint string
}

type encodedCursor struct {
	Version     int    `json:"v"`
	Priority    int    `json:"p"`
	UpdatedAt   string `json:"u"`
	ID          string `json:"i"`
	Fingerprint string `json:"f"`
}

func NewCursor(priority int, updatedAt time.Time, id, fingerprint string) (string, error) {
	if priority < 0 || priority > 2 || !isUTC(updatedAt) || !domain.ValidSortableID(id) || !validFingerprint(fingerprint) {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(encodedCursor{Version: 1, Priority: priority, UpdatedAt: updatedAt.Format(time.RFC3339Nano), ID: id, Fingerprint: fingerprint})
	if err != nil {
		return "", ErrInvalid
	}
	out := "v1." + base64.RawURLEncoding.EncodeToString(raw)
	if len(out) > MaxCursorSize {
		return "", ErrInvalid
	}
	return out, nil
}

func ParseCursor(raw, fingerprint string) (Cursor, error) {
	if raw == "" {
		return Cursor{}, nil
	}
	if !validCursorText(raw) || !strings.HasPrefix(raw, "v1.") || !validFingerprint(fingerprint) {
		return Cursor{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, "v1."))
	if err != nil || len(payload) > 1024 {
		return Cursor{}, ErrInvalid
	}
	var in encodedCursor
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.Version != 1 || in.Priority < 0 || in.Priority > 2 || !domain.ValidSortableID(in.ID) || in.Fingerprint != fingerprint {
		return Cursor{}, ErrInvalid
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Cursor{}, ErrInvalid
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, in.UpdatedAt)
	if err != nil || updatedAt.Location() != time.UTC {
		return Cursor{}, ErrInvalid
	}
	return Cursor{Priority: in.Priority, UpdatedAt: updatedAt, ID: in.ID, Fingerprint: in.Fingerprint}, nil
}

func ProductFingerprint(q ProductQuery) string {
	return fingerprint("product", normalize(q.Text), q.Status)
}

func OrderFingerprint(q OrderQuery) string {
	from, to := "", ""
	if q.PlacedFrom != nil {
		from = q.PlacedFrom.Format(time.RFC3339Nano)
	}
	if q.PlacedTo != nil {
		to = q.PlacedTo.Format(time.RFC3339Nano)
	}
	return fingerprint("order", normalize(q.Text), q.Status, from, to)
}

func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func normalize(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
func validFingerprint(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}
func validQueryText(v string) bool {
	if v == "" {
		return true
	}
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) > MaxQueryRunes {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validCursorText(v string) bool {
	if v == "" {
		return true
	}
	if len(v) <= len("v1.") || len(v) > MaxCursorSize || !utf8.ValidString(v) || v != strings.TrimSpace(v) || !strings.HasPrefix(v, "v1.") {
		return false
	}
	for _, r := range v {
		if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
func validCode(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for i, r := range v {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (i > 0 && (r == '.' || r == '_' || r == ':' || r == '/' || r == '-')) {
			continue
		}
		return false
	}
	return true
}
func validTitle(v string) bool {
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 300 {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validDescription(v string) bool {
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) > 20000 {
		return false
	}
	for _, r := range v {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' || r == 0x7f {
			return false
		}
	}
	return true
}
func isUTC(v time.Time) bool { return !v.IsZero() && v.Location() == time.UTC }
