package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

// MemoryProjection is a deterministic semantic reference used by tests/local
// development. It deliberately deduplicates by event_id to model replay-safe
// analytical projections, but it is not a production substitute for ClickHouse.
type MemoryProjection struct {
	mu        sync.Mutex
	eventIDs  map[string]struct{}
	orders    map[string]orderState
	inventory map[string]InventoryPosition
	freshness map[string]Freshness
}

type orderState struct {
	OrganizationID string
	WorkspaceID    string
	ID             string
	Status         string
	Currency       string
	TotalMinor     int64
	Version        uint64
	LastEventID    string
	FirstSeen      time.Time
	ChangedAt      time.Time
}

func NewMemoryProjection() *MemoryProjection {
	return &MemoryProjection{eventIDs: map[string]struct{}{}, orders: map[string]orderState{}, inventory: map[string]InventoryPosition{}, freshness: map[string]Freshness{}}
}

func (m *MemoryProjection) Append(_ context.Context, batch Batch) error {
	if _, err := NewBatch(batch.Records); err != nil || batch.DedupToken != batchToken(batch.Records) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, record := range batch.Records {
		if _, duplicate := m.eventIDs[record.EventID]; duplicate {
			continue
		}
		m.eventIDs[record.EventID] = struct{}{}
		m.apply(record)
	}
	return nil
}

func (m *MemoryProjection) apply(record Record) {
	family, _ := record.EventType.Family()
	key := record.OrganizationID + "\x00" + record.WorkspaceID + "\x00" + family
	f := m.freshness[key]
	if f.EventCount == 0 || record.OccurredAt.Time().After(f.LastOccurredAt) {
		f.LastOccurredAt = record.OccurredAt.Time()
	}
	if f.EventCount == 0 || record.IngestedAt.Time().After(f.LastIngestedAt) {
		f.LastIngestedAt = record.IngestedAt.Time()
	}
	f.EventFamily = family
	f.EventCount++
	f.ObservedAt = record.IngestedAt.Time()
	if f.LastIngestedAt.After(f.LastOccurredAt) {
		f.SourceLag = f.LastIngestedAt.Sub(f.LastOccurredAt)
	} else {
		f.SourceLag = 0
	}
	m.freshness[key] = f

	switch record.EventType.String() {
	case "commerce.orders.order_changed.v1":
		var payload struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
			Total   struct {
				MinorUnits int64  `json:"minor_units"`
				Currency   string `json:"currency"`
			} `json:"total"`
			Version uint64 `json:"version"`
		}
		if json.Unmarshal(record.AnalyticsData, &payload) != nil || payload.OrderID == "" || payload.Version == 0 || payload.Total.Currency == "" {
			return
		}
		stateKey := record.OrganizationID + "\x00" + record.WorkspaceID + "\x00" + payload.OrderID
		current, exists := m.orders[stateKey]
		if !exists || payload.Version > current.Version || (payload.Version == current.Version && record.EventID > current.LastEventID) {
			firstSeen := record.OccurredAt.Time()
			if exists && !current.FirstSeen.IsZero() && current.FirstSeen.Before(firstSeen) {
				firstSeen = current.FirstSeen
			}
			m.orders[stateKey] = orderState{OrganizationID: record.OrganizationID, WorkspaceID: record.WorkspaceID, ID: payload.OrderID, Status: payload.Status, Currency: payload.Total.Currency, TotalMinor: payload.Total.MinorUnits, Version: payload.Version, LastEventID: record.EventID, FirstSeen: firstSeen, ChangedAt: record.OccurredAt.Time()}
		}
	case "commerce.inventory.stock_changed.v1":
		var payload struct {
			OfferID     string `json:"offer_id"`
			WarehouseID string `json:"warehouse_id"`
			NewQuantity string `json:"new_quantity"`
		}
		if json.Unmarshal(record.AnalyticsData, &payload) != nil || payload.OfferID == "" || payload.WarehouseID == "" || !validDecimal(payload.NewQuantity) {
			return
		}
		stateKey := record.OrganizationID + "\x00" + record.WorkspaceID + "\x00" + payload.OfferID + "\x00" + payload.WarehouseID
		current, exists := m.inventory[stateKey]
		if !exists || record.OccurredAt.Time().After(current.ChangedAt) || (record.OccurredAt.Time().Equal(current.ChangedAt) && record.EventID > current.EventID) {
			m.inventory[stateKey] = InventoryPosition{OfferID: payload.OfferID, WarehouseID: payload.WarehouseID, Quantity: payload.NewQuantity, ChangedAt: record.OccurredAt.Time(), EventID: record.EventID}
		}
	}
}

func (m *MemoryProjection) Sales(_ context.Context, scope tenancy.Scope, query SalesQuery) ([]SalesBucket, error) {
	if !scope.Valid() || query.Validate() != nil {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	type key struct{ day, currency string }
	buckets := map[key]SalesBucket{}
	for _, order := range m.orders {
		if order.OrganizationID != scope.OrganizationID().String() || order.WorkspaceID != scope.WorkspaceID().String() || order.FirstSeen.Before(query.From) || !order.FirstSeen.Before(query.To) {
			continue
		}
		day := time.Date(order.FirstSeen.Year(), order.FirstSeen.Month(), order.FirstSeen.Day(), 0, 0, 0, 0, time.UTC)
		k := key{day: day.Format("2006-01-02"), currency: order.Currency}
		bucket := buckets[k]
		bucket.Day, bucket.Currency = day, order.Currency
		bucket.Orders++
		if order.Status == "fulfilled" {
			bucket.FulfilledOrders++
		}
		if order.Status == "cancelled" {
			bucket.CancelledOrders++
		}
		if order.Status != "cancelled" {
			bucket.GrossMinorUnits += order.TotalMinor
		}
		buckets[k] = bucket
	}
	rows := make([]SalesBucket, 0, len(buckets))
	for _, row := range buckets {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Day.Equal(rows[j].Day) {
			return rows[i].Currency < rows[j].Currency
		}
		return rows[i].Day.Before(rows[j].Day)
	})
	if len(rows) > query.Limit {
		rows = rows[:query.Limit]
	}
	return rows, nil
}

func (m *MemoryProjection) Inventory(_ context.Context, scope tenancy.Scope, query InventoryQuery) ([]InventoryPosition, error) {
	if !scope.Valid() || query.Validate() != nil {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := []InventoryPosition{}
	prefix := scope.OrganizationID().String() + "\x00" + scope.WorkspaceID().String() + "\x00"
	for key, row := range m.inventory {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix || row.OfferID != query.OfferID || (query.WarehouseID != "" && row.WarehouseID != query.WarehouseID) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].WarehouseID < rows[j].WarehouseID })
	if len(rows) > query.Limit {
		rows = rows[:query.Limit]
	}
	return rows, nil
}

func (m *MemoryProjection) Freshness(_ context.Context, scope tenancy.Scope) ([]Freshness, error) {
	if !scope.Valid() {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := scope.OrganizationID().String() + "\x00" + scope.WorkspaceID().String() + "\x00"
	rows := []Freshness{}
	for key, row := range m.freshness {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

var _ Sink = (*MemoryProjection)(nil)
var _ QueryPort = (*MemoryProjection)(nil)
var _ = errors.Is
