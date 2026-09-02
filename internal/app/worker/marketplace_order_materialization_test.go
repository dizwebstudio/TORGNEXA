package worker

import (
	"context"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	materializationOrderID = "0199f7c4-0000-7000-8000-000000000001"
	materializationItemID  = "0199f7c4-0000-7000-8000-000000000002"
	materializationOfferID = "0199f7c4-0000-7000-8000-000000000003"
)

func TestBuildMarketplaceOrderCreateRequiresResolvedCanonicalEvidence(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	remote := sdk.RemoteOrder{RemoteID: "posting-1", ExternalID: "ORDER-1", StatusRemoteID: "awaiting_pack", CreatedAt: now, UpdatedAt: now, Items: []sdk.RemoteOrderItem{{RemoteID: "line-1", VariantRemoteID: "offer-remote-1", Quantity: 2}}}
	currency, _ := orders.NewCurrency("RUB")
	unit, _ := orders.NewUnitCode("PCS")
	decimal, _ := orders.NewDecimal(2, 0)
	quantity, _ := orders.NewQuantity(decimal, unit)
	price, _ := orders.NewMoney(1000, currency)
	subtotal, _ := orders.NewMoney(2000, currency)
	zero, _ := orders.NewMoney(0, currency)
	rate, _ := orders.NewDecimal(0, 0)
	line := orders.TaxSnapshot{Jurisdiction: "RU", Category: "zero", Rate: rate, PriceIncludesTax: false}
	command, err := BuildMarketplaceOrderCreate(remote, MarketplaceOrderMaterialization{OrderID: orders.OrderID(materializationOrderID), Number: "ORDER-1", Currency: currency, ShippingTotal: zero, PlacedAt: now, Lines: []MarketplaceOrderLine{{RemoteItemID: "line-1", OfferID: orders.OfferID(materializationOfferID), SKU: "SKU-1", Quantity: quantity, UnitPrice: price, Subtotal: subtotal, Discount: zero, TaxTotal: zero, LineTotal: subtotal, Tax: line, ItemID: orders.OrderItemID(materializationItemID)}}})
	if err != nil {
		t.Fatalf("complete remote snapshot rejected: %v", err)
	}
	if err := command.Validate(); err != nil || len(command.Items) != 1 {
		t.Fatalf("canonical create invalid: err=%v command=%#v", err, command)
	}
}

func TestBuildMarketplaceOrderCreateRejectsMissingMappingOrPricing(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	remote := sdk.RemoteOrder{RemoteID: "posting-1", ExternalID: "ORDER-1", StatusRemoteID: "awaiting_pack", CreatedAt: now, UpdatedAt: now, Items: []sdk.RemoteOrderItem{{RemoteID: "line-1", VariantRemoteID: "offer-remote-1", Quantity: 1}}}
	currency, _ := orders.NewCurrency("RUB")
	zero, _ := orders.NewMoney(0, currency)
	_, err := BuildMarketplaceOrderCreate(remote, MarketplaceOrderMaterialization{OrderID: orders.OrderID(materializationOrderID), Number: "ORDER-1", Currency: currency, ShippingTotal: zero, PlacedAt: now})
	if err == nil {
		t.Fatal("order without resolved mapping/pricing was materialized")
	}
}

func TestMarketplaceOrderImporterIsIdempotentAndAdvancesOnlyForward(t *testing.T) {
	ctx := context.Background()
	scope, err := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	currency, _ := orders.NewCurrency("RUB")
	unit, _ := orders.NewUnitCode("PCS")
	quantityValue, _ := orders.NewDecimal(2, 0)
	quantity, _ := orders.NewQuantity(quantityValue, unit)
	price, _ := orders.NewMoney(1000, currency)
	subtotal, _ := orders.NewMoney(2000, currency)
	zero, _ := orders.NewMoney(0, currency)
	rate, _ := orders.NewDecimal(0, 0)
	tax := orders.TaxSnapshot{Jurisdiction: "RU", Category: "zero", Rate: rate, PriceIncludesTax: false}
	remote := sdk.RemoteOrder{RemoteID: "posting-1", ExternalID: "ORDER-1", StatusRemoteID: "processing", CreatedAt: now, UpdatedAt: now, Items: []sdk.RemoteOrderItem{{RemoteID: "line-1", VariantRemoteID: "offer-remote-1", Quantity: 2}}}
	materialization := MarketplaceOrderMaterialization{OrderID: orders.OrderID(materializationOrderID), Number: "ORDER-1", Currency: currency, ShippingTotal: zero, PlacedAt: now, Lines: []MarketplaceOrderLine{{RemoteItemID: "line-1", OfferID: orders.OfferID(materializationOfferID), SKU: "SKU-1", Quantity: quantity, UnitPrice: price, Subtotal: subtotal, Discount: zero, TaxTotal: zero, LineTotal: subtotal, Tax: tax, ItemID: orders.OrderItemID(materializationItemID)}}}
	store := newImporterOrderStore()
	mappings := newImporterMappingStore()
	importer, err := NewMarketplaceOrderImporter(store, mappings)
	if err != nil {
		t.Fatal(err)
	}
	mutation := orders.Mutation{EventID: "evt_marketplace_import_1", AuditID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", ActorID: "system", Source: "worker.marketplace", CorrelationID: "corr_marketplace_import_1", OccurredAt: now}
	first, err := importer.Import(ctx, scope, "account-1", remote, materialization, orders.StatusProcessing, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate || !first.StatusAdvanced || first.Order.Status != orders.StatusProcessing || store.creates != 1 {
		t.Fatalf("first import = %+v, creates=%d", first, store.creates)
	}
	second, err := importer.Import(ctx, scope, "account-1", remote, materialization, orders.StatusFulfilled, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || !second.StatusAdvanced || second.Order.Status != orders.StatusFulfilled || store.creates != 1 {
		t.Fatalf("duplicate import = %+v, creates=%d", second, store.creates)
	}
	stale, err := importer.Import(ctx, scope, "account-1", remote, materialization, orders.StatusConfirmed, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if !stale.Duplicate || stale.StatusAdvanced || stale.Order.Status != orders.StatusFulfilled {
		t.Fatalf("stale remote status regressed order: %+v", stale)
	}
}

type importerOrderStore struct {
	orders  map[orders.OrderID]orders.Order
	creates int
}

func newImporterOrderStore() *importerOrderStore {
	return &importerOrderStore{orders: map[orders.OrderID]orders.Order{}}
}

func (s *importerOrderStore) Order(_ context.Context, _ orders.Scope, id orders.OrderID) (orders.Order, error) {
	order, ok := s.orders[id]
	if !ok {
		return orders.Order{}, orders.ErrNotFound
	}
	return order, nil
}

func (s *importerOrderStore) Create(_ context.Context, scope orders.Scope, command orders.CreateOrder, _ orders.Mutation) (orders.Order, error) {
	if _, ok := s.orders[command.ID]; ok {
		return orders.Order{}, orders.ErrConflict
	}
	order, err := orders.BuildCreate(command, scope, time.Date(2026, 9, 1, 10, 0, 1, 0, time.UTC))
	if err != nil {
		return orders.Order{}, err
	}
	s.orders[order.ID] = order
	s.creates++
	return order, nil
}

func (s *importerOrderStore) ChangeStatus(_ context.Context, _ orders.Scope, command orders.ChangeStatus, _ orders.Mutation) (orders.Order, error) {
	current, ok := s.orders[command.ID]
	if !ok {
		return orders.Order{}, orders.ErrNotFound
	}
	if current.Version != command.ExpectedVersion {
		return orders.Order{}, orders.ErrConflict
	}
	if err := orders.ValidateTransition(current.Status, command.Status); err != nil {
		return orders.Order{}, err
	}
	current.Status = command.Status
	current.Version++
	current.UpdatedAt = current.UpdatedAt.Add(time.Second).UTC()
	s.orders[current.ID] = current
	return current, nil
}

type importerMappingStore struct {
	byRemote map[string]sdk.EntityMapping
}

func newImporterMappingStore() *importerMappingStore {
	return &importerMappingStore{byRemote: map[string]sdk.EntityMapping{}}
}

func (s *importerMappingStore) MappingByRemote(_ context.Context, _, _, account, entity, remoteID string) (sdk.EntityMapping, error) {
	mapping, ok := s.byRemote[account+":"+entity+":"+remoteID]
	if !ok {
		return sdk.EntityMapping{}, sdk.ErrMappingNotFound
	}
	return mapping, nil
}

func (s *importerMappingStore) MappingByLocal(_ context.Context, _, _, _, _, _ string) (sdk.EntityMapping, error) {
	return sdk.EntityMapping{}, sdk.ErrMappingNotFound
}

func (s *importerMappingStore) UpsertMapping(_ context.Context, command sdk.MappingUpsert) (sdk.EntityMapping, error) {
	key := command.ConnectorAccountID + ":" + command.EntityType + ":" + command.RemoteID
	if _, ok := s.byRemote[key]; ok {
		return sdk.EntityMapping{}, sdk.ErrMappingConflict
	}
	item := sdk.EntityMapping{OrganizationID: command.OrganizationID, WorkspaceID: command.WorkspaceID, ConnectorAccountID: command.ConnectorAccountID, EntityType: command.EntityType, LocalEntityID: command.LocalEntityID, RemoteID: command.RemoteID, Version: 1, CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)}
	if err := item.Validate(); err != nil {
		return sdk.EntityMapping{}, err
	}
	s.byRemote[key] = item
	return item, nil
}

var _ marketplaceOrderStore = (*importerOrderStore)(nil)
var _ sdk.MappingRepository = (*importerMappingStore)(nil)
