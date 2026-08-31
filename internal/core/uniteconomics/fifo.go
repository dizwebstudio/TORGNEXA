package uniteconomics

import (
	"errors"
	"math"
	"math/big"
	"sort"
	"time"
)

var ErrFIFOCostUnavailable = errors.New("fifo: historical cost unavailable")

type FIFOMovementKind string

const (
	FIFOMovementReceive    FIFOMovementKind = "receive"
	FIFOMovementMoveIn     FIFOMovementKind = "move_in"
	FIFOMovementMoveOut    FIFOMovementKind = "move_out"
	FIFOMovementSale       FIFOMovementKind = "sale"
	FIFOMovementReturn     FIFOMovementKind = "return"
	FIFOMovementScrap      FIFOMovementKind = "scrap"
	FIFOMovementQuarantine FIFOMovementKind = "quarantine"
	FIFOMovementRelease    FIFOMovementKind = "release"
)

func (k FIFOMovementKind) valid() bool {
	switch k {
	case FIFOMovementReceive, FIFOMovementMoveIn, FIFOMovementMoveOut, FIFOMovementSale, FIFOMovementReturn, FIFOMovementScrap, FIFOMovementQuarantine, FIFOMovementRelease:
		return true
	}
	return false
}

// StockMovement is the minimum historical stock evidence needed to value a
// sale. QuantityMilli is an exact fixed-point quantity (one unit = 1000).
type StockMovement struct {
	ID                string
	SKU               string
	WarehouseID       string
	FromWarehouseID   string
	ToWarehouseID     string
	RelatedMovementID string
	Kind              FIFOMovementKind
	QuantityMilli     int64
	UnitCostMinor     int64
	Currency          string
	SourceRef         string
	OccurredAt        time.Time
}

type FIFOAllocation struct {
	MovementID    string `json:"movement_id"`
	LayerID       string `json:"layer_id"`
	SKU           string `json:"sku"`
	WarehouseID   string `json:"warehouse_id"`
	QuantityMilli int64  `json:"quantity_milli"`
	UnitCostMinor int64  `json:"unit_cost_minor_units"`
	CostMinor     int64  `json:"cost_minor_units"`
	Currency      string `json:"currency"`
	SourceRef     string `json:"source_ref"`
}

type FIFOIssue struct {
	MovementID     string `json:"movement_id"`
	SKU            string `json:"sku"`
	WarehouseID    string `json:"warehouse_id"`
	RequestedMilli int64  `json:"requested_milli"`
	AvailableMilli int64  `json:"available_milli"`
	Reason         string `json:"reason"`
}

type FIFOResult struct {
	Allocations            []FIFOAllocation `json:"allocations"`
	Issues                 []FIFOIssue      `json:"issues"`
	Layers                 []StockMovement  `json:"remaining_layers"`
	ValuationPolicyVersion string           `json:"valuation_policy_version"`
}

type fifoLayer struct {
	id, sku, warehouse, currency, source string
	quantity, unitCost                   int64
}
type fifoChunk struct {
	layerID            string
	quantity, unitCost int64
	currency, source   string
}

func fifoCost(unitCost, quantity int64) (int64, error) {
	if unitCost < 0 || quantity < 0 || unitCost > math.MaxInt64 || quantity > math.MaxInt64 {
		return 0, ErrInvalid
	}
	n := new(big.Int).Mul(big.NewInt(unitCost), big.NewInt(quantity))
	n.Add(n, big.NewInt(500))
	n.Quo(n, big.NewInt(1000))
	if !n.IsInt64() {
		return 0, ErrInvalid
	}
	return n.Int64(), nil
}

func validateMovement(m StockMovement) error {
	if m.ID == "" || m.SKU == "" || !m.Kind.valid() || m.QuantityMilli <= 0 || m.UnitCostMinor < 0 || !currencyPattern.MatchString(m.Currency) || m.SourceRef == "" || m.OccurredAt.IsZero() || !m.OccurredAt.Equal(m.OccurredAt.UTC()) {
		return ErrInvalid
	}
	return nil
}

// ValueFIFO applies immutable movements in event order. Transfers carry the
// consumed layers by RelatedMovementID; missing stock returns explicit issues
// and ErrFIFOCostUnavailable, never a zero COGS value.
func ValueFIFO(movements []StockMovement) (FIFOResult, error) {
	ordered := append([]StockMovement(nil), movements...)
	for _, m := range ordered {
		if err := validateMovement(m); err != nil {
			return FIFOResult{}, err
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].OccurredAt.Equal(ordered[j].OccurredAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
	})
	layers := map[string][]*fifoLayer{}
	moved := map[string][]fifoChunk{}
	result := FIFOResult{Allocations: make([]FIFOAllocation, 0), Issues: make([]FIFOIssue, 0), ValuationPolicyVersion: ValuationPolicyVersion}
	add := func(warehouse string, m StockMovement, qty, cost int64) {
		layers[warehouse+"\x00"+m.SKU] = append(layers[warehouse+"\x00"+m.SKU], &fifoLayer{id: m.ID, sku: m.SKU, warehouse: warehouse, currency: m.Currency, source: m.SourceRef, quantity: qty, unitCost: cost})
	}
	consume := func(m StockMovement, warehouse string) ([]fifoChunk, int64) {
		key := warehouse + "\x00" + m.SKU
		remaining := m.QuantityMilli
		chunks := make([]fifoChunk, 0)
		available := int64(0)
		for _, l := range layers[key] {
			available += l.quantity
		}
		for _, l := range layers[key] {
			if remaining == 0 {
				break
			}
			if l.quantity <= 0 {
				continue
			}
			take := l.quantity
			if take > remaining {
				take = remaining
			}
			chunks = append(chunks, fifoChunk{l.id, take, l.unitCost, l.currency, l.source})
			l.quantity -= take
			remaining -= take
		}
		if remaining > 0 {
			result.Issues = append(result.Issues, FIFOIssue{MovementID: m.ID, SKU: m.SKU, WarehouseID: warehouse, RequestedMilli: m.QuantityMilli, AvailableMilli: available - remaining, Reason: "historical_stock_missing"})
		}
		return chunks, remaining
	}
	for _, m := range ordered {
		warehouse := m.WarehouseID
		if m.Kind == FIFOMovementMoveOut {
			warehouse = m.FromWarehouseID
		}
		if warehouse == "" {
			warehouse = m.ToWarehouseID
		}
		switch m.Kind {
		case FIFOMovementReceive, FIFOMovementReturn, FIFOMovementRelease:
			add(warehouse, m, m.QuantityMilli, m.UnitCostMinor)
		case FIFOMovementMoveIn:
			chunks := moved[m.RelatedMovementID]
			if len(chunks) > 0 {
				for _, c := range chunks {
					add(m.ToWarehouseID, m, c.quantity, c.unitCost)
				}
			} else {
				add(m.ToWarehouseID, m, m.QuantityMilli, m.UnitCostMinor)
			}
		case FIFOMovementMoveOut:
			chunks, remaining := consume(m, warehouse)
			if remaining == 0 {
				moved[m.ID] = chunks
			}
		case FIFOMovementSale, FIFOMovementScrap, FIFOMovementQuarantine:
			chunks, remaining := consume(m, warehouse)
			if remaining == 0 && m.Kind == FIFOMovementSale {
				for _, c := range chunks {
					cost, err := fifoCost(c.unitCost, c.quantity)
					if err != nil {
						return FIFOResult{}, err
					}
					result.Allocations = append(result.Allocations, FIFOAllocation{MovementID: m.ID, LayerID: c.layerID, SKU: m.SKU, WarehouseID: warehouse, QuantityMilli: c.quantity, UnitCostMinor: c.unitCost, CostMinor: cost, Currency: c.currency, SourceRef: c.source})
				}
			}
		}
	}
	for _, bucket := range layers {
		for _, l := range bucket {
			if l.quantity > 0 {
				result.Layers = append(result.Layers, StockMovement{ID: l.id, SKU: l.sku, WarehouseID: l.warehouse, Kind: FIFOMovementReceive, QuantityMilli: l.quantity, UnitCostMinor: l.unitCost, Currency: l.currency, SourceRef: l.source})
			}
		}
	}
	sort.Slice(result.Allocations, func(i, j int) bool {
		if result.Allocations[i].MovementID == result.Allocations[j].MovementID {
			return result.Allocations[i].LayerID < result.Allocations[j].LayerID
		}
		return result.Allocations[i].MovementID < result.Allocations[j].MovementID
	})
	if len(result.Issues) > 0 {
		return result, ErrFIFOCostUnavailable
	}
	return result, nil
}
