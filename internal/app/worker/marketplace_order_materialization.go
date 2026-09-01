package worker

import (
	"context"
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrMarketplaceOrderMaterialization = errors.New("worker: marketplace order materialization requires canonical evidence")

// ErrMarketplaceOrderImportConflict identifies a remote order identity that
// is already mapped to a different canonical order or immutable snapshot.
var ErrMarketplaceOrderImportConflict = errors.New("worker: marketplace order import identity conflict")

// MarketplaceOrderLine is the host-side evidence needed to turn a remote
// order line into the canonical orders aggregate. Remote IDs are lookup keys;
// OfferID and pricing must already be resolved by the tenant-scoped host.
type MarketplaceOrderLine struct {
	RemoteItemID string
	OfferID      orders.OfferID
	SKU          string
	Quantity     orders.Quantity
	UnitPrice    orders.Money
	Subtotal     orders.Money
	Discount     orders.Money
	TaxTotal     orders.Money
	LineTotal    orders.Money
	Tax          orders.TaxSnapshot
	ItemID       orders.OrderItemID
}

// MarketplaceOrderMaterialization is the explicit, provider-neutral input to
// canonical order creation. The repository call remains separate so the
// caller can put mapping, order insert, audit and outbox in its transaction.
type MarketplaceOrderMaterialization struct {
	OrderID       orders.OrderID
	Number        string
	Currency      orders.Currency
	ShippingTotal orders.Money
	PlacedAt      time.Time
	Lines         []MarketplaceOrderLine
}

// BuildMarketplaceOrderCreate verifies that a remote snapshot is complete
// enough for canonical materialization. It never guesses an offer, price,
// tax, quantity or status, and it never stores the remote provider payload.
func BuildMarketplaceOrderCreate(remote sdk.RemoteOrder, input MarketplaceOrderMaterialization) (orders.CreateOrder, error) {
	if remote.Validate() != nil || !input.OrderID.Valid() || input.Currency.Validate() != nil || input.ShippingTotal.Validate() != nil || input.ShippingTotal.Currency() != input.Currency || input.PlacedAt.IsZero() || input.PlacedAt.Location() != time.UTC || len(input.Lines) != len(remote.Items) || len(input.Lines) == 0 {
		return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
	}
	byRemoteItem := make(map[string]MarketplaceOrderLine, len(input.Lines))
	for _, line := range input.Lines {
		if line.RemoteItemID == "" || !line.OfferID.Valid() || !line.ItemID.Valid() || line.Quantity.Validate() != nil || line.UnitPrice.Validate() != nil || line.Subtotal.Validate() != nil || line.Discount.Validate() != nil || line.TaxTotal.Validate() != nil || line.LineTotal.Validate() != nil || line.Tax.Validate() != nil {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		if line.SKU == "" || line.UnitPrice.Currency() != input.Currency || line.Subtotal.Currency() != input.Currency || line.Discount.Currency() != input.Currency || line.TaxTotal.Currency() != input.Currency || line.LineTotal.Currency() != input.Currency || line.Discount.MinorUnits() > line.Subtotal.MinorUnits() {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		if _, exists := byRemoteItem[line.RemoteItemID]; exists {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		byRemoteItem[line.RemoteItemID] = line
	}
	items := make([]orders.CreateItem, 0, len(remote.Items))
	for index, remoteItem := range remote.Items {
		line, ok := byRemoteItem[remoteItem.RemoteID]
		if !ok || line.Quantity.Value.Coefficient() != remoteItem.Quantity || line.Quantity.Value.Scale() != 0 {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		if line.SKU == "" {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		items = append(items, orders.CreateItem{ID: line.ItemID, OfferID: line.OfferID, Position: index + 1, SKU: line.SKU, Quantity: line.Quantity, UnitPrice: line.UnitPrice, Subtotal: line.Subtotal, DiscountTotal: line.Discount, TaxTotal: line.TaxTotal, LineTotal: line.LineTotal, Tax: line.Tax})
	}
	command := orders.CreateOrder{ID: input.OrderID, Number: input.Number, Currency: input.Currency, Items: items, ShippingTotal: input.ShippingTotal, PlacedAt: input.PlacedAt}
	if command.Validate() != nil {
		return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
	}
	return command, nil
}

// marketplaceOrderStore is the host-side persistence boundary for canonical
// orders. It deliberately exposes only the existing Orders aggregate; the
// marketplace importer never creates a parallel order model.
type marketplaceOrderStore interface {
	Order(context.Context, orders.Scope, orders.OrderID) (orders.Order, error)
	Create(context.Context, orders.Scope, orders.CreateOrder, orders.Mutation) (orders.Order, error)
	ChangeStatus(context.Context, orders.Scope, orders.ChangeStatus, orders.Mutation) (orders.Order, error)
}

// MarketplaceOrderImportResult describes an idempotent host import. Duplicate
// means that the remote identity was already mapped; the returned Order is
// always the canonical aggregate after any forward-only status advancement.
type MarketplaceOrderImportResult struct {
	Order          orders.Order
	Duplicate      bool
	StatusAdvanced bool
}

// MarketplaceOrderImporter materializes a validated remote snapshot into the
// canonical Orders aggregate and records the remote identity in the existing
// connector mapping table. Mapping lookup, order creation and status changes
// are intentionally kept in the host so provider adapters only normalize
// remote data and never write directly to Core.
type MarketplaceOrderImporter struct {
	orders  marketplaceOrderStore
	mapping sdk.MappingRepository
	now     func() time.Time
}

// NewMarketplaceOrderImporter constructs the tenant-scoped host importer.
func NewMarketplaceOrderImporter(store marketplaceOrderStore, mappings sdk.MappingRepository) (*MarketplaceOrderImporter, error) {
	if store == nil || mappings == nil {
		return nil, errors.New("worker: marketplace order importer dependencies required")
	}
	return &MarketplaceOrderImporter{orders: store, mapping: mappings, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Import creates or reuses a canonical order for one remote order. The caller
// supplies resolved offer, money, tax and canonical status evidence; the
// importer refuses to guess any of them. Remote status values must be mapped
// to orders.Status at the connector boundary before this method is called.
func (i *MarketplaceOrderImporter) Import(ctx context.Context, scope tenancy.Scope, connectorAccountID string, remote sdk.RemoteOrder, materialization MarketplaceOrderMaterialization, targetStatus orders.Status, mutation orders.Mutation) (MarketplaceOrderImportResult, error) {
	if i == nil || i.orders == nil || i.mapping == nil || ctx == nil || !scope.Valid() || connectorAccountID == "" || remote.Validate() != nil || remote.ExternalID == "" || materialization.Number != remote.ExternalID || !targetStatus.Valid() || mutation.Validate() != nil {
		return MarketplaceOrderImportResult{}, ErrMarketplaceOrderMaterialization
	}
	orderScope, err := orders.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return MarketplaceOrderImportResult{}, ErrMarketplaceOrderMaterialization
	}
	if !materialization.OrderID.Valid() {
		return MarketplaceOrderImportResult{}, ErrMarketplaceOrderMaterialization
	}
	command, err := BuildMarketplaceOrderCreate(remote, materialization)
	if err != nil {
		return MarketplaceOrderImportResult{}, err
	}
	org, workspace, account := scope.OrganizationID().String(), scope.WorkspaceID().String(), connectorAccountID
	mapped, mapErr := i.mapping.MappingByRemote(ctx, org, workspace, account, "order", remote.RemoteID)
	if mapErr == nil {
		orderID, parseErr := orders.ParseOrderID(mapped.LocalEntityID)
		if parseErr != nil {
			return MarketplaceOrderImportResult{}, ErrMarketplaceOrderImportConflict
		}
		current, readErr := i.orders.Order(ctx, orderScope, orderID)
		if readErr != nil {
			return MarketplaceOrderImportResult{}, readErr
		}
		if current.Number != command.Number || !canonicalOrderMatches(command, current) {
			return MarketplaceOrderImportResult{}, ErrMarketplaceOrderImportConflict
		}
		updated, advanced, statusErr := i.advanceStatus(ctx, orderScope, current, targetStatus, mutation, remote.RemoteID)
		if statusErr != nil {
			return MarketplaceOrderImportResult{}, statusErr
		}
		return MarketplaceOrderImportResult{Order: updated, Duplicate: true, StatusAdvanced: advanced}, nil
	}
	if !errors.Is(mapErr, sdk.ErrMappingNotFound) {
		return MarketplaceOrderImportResult{}, mapErr
	}
	created, createErr := i.orders.Create(ctx, orderScope, command, mutation)
	if errors.Is(createErr, orders.ErrConflict) {
		// A previous attempt may have committed the canonical order but crashed
		// before the mapping insert. Reusing the exact deterministic ID is safe;
		// a different order identity is never silently merged.
		created, createErr = i.orders.Order(ctx, orderScope, command.ID)
		if createErr == nil && !canonicalOrderMatches(command, created) {
			createErr = ErrMarketplaceOrderImportConflict
		}
	}
	if createErr != nil {
		return MarketplaceOrderImportResult{}, createErr
	}
	if _, mapErr = i.mapping.UpsertMapping(ctx, sdk.MappingUpsert{OrganizationID: org, WorkspaceID: workspace, ConnectorAccountID: account, EntityType: "order", LocalEntityID: command.ID.String(), RemoteID: remote.RemoteID, ExpectedVersion: 0}); mapErr != nil {
		if errors.Is(mapErr, sdk.ErrMappingConflict) {
			existing, lookupErr := i.mapping.MappingByRemote(ctx, org, workspace, account, "order", remote.RemoteID)
			if lookupErr == nil && existing.LocalEntityID == command.ID.String() {
				// Concurrent delivery established the same identity.
			} else {
				return MarketplaceOrderImportResult{}, ErrMarketplaceOrderImportConflict
			}
		} else {
			return MarketplaceOrderImportResult{}, mapErr
		}
	}
	updated, advanced, statusErr := i.advanceStatus(ctx, orderScope, created, targetStatus, mutation, remote.RemoteID)
	if statusErr != nil {
		return MarketplaceOrderImportResult{}, statusErr
	}
	return MarketplaceOrderImportResult{Order: updated, StatusAdvanced: advanced}, nil
}

func canonicalOrderMatches(command orders.CreateOrder, current orders.Order) bool {
	if current.ID != command.ID || current.Number != command.Number || current.Currency != command.Currency || current.ShippingTotal != command.ShippingTotal || len(current.Items) != len(command.Items) {
		return false
	}
	for index, expected := range command.Items {
		actual := current.Items[index]
		if actual.ID != expected.ID || actual.OfferID != expected.OfferID || actual.Position != expected.Position || actual.SKU != expected.SKU || actual.Quantity != expected.Quantity || actual.UnitPrice != expected.UnitPrice || actual.Subtotal != expected.Subtotal || actual.DiscountTotal != expected.DiscountTotal || actual.TaxTotal != expected.TaxTotal || actual.LineTotal != expected.LineTotal || actual.Tax != expected.Tax {
			return false
		}
	}
	return true
}

func (i *MarketplaceOrderImporter) advanceStatus(ctx context.Context, scope orders.Scope, current orders.Order, target orders.Status, mutation orders.Mutation, remoteID string) (orders.Order, bool, error) {
	if current.Status == target || statusRank(target) < statusRank(current.Status) || (current.Status.Terminal() && target != current.Status) {
		return current, false, nil
	}
	advanced := false
	for current.Status != target {
		next, ok := nextImportedStatus(current.Status, target)
		if !ok {
			return orders.Order{}, advanced, orders.ErrInvalidState
		}
		stepMutation := mutation
		stepMutation.EventID = stableID("mkt_order_import_", mutation.EventID+":"+remoteID+":"+string(next))
		stepMutation.AuditID = stableUUID("mkt_order_import_audit:" + mutation.EventID + ":" + remoteID + ":" + string(next))
		stepMutation.CausationID = remoteID
		stepMutation.OccurredAt = i.now().UTC()
		updated, err := i.orders.ChangeStatus(ctx, scope, orders.ChangeStatus{ID: current.ID, ExpectedVersion: current.Version, Status: next}, stepMutation)
		if err != nil {
			return orders.Order{}, advanced, err
		}
		current = updated
		advanced = true
	}
	return current, advanced, nil
}

func nextImportedStatus(current, target orders.Status) (orders.Status, bool) {
	if target == orders.StatusCancelled && current != orders.StatusFulfilled && !current.Terminal() {
		return orders.StatusCancelled, true
	}
	switch current {
	case orders.StatusPending:
		return orders.StatusConfirmed, target == orders.StatusConfirmed || target == orders.StatusProcessing || target == orders.StatusFulfilled
	case orders.StatusConfirmed:
		return orders.StatusProcessing, target == orders.StatusProcessing || target == orders.StatusFulfilled
	case orders.StatusProcessing:
		return orders.StatusFulfilled, target == orders.StatusFulfilled
	default:
		return "", false
	}
}

func statusRank(status orders.Status) int {
	switch status {
	case orders.StatusPending:
		return 1
	case orders.StatusConfirmed:
		return 2
	case orders.StatusProcessing:
		return 3
	case orders.StatusFulfilled, orders.StatusCancelled:
		return 4
	default:
		return 0
	}
}
