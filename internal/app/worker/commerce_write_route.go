package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectormaprepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/publicationquality"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const (
	commerceSyncKafkaConsumerGroup = "torgnexa.commerce-sync.v1"
	commerceProductsEntity         = "products"
	commercePricesEntity           = "prices"
	commerceInventoryEntity        = "inventory"
	commerceOrdersEntity           = "orders"
)

type eventOnlyReconciliationSource struct{}

func (eventOnlyReconciliationSource) Scan(ctx context.Context, _ tenancy.Scope, req reconciliation.ScanRequest) (reconciliation.ScanPage, error) {
	if ctx == nil || req.Validate() != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	now := time.Now().UTC()
	return reconciliation.ScanPage{RemoteObservedAt: now, Subjects: []reconciliation.Subject{}}, nil
}

// commerceWriteRuntime is the deliberately small bridge from the worker to
// the reviewed built-in registry. It keeps provider-specific construction out
// of the event route and makes the route unit-testable with deterministic
// connector doubles.
type commerceWriteRuntime interface {
	supportsSync(sdk.Account, string, string) bool
	productStatus(sdk.Account, catalog.Status) (string, bool)
	productWriter(tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.ProductWriter, error)
	priceWriter(tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.PriceWriter, error)
	inventoryWriter(tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.InventoryWriter, error)
	orderStatusWriter(tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.OrderStatusWriter, error)
	orderStatus(tenancy.Scope, sdk.Account, string) (string, bool)
}

type commerceWriteRuntimeFactory func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error)

type commerceWriteRoute struct {
	policies   *syncrepo.Repository
	accounts   *connectorrepo.Repository
	mappings   *connectormaprepo.Repository
	catalog    *catalogrepo.Repository
	receipts   *syncrepo.Repository
	registry   commerceWriteRuntime
	newRuntime commerceWriteRuntimeFactory
	quality    commercePublicationQualityGate
	now        func() time.Time
}

func newCommerceWriteRoute(policies *syncrepo.Repository, accounts *connectorrepo.Repository, mappings *connectormaprepo.Repository, catalogRepository *catalogrepo.Repository, registry commerceWriteRuntime, newRuntime commerceWriteRuntimeFactory, quality ...commercePublicationQualityGate) (*commerceWriteRoute, error) {
	if policies == nil || accounts == nil || mappings == nil || catalogRepository == nil || registry == nil || newRuntime == nil {
		return nil, errors.New("worker: commerce write route dependencies required")
	}
	route := &commerceWriteRoute{policies: policies, accounts: accounts, mappings: mappings, catalog: catalogRepository, receipts: policies, registry: registry, newRuntime: newRuntime, now: func() time.Time { return time.Now().UTC() }}
	if len(quality) > 0 {
		route.quality = quality[0]
	}
	return route, nil
}

// Handle routes canonical product, price and inventory mutations to enabled
// outbound policies. The event id is the durable change id; receipts are
// written only after a connector confirms applied or duplicate, so a crash
// between the remote side effect and receipt commit safely retries with the
// same key.
func (r *commerceWriteRoute) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	if r == nil || ctx == nil {
		return eventbus.Permanent("commerce_sync_invalid_route")
	}
	entity, ok := commerceWriteEntity(delivery.Event.Type)
	if !ok {
		return nil
	}
	scope, err := tenancy.ParseScope(delivery.Event.OrganizationID, delivery.Event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_scope")
	}
	if entity == commerceProductsEntity {
		var productPayload commerceProductEvent
		if err := decodeCommerceEvent(delivery.Event.Data, &productPayload); err != nil || productPayload.ProductID != delivery.Event.EntityID || productPayload.Version < 1 || !catalog.Status(productPayload.Status).Valid() || !validProductChange(productPayload.Change) {
			return eventbus.Permanent("commerce_sync_invalid_payload")
		}
		return r.routeProduct(ctx, scope, delivery.Event, productPayload)
	}
	if entity == commerceOrdersEntity {
		var orderPayload commerceOrderEvent
		if err := decodeCommerceEvent(delivery.Event.Data, &orderPayload); err != nil || orderPayload.OrderID != delivery.Event.EntityID || orderPayload.Version < 1 || !isCanonicalOrderStatus(orderPayload.Status) || orderPayload.Change != "status_changed" {
			return eventbus.Permanent("commerce_sync_invalid_payload")
		}
		return r.routeOrderStatus(ctx, scope, delivery.Event, orderPayload)
	}
	var payload commercePriceEvent
	if entity == commerceInventoryEntity {
		var inventoryPayload commerceInventoryEvent
		if err := decodeCommerceEvent(delivery.Event.Data, &inventoryPayload); err != nil || inventoryPayload.PositionID != delivery.Event.EntityID || inventoryPayload.OfferID == "" || inventoryPayload.WarehouseID == "" || inventoryPayload.Version < 1 {
			return eventbus.Permanent("commerce_sync_invalid_payload")
		}
		return r.routeInventory(ctx, scope, delivery.Event, inventoryPayload)
	}
	if err := decodeCommerceEvent(delivery.Event.Data, &payload); err != nil || payload.PriceID != delivery.Event.EntityID || payload.OfferID == "" || payload.Version < 1 || payload.Kind != "regular" {
		return eventbus.Permanent("commerce_sync_invalid_payload")
	}
	return r.routePrice(ctx, scope, delivery.Event, payload)
}

type commercePriceEvent struct {
	PriceID string       `json:"price_id"`
	OfferID string       `json:"offer_id"`
	Kind    string       `json:"kind"`
	Amount  domain.Money `json:"amount"`
	Version int64        `json:"version"`
	Change  string       `json:"change"`
}

type commerceProductEvent struct {
	ProductID string `json:"product_id"`
	Version   int64  `json:"version"`
	Status    string `json:"status"`
	Change    string `json:"change"`
}

type commerceInventoryEvent struct {
	PositionID  string          `json:"position_id"`
	OfferID     string          `json:"offer_id"`
	WarehouseID string          `json:"warehouse_id"`
	Available   domain.Quantity `json:"available"`
	Version     int64           `json:"version"`
	Change      string          `json:"change"`
	Reason      string          `json:"reason,omitempty"`
}

type commerceOrderEvent struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Version int64  `json:"version"`
	Change  string `json:"change"`
}

func (r *commerceWriteRoute) routePrice(ctx context.Context, scope tenancy.Scope, event eventbus.Event, payload commercePriceEvent) error {
	value, err := moneyToMajor(payload.Amount)
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_price")
	}
	coreScope, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_scope")
	}
	offerID, err := catalog.ParseOfferID(payload.OfferID)
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_price")
	}
	offer, err := r.catalog.Offer(ctx, coreScope, offerID)
	if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrInvalidRecord) {
		return eventbus.Permanent("commerce_sync_offer_missing")
	}
	if err != nil {
		return eventbus.Retryable("commerce_sync_offer_read_failed")
	}
	return r.route(ctx, scope, event, commercePricesEntity, payload.PriceID, "offer", payload.OfferID, payload.Version, false, func(account sdk.Account, runtime sdk.Runtime, policy syncengine.Policy, remoteID string) (sdk.CommerceWriteReceipt, error) {
		productRemoteID := ""
		productMapping, mappingErr := r.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, "product", offer.ProductID.String())
		if mappingErr == nil {
			productRemoteID = productMapping.RemoteID
		} else if !errors.Is(mappingErr, sdk.ErrMappingNotFound) {
			return sdk.CommerceWriteReceipt{}, mappingErr
		}
		writer, err := r.registry.priceWriter(scope, account, runtime)
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		receipt, err := writer.WritePrice(ctx, account, runtime, sdk.PriceWriteRequest{ProductRemoteID: productRemoteID, VariantRemoteID: remoteID, Value: value, Currency: payload.Amount.Currency().String(), IdempotencyKey: commerceSyncIdempotencyKey(policy.ID, event.ID)})
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		return receipt, receipt.Validate()
	})
}

func (r *commerceWriteRoute) routeInventory(ctx context.Context, scope tenancy.Scope, event eventbus.Event, payload commerceInventoryEvent) error {
	quantity, err := discreteQuantity(payload.Available)
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_inventory")
	}
	return r.route(ctx, scope, event, commerceInventoryEntity, payload.PositionID, "offer", payload.OfferID, payload.Version, false, func(account sdk.Account, runtime sdk.Runtime, policy syncengine.Policy, remoteID string) (sdk.CommerceWriteReceipt, error) {
		warehouse, mappingErr := r.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, "warehouse", payload.WarehouseID)
		if errors.Is(mappingErr, sdk.ErrMappingNotFound) {
			return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
		}
		if mappingErr != nil {
			return sdk.CommerceWriteReceipt{}, mappingErr
		}
		if warehouse.RemoteID == "" {
			return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
		}
		writer, err := r.registry.inventoryWriter(scope, account, runtime)
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		receipt, err := writer.WriteInventory(ctx, account, runtime, sdk.InventoryWriteRequest{VariantRemoteID: remoteID, LocationRemoteID: warehouse.RemoteID, Quantity: quantity, IdempotencyKey: commerceSyncIdempotencyKey(policy.ID, event.ID)})
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		return receipt, receipt.Validate()
	})
}

func (r *commerceWriteRoute) routeOrderStatus(ctx context.Context, scope tenancy.Scope, event eventbus.Event, payload commerceOrderEvent) error {
	return r.route(ctx, scope, event, commerceOrdersEntity, payload.OrderID, "order", payload.OrderID, payload.Version, false, func(account sdk.Account, runtime sdk.Runtime, policy syncengine.Policy, remoteID string) (sdk.CommerceWriteReceipt, error) {
		status, ok := r.registry.orderStatus(scope, account, payload.Status)
		if !ok {
			return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
		}
		writer, err := r.registry.orderStatusWriter(scope, account, runtime)
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		receipt, err := writer.WriteOrderStatus(ctx, account, runtime, sdk.OrderStatusWriteRequest{OrderRemoteID: remoteID, StatusRemoteID: status, IdempotencyKey: commerceSyncIdempotencyKey(policy.ID, event.ID)})
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		return receipt, receipt.Validate()
	})
}

func (r *commerceWriteRoute) routeProduct(ctx context.Context, scope tenancy.Scope, event eventbus.Event, payload commerceProductEvent) error {
	coreScope, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_scope")
	}
	productID, err := catalog.ParseProductID(payload.ProductID)
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_payload")
	}
	product, err := r.catalog.Product(ctx, coreScope, productID)
	if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrInvalidRecord) {
		return eventbus.Permanent("commerce_sync_product_missing")
	}
	if err != nil {
		return eventbus.Retryable("commerce_sync_product_read_failed")
	}
	return r.route(ctx, scope, event, commerceProductsEntity, payload.ProductID, "product", payload.ProductID, payload.Version, true, func(account sdk.Account, runtime sdk.Runtime, policy syncengine.Policy, remoteID string) (sdk.CommerceWriteReceipt, error) {
		status, ok := r.registry.productStatus(account, product.Status)
		if !ok {
			return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
		}
		writer, err := r.registry.productWriter(scope, account, runtime)
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		receipt, err := writer.UpsertProduct(ctx, account, runtime, sdk.ProductWriteRequest{
			RemoteID:       remoteID,
			SellerSKU:      product.Code,
			Title:          product.Title,
			Description:    product.Description,
			StatusRemoteID: status,
			IdempotencyKey: commerceSyncIdempotencyKey(policy.ID, event.ID),
		})
		if err != nil {
			return sdk.CommerceWriteReceipt{}, err
		}
		return receipt, receipt.Validate()
	})
}

func validProductChange(change string) bool {
	switch change {
	case "created", "updated", "status_changed":
		return true
	default:
		return false
	}
}

// route applies one event to every eligible tenant policy. Offer mappings are
// intentionally reused for price and inventory variant IDs. Product events
// use the canonical product mapping and may create it after a successful
// remote upsert. No provider ID is copied into canonical events.
func (r *commerceWriteRoute) route(ctx context.Context, scope tenancy.Scope, event eventbus.Event, entity, changeID, mappingEntityType, mappingLocalID string, localVersion int64, allowCreateMapping bool, apply func(sdk.Account, sdk.Runtime, syncengine.Policy, string) (sdk.CommerceWriteReceipt, error)) error {
	if apply == nil || changeID == "" || mappingEntityType == "" || mappingLocalID == "" || localVersion < 1 {
		return eventbus.Permanent("commerce_sync_invalid_payload")
	}
	mutation := syncengine.LocalMutation{EventID: event.ID, EntityType: entity, LocalEntityID: changeID, LocalVersion: localVersion, Operation: syncengine.OperationUpsert, Payload: append(json.RawMessage(nil), event.Data...), Source: event.Source, CorrelationID: event.CorrelationID, CausationID: event.CausationID, OccurredAt: event.OccurredAt.Time()}
	fingerprint, err := syncengine.LocalMutationFingerprint(mutation)
	if err != nil {
		return eventbus.Permanent("commerce_sync_invalid_payload")
	}
	policies, err := r.policies.ListPolicies(ctx, scope, 200)
	if err != nil {
		return eventbus.Retryable("commerce_sync_policy_read_failed")
	}
	var permanentErr error
	var retryableErr error
	for _, policy := range policies {
		if !policy.Enabled || policy.EntityType != entity || !policy.Direction.AllowsOutbound() {
			continue
		}
		// Check the durable receipt before reading mutable account, mapping or
		// secret state. A redelivery after a successful remote write must remain
		// a no-op even if an operator has since disabled or removed that policy.
		if receipt, receiptErr := r.receipts.LocalReceipt(ctx, scope, policy.ID, event.ID); receiptErr == nil {
			if receipt.Fingerprint != fingerprint {
				return eventbus.Permanent("commerce_sync_receipt_collision")
			}
			continue
		} else if !errors.Is(receiptErr, syncengine.ErrNotFound) {
			return eventbus.Retryable("commerce_sync_receipt_read_failed")
		}
		account, err := r.accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.ConnectorAccountID)
		if errors.Is(err, sdk.ErrAccountNotFound) || (err == nil && account.Status != sdk.AccountActive) {
			continue
		}
		if err != nil {
			return eventbus.Retryable("commerce_sync_account_read_failed")
		}
		readCapability, writeCapability, supported := sdk.RequiredSyncCapabilities(account.Family, entity)
		manifest, manifestErr := sdk.CatalogManifest(account.ConnectorID)
		if !supported || manifestErr != nil || !manifest.Supports(readCapability) || !manifest.Supports(writeCapability) || !r.registry.supportsSync(account, entity, string(policy.Direction)) {
			continue
		}
		settings, err := r.accounts.AccountCapabilities(ctx, scope, account.ID)
		if err != nil {
			return eventbus.Retryable("commerce_sync_capability_read_failed")
		}
		if !sdk.CapabilityEnabled(settings, writeCapability) {
			continue
		}
		if entity == commerceProductsEntity && r.quality != nil {
			if qualityErr := r.quality.CheckProduct(ctx, scope, account, mappingLocalID, localVersion, r.now().UTC()); qualityErr != nil {
				if errors.Is(qualityErr, publicationquality.ErrGateDenied) || errors.Is(qualityErr, publicationquality.ErrReceiptStale) || errors.Is(qualityErr, publicationquality.ErrUnsupported) {
					permanentErr = eventbus.Permanent("commerce_sync_quality_gate_denied")
				} else {
					retryableErr = eventbus.Retryable("commerce_sync_quality_gate_unavailable")
				}
				continue
			}
		}
		mapping, err := r.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, mappingEntityType, mappingLocalID)
		mappingMissing := false
		if errors.Is(err, sdk.ErrMappingNotFound) {
			if !allowCreateMapping {
				permanentErr = eventbus.Permanent("commerce_sync_mapping_missing")
				continue
			}
			mappingMissing = true
		}
		if err != nil {
			return eventbus.Retryable("commerce_sync_mapping_read_failed")
		}
		runtime, err := r.newRuntime(ctx, scope, account)
		if err != nil {
			permanentErr = eventbus.Permanent("commerce_sync_runtime_unavailable")
			continue
		}
		remoteID := ""
		if !mappingMissing {
			remoteID = mapping.RemoteID
		}
		writeReceipt, err := apply(account, runtime, policy, remoteID)
		if err != nil {
			classified := classifyCommerceWriteError(err)
			if classified != nil {
				if class, _ := eventbus.ClassifyFailure(classified); class == eventbus.FailurePermanent {
					permanentErr = classified
				} else {
					retryableErr = classified
				}
			}
			continue
		}
		outcome := syncengine.OutcomeApplied
		if writeReceipt.Duplicate {
			outcome = syncengine.OutcomeDuplicate
		}
		if mappingMissing {
			if _, err := r.mappings.UpsertMapping(ctx, sdk.MappingUpsert{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorAccountID: account.ID, EntityType: mappingEntityType, LocalEntityID: mappingLocalID, RemoteID: writeReceipt.RemoteID, ExpectedVersion: 0}); err != nil {
				if errors.Is(err, sdk.ErrMappingConflict) {
					existing, lookupErr := r.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, mappingEntityType, mappingLocalID)
					if lookupErr == nil && existing.RemoteID == writeReceipt.RemoteID {
						// Another delivery established the same identity while this
						// write was in flight; the remote receipt remains valid.
					} else {
						permanentErr = eventbus.Permanent("commerce_sync_mapping_collision")
						continue
					}
				} else {
					retryableErr = eventbus.Retryable("commerce_sync_mapping_write_failed")
					continue
				}
			}
		}
		// The connector has already validated the receipt. Duplicate receipts are
		// still recorded, which makes replay observable without another call.
		if err := r.recordReceipt(ctx, scope, policy, event, mutation, fingerprint, outcome); err != nil {
			if class, _ := eventbus.ClassifyFailure(err); class == eventbus.FailurePermanent {
				permanentErr = err
			} else {
				retryableErr = err
			}
		}
	}
	if retryableErr != nil {
		return retryableErr
	}
	if permanentErr != nil {
		return permanentErr
	}
	return nil
}

func (r *commerceWriteRoute) recordReceipt(ctx context.Context, scope tenancy.Scope, policy syncengine.Policy, event eventbus.Event, mutation syncengine.LocalMutation, fingerprint string, outcome syncengine.Outcome) error {
	if mutation.EventID == "" || fingerprint == "" {
		return eventbus.Permanent("commerce_sync_invalid_receipt")
	}
	if err := r.receipts.RecordLocalReceipt(ctx, scope, syncengine.Receipt{PolicyID: policy.ID, ChangeID: event.ID, Fingerprint: fingerprint, Outcome: outcome, CreatedAt: r.now().UTC()}); err != nil {
		if errors.Is(err, syncengine.ErrReceiptCollision) {
			return eventbus.Permanent("commerce_sync_receipt_collision")
		}
		return eventbus.Retryable("commerce_sync_receipt_write_failed")
	}
	return nil
}

func classifyCommerceWriteError(err error) error {
	if err == nil {
		return nil
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		if remote.Retryable() {
			return eventbus.Retryable("commerce_sync_remote_retryable")
		}
		return eventbus.Permanent("commerce_sync_remote_rejected")
	}
	if errors.Is(err, sdk.ErrInvalidCommerceWrite) {
		return eventbus.Permanent("commerce_sync_invalid_write")
	}
	return eventbus.Retryable("commerce_sync_write_failed")
}

func decodeCommerceEvent(data json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing event data")
	}
	return nil
}

func commerceWriteEntity(typ eventbus.EventType) (string, bool) {
	switch typ.String() {
	case "commerce.catalog.product_changed.v1":
		return commerceProductsEntity, true
	case "commerce.pricing.price_changed.v1":
		return commercePricesEntity, true
	case "commerce.inventory.position_changed.v1":
		return commerceInventoryEntity, true
	case "commerce.orders.order_changed.v1":
		return commerceOrdersEntity, true
	default:
		return "", false
	}
}

func isCanonicalOrderStatus(status string) bool {
	switch status {
	case "pending", "confirmed", "processing", "fulfilled", "cancelled":
		return true
	default:
		return false
	}
}

func commerceSyncIdempotencyKey(policyID, eventID string) string {
	sum := sha256.Sum256([]byte(policyID + "\x00" + eventID))
	return "commercesync_" + hex.EncodeToString(sum[:16])
}

func moneyToMajor(value domain.Money) (string, error) {
	if err := value.Validate(); err != nil || value.MinorUnits() < 0 {
		return "", errors.New("invalid money")
	}
	scale := currencyMinorScale(value.Currency().String())
	minor := strconv.FormatInt(value.MinorUnits(), 10)
	if scale == 0 {
		return minor, nil
	}
	if len(minor) <= scale {
		minor = strings.Repeat("0", scale+1-len(minor)) + minor
	}
	point := len(minor) - scale
	return minor[:point] + "." + minor[point:], nil
}

func currencyMinorScale(currency string) int {
	switch currency {
	case "BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND":
		return 3
	case "BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "MGA", "PYG", "RWF", "UGX", "VND", "VUV", "XAF", "XOF", "XPF":
		return 0
	default:
		// ISO-4217 currencies not listed above use two minor units. The
		// connector still validates the configured store currency exactly.
		return 2
	}
}

func discreteQuantity(value domain.Quantity) (int64, error) {
	if value.Validate() != nil || value.Value.Scale() != 0 || value.Value.Coefficient() < 0 {
		return 0, errors.New("inventory quantity must be a non-negative integer")
	}
	switch value.Unit.String() {
	case "PCS", "EA", "UNIT", "PIECE":
		return value.Value.Coefficient(), nil
	default:
		return 0, fmt.Errorf("unsupported inventory unit %s", value.Unit)
	}
}
