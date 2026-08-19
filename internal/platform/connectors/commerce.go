package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidCommerceRead = errors.New("connectors: invalid commerce read projection")

var unsignedMoneyPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,9})?$`)

type RemotePrice struct {
	VariantRemoteID string    `json:"variant_remote_id"`
	Value           string    `json:"value"`
	CompareAt       string    `json:"compare_at,omitempty"`
	Currency        string    `json:"currency"`
	VATRemoteID     string    `json:"vat_remote_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (item RemotePrice) Validate() error {
	if !validRemoteReadID(item.VariantRemoteID) || !validUnsignedMoney(item.Value) ||
		(item.CompareAt != "" && !validUnsignedMoney(item.CompareAt)) || !validCurrency(item.Currency) ||
		!validOptionalRemoteReadID(item.VATRemoteID) || item.UpdatedAt.IsZero() || item.UpdatedAt.Location() != time.UTC {
		return ErrInvalidCommerceRead
	}
	return nil
}

type PricePage struct {
	Items      []RemotePrice `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (page PricePage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidCommerceRead
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidCommerceRead
		}
		if _, duplicate := seen[item.VariantRemoteID]; duplicate {
			return ErrInvalidCommerceRead
		}
		seen[item.VariantRemoteID] = struct{}{}
	}
	return nil
}

type PriceReader interface {
	ReadPrices(context.Context, Account, Runtime, PageRequest) (PricePage, error)
}

type RemoteOrderItem struct {
	RemoteID        string `json:"remote_id"`
	VariantRemoteID string `json:"variant_remote_id"`
	Quantity        int64  `json:"quantity"`
}

func (item RemoteOrderItem) Validate() error {
	if !validRemoteReadID(item.RemoteID) || !validRemoteReadID(item.VariantRemoteID) || item.Quantity < 1 {
		return ErrInvalidCommerceRead
	}
	return nil
}

type RemoteOrder struct {
	RemoteID          string            `json:"remote_id"`
	ExternalID        string            `json:"external_id,omitempty"`
	CampaignRemoteID  string            `json:"campaign_remote_id,omitempty"`
	ProgramRemoteID   string            `json:"program_remote_id,omitempty"`
	StatusRemoteID    string            `json:"status_remote_id"`
	SubstatusRemoteID string            `json:"substatus_remote_id,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	Items             []RemoteOrderItem `json:"items"`
}

func (order RemoteOrder) Validate() error {
	if !validRemoteReadID(order.RemoteID) || !validOptionalReadText(order.ExternalID, 300) ||
		!validOptionalRemoteReadID(order.CampaignRemoteID) || !validOptionalRemoteReadID(order.ProgramRemoteID) ||
		!validRemoteReadID(order.StatusRemoteID) || !validOptionalRemoteReadID(order.SubstatusRemoteID) ||
		order.CreatedAt.IsZero() || order.CreatedAt.Location() != time.UTC || order.UpdatedAt.IsZero() || order.UpdatedAt.Location() != time.UTC ||
		order.UpdatedAt.Before(order.CreatedAt) || len(order.Items) > 1000 {
		return ErrInvalidCommerceRead
	}
	seen := make(map[string]struct{}, len(order.Items))
	for _, item := range order.Items {
		if item.Validate() != nil {
			return ErrInvalidCommerceRead
		}
		if _, duplicate := seen[item.RemoteID]; duplicate {
			return ErrInvalidCommerceRead
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

type OrderPage struct {
	Items      []RemoteOrder `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (page OrderPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidCommerceRead
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidCommerceRead
		}
		if _, duplicate := seen[item.RemoteID]; duplicate {
			return ErrInvalidCommerceRead
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

type OrderReader interface {
	ReadOrders(context.Context, Account, Runtime, PageRequest) (OrderPage, error)
}

type MarketplaceNotification struct {
	Type             string    `json:"type"`
	BusinessRemoteID string    `json:"business_remote_id,omitempty"`
	CampaignRemoteID string    `json:"campaign_remote_id,omitempty"`
	ResourceKind     string    `json:"resource_kind"`
	ResourceRemoteID string    `json:"resource_remote_id,omitempty"`
	OccurredAt       time.Time `json:"occurred_at"`
	DedupKey         string    `json:"dedup_key"`
}

func (item MarketplaceNotification) Validate() error {
	if !validNotificationType(item.Type) || !validOptionalRemoteReadID(item.BusinessRemoteID) ||
		!validOptionalRemoteReadID(item.CampaignRemoteID) || !validNotificationKind(item.ResourceKind) ||
		!validOptionalRemoteReadID(item.ResourceRemoteID) || item.OccurredAt.IsZero() || item.OccurredAt.Location() != time.UTC ||
		len(item.DedupKey) != 64 {
		return ErrInvalidCommerceRead
	}
	for _, r := range item.DedupKey {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ErrInvalidCommerceRead
		}
	}
	if item.Type != "PING" && item.ResourceRemoteID == "" {
		return ErrInvalidCommerceRead
	}
	return nil
}

type MarketplaceNotificationDecoder interface {
	DecodeMarketplaceNotification(context.Context, Account, []byte) (MarketplaceNotification, error)
}

func validUnsignedMoney(value string) bool { return unsignedMoneyPattern.MatchString(value) }
func validCurrency(value string) bool {
	if len(value) < 3 || len(value) > 8 || value != strings.ToUpper(value) {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
func validNotificationType(value string) bool {
	if value == "" || len(value) > 64 || value != strings.ToUpper(value) {
		return false
	}
	for _, r := range value {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}
func validNotificationKind(value string) bool {
	switch value {
	case "ping", "order", "return", "review", "chat", "question", "product", "coupon", "customer":
		return true
	default:
		return false
	}
}
