package connectors

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrInvalidAdvertisingRequest = errors.New("connectors: invalid advertising request")

// AdvertisingQuery is a bounded time-window request. The connector owns the
// opaque cursor and translates provider-specific periods at its edge.
type AdvertisingQuery struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	CampaignIDs []string  `json:"campaign_ids,omitempty"`
	Cursor      string    `json:"cursor,omitempty"`
	Limit       int       `json:"limit"`
}

func (q AdvertisingQuery) Validate(maxCampaigns, maxLimit int) error {
	if maxCampaigns < 1 || maxLimit < 1 || q.From.IsZero() || q.To.IsZero() || q.From.Location() != time.UTC || q.To.Location() != time.UTC || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour || len(q.CampaignIDs) > maxCampaigns || q.Limit < 1 || q.Limit > maxLimit || len(q.Cursor) > 4096 {
		return ErrInvalidAdvertisingRequest
	}
	seen := map[string]struct{}{}
	for _, id := range q.CampaignIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidAdvertisingRequest
		}
		if _, ok := seen[id]; ok {
			return ErrInvalidAdvertisingRequest
		}
		seen[id] = struct{}{}
	}
	return nil
}

// RemoteCampaign is the SDK projection for campaign metadata.
type RemoteCampaign struct {
	RemoteID         string    `json:"remote_id"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	Currency         string    `json:"currency"`
	DailyBudgetMinor int64     `json:"daily_budget_minor"`
	TotalBudgetMinor int64     `json:"total_budget_minor"`
	ObservedAt       time.Time `json:"observed_at"`
}

// RemoteAdSpendFact is a normalized provider response. It contains no raw
// response body and no credential material.
type RemoteAdSpendFact struct {
	RemoteFactID string    `json:"remote_fact_id"`
	CampaignID   string    `json:"campaign_id"`
	AdID         string    `json:"ad_id,omitempty"`
	SKU          string    `json:"sku,omitempty"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	AmountMinor  int64     `json:"amount_minor"`
	Currency     string    `json:"currency"`
	ObservedAt   time.Time `json:"observed_at"`
	EffectiveAt  time.Time `json:"effective_at"`
	Quality      string    `json:"quality"`
}

// RemoteAdPerformanceFact is the normalized delivery and conversion result.
type RemoteAdPerformanceFact struct {
	RemoteFactID string    `json:"remote_fact_id"`
	CampaignID   string    `json:"campaign_id"`
	AdID         string    `json:"ad_id,omitempty"`
	SKU          string    `json:"sku,omitempty"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	Impressions  int64     `json:"impressions"`
	Clicks       int64     `json:"clicks"`
	Orders       int64     `json:"orders"`
	RevenueMinor int64     `json:"revenue_minor"`
	Currency     string    `json:"currency"`
	ObservedAt   time.Time `json:"observed_at"`
	EffectiveAt  time.Time `json:"effective_at"`
	Quality      string    `json:"quality"`
}

type AdvertisingCampaignPage struct {
	Items      []RemoteCampaign `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
type AdvertisingSpendPage struct {
	Items      []RemoteAdSpendFact `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}
type AdvertisingPerformancePage struct {
	Items      []RemoteAdPerformanceFact `json:"items"`
	NextCursor string                    `json:"next_cursor,omitempty"`
}

// AdvertisingReader is the read-only MVP surface for marketplace ads.
type AdvertisingReader interface {
	ReadAdvertisingCampaigns(context.Context, Account, Runtime, PageRequest) (AdvertisingCampaignPage, error)
	ReadAdvertisingSpend(context.Context, Account, Runtime, AdvertisingQuery) (AdvertisingSpendPage, error)
	ReadAdvertisingPerformance(context.Context, Account, Runtime, AdvertisingQuery) (AdvertisingPerformancePage, error)
}

// AdvertisingOperationName names the second-stage management operations. The
// interface is typed now so providers cannot smuggle an unbounded Invoke map
// into the SDK; no marketplace advertises ads.manage in the read-only MVP.
type AdvertisingOperationName string

const (
	AdvertisingLaunch       AdvertisingOperationName = "launch"
	AdvertisingStop         AdvertisingOperationName = "stop"
	AdvertisingPause        AdvertisingOperationName = "pause"
	AdvertisingSetBudget    AdvertisingOperationName = "set_budget"
	AdvertisingSetBid       AdvertisingOperationName = "set_bid"
	AdvertisingLinkProducts AdvertisingOperationName = "link_products"
	AdvertisingArchive      AdvertisingOperationName = "archive"
)

// AdvertisingOperation is validated host-side before a future remote write.
type AdvertisingOperation struct {
	Name           AdvertisingOperationName `json:"name"`
	CampaignID     string                   `json:"campaign_id"`
	AmountMinor    int64                    `json:"amount_minor,omitempty"`
	Currency       string                   `json:"currency,omitempty"`
	ProductIDs     []string                 `json:"product_ids,omitempty"`
	IdempotencyKey string                   `json:"idempotency_key"`
	DryRun         bool                     `json:"dry_run"`
}

func (o AdvertisingOperation) Validate() error {
	validName := o.Name == AdvertisingLaunch || o.Name == AdvertisingStop || o.Name == AdvertisingPause || o.Name == AdvertisingSetBudget || o.Name == AdvertisingSetBid || o.Name == AdvertisingLinkProducts || o.Name == AdvertisingArchive
	if !validName || !validRemoteReadID(o.CampaignID) || len(o.IdempotencyKey) < 1 || len(o.IdempotencyKey) > 128 || o.IdempotencyKey != strings.TrimSpace(o.IdempotencyKey) || o.AmountMinor < 0 || (o.AmountMinor > 0 && len(o.Currency) != 3) || len(o.ProductIDs) > 1000 {
		return ErrInvalidAdvertisingRequest
	}
	for _, id := range o.ProductIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidAdvertisingRequest
		}
	}
	return nil
}

type AdvertisingOperationState string

const (
	AdvertisingAccepted AdvertisingOperationState = "accepted"
	AdvertisingRejected AdvertisingOperationState = "rejected"
	AdvertisingUnknown  AdvertisingOperationState = "unknown"
)

type AdvertisingOperationResult struct {
	State             AdvertisingOperationState `json:"state"`
	RemoteOperationID string                    `json:"remote_operation_id,omitempty"`
	ReadAfterWrite    bool                      `json:"read_after_write"`
}

// AdvertisingManager is intentionally additive to Connector SDK v1. Providers
// must implement it only after a separate capability qualification.
type AdvertisingManager interface {
	ApplyAdvertisingOperation(context.Context, Account, Runtime, AdvertisingOperation) (AdvertisingOperationResult, error)
}
