package worker

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/core/pricing"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	builtins "github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	runtimeconfigstore "github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

var ErrConnectorEntityUnsupported = errors.New("worker: connector reconciliation entity unsupported")

type runtimeRegistry struct {
	database *sql.DB
	configs  *runtimeconfigstore.Repository
	builtins *builtins.Registry
}

func newRuntimeRegistry(database *sql.DB) (*runtimeRegistry, error) {
	if database == nil {
		return nil, errors.New("worker: runtime registry database required")
	}
	configs, err := runtimeconfigstore.New(database)
	if err != nil {
		return nil, err
	}
	return &runtimeRegistry{database: database, configs: configs, builtins: builtins.New()}, nil
}

func (registry *runtimeRegistry) Source(ctx context.Context, scope tenancy.Scope, policy syncengine.Policy, account sdk.Account, manifest sdk.Manifest, runtime sdk.Runtime) (reconciliation.Source, error) {
	if registry == nil || registry.database == nil || registry.configs == nil || registry.builtins == nil || !scope.Valid() || policy.Validate() != nil || account.Validate() != nil || manifest.Validate() != nil || runtime == nil {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	entity := strings.TrimSuffix(policy.EntityType, "s")
	readiness, readinessErr := sdk.ReadinessProfileFor(account.ConnectorID)
	if readinessErr != nil || (readiness.Status != sdk.ReadinessReady && readiness.Status != sdk.ReadinessQualified) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	if entity == "order" {
		reader, err := registry.orderReader(scope, account, runtime)
		if err != nil {
			return nil, err
		}
		return &orderReconciliationSource{database: registry.database, account: account, reader: reader, now: func() time.Time { return time.Now().UTC() }}, nil
	}
	if entity == "price" {
		reader, err := registry.priceReader(scope, account, runtime)
		if err != nil {
			return nil, err
		}
		return &priceReconciliationSource{database: registry.database, account: account, runtime: runtime, reader: reader, now: func() time.Time { return time.Now().UTC() }}, nil
	}
	if entity == "inventory" {
		reader, err := registry.inventoryReader(scope, account, runtime)
		if err != nil {
			return nil, err
		}
		return &inventoryReconciliationSource{database: registry.database, account: account, runtime: runtime, reader: reader, now: func() time.Time { return time.Now().UTC() }}, nil
	}
	if entity != "product" {
		return nil, fmt.Errorf("%w: %s", ErrConnectorEntityUnsupported, policy.EntityType)
	}

	reader, err := registry.productReader(scope, account, runtime)
	if err != nil {
		return nil, err
	}
	return &productReconciliationSource{database: registry.database, account: account, reader: reader, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (registry *runtimeRegistry) priceReader(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (builtins.PriceReader, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	reader, err := registry.builtins.PriceReader(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return reader, err
}

func (registry *runtimeRegistry) inventoryReader(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (builtins.InventoryReader, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	reader, err := registry.builtins.InventoryReader(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return reader, err
}

func (registry *runtimeRegistry) paymentGateway(scope tenancy.Scope, account sdk.Account) (builtins.PaymentGateway, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	gateway, err := registry.builtins.PaymentGateway(account, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return gateway, err
}

func (registry *runtimeRegistry) configLoader(scope tenancy.Scope) builtins.ConfigLoader {
	return func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, err := registry.configs.Config(ctx, scope, accountID)
		return raw, err
	}
}

func (registry *runtimeRegistry) productReader(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (builtins.ProductReader, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	reader, err := registry.builtins.ProductReader(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return reader, err
}

func (registry *runtimeRegistry) orderReader(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (builtins.OrderReader, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	reader, err := registry.builtins.OrderReader(context.Background(), account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return reader, err
}

func (registry *runtimeRegistry) productWriter(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.ProductWriter, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, reconciliation.ErrActionUnavailable
	}
	writer, err := registry.builtins.ProductWriter(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, reconciliation.ErrActionUnavailable
	}
	return writer, err
}

func (registry *runtimeRegistry) productStatus(account sdk.Account, status catalog.Status) (string, bool) {
	if registry == nil || registry.builtins == nil {
		return "", false
	}
	return registry.builtins.ProductStatus(account.ConnectorID, string(status))
}

func (registry *runtimeRegistry) supportsProductWrite(account sdk.Account) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsProductWrite(account)
}

func (registry *runtimeRegistry) priceWriter(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.PriceWriter, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, reconciliation.ErrActionUnavailable
	}
	writer, err := registry.builtins.PriceWriter(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, reconciliation.ErrActionUnavailable
	}
	return writer, err
}

func (registry *runtimeRegistry) supportsPriceWrite(account sdk.Account) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsPriceWrite(account)
}

func (registry *runtimeRegistry) inventoryWriter(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.InventoryWriter, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, reconciliation.ErrActionUnavailable
	}
	writer, err := registry.builtins.InventoryWriter(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, reconciliation.ErrActionUnavailable
	}
	return writer, err
}

func (registry *runtimeRegistry) orderStatusWriter(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.OrderStatusWriter, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, reconciliation.ErrActionUnavailable
	}
	writer, err := registry.builtins.OrderStatusWriter(context.Background(), account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, reconciliation.ErrActionUnavailable
	}
	return writer, err
}

func (registry *runtimeRegistry) logisticsCanceler(ctx context.Context, scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.LogisticsShipmentCanceler, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	canceler, err := registry.builtins.LogisticsCanceler(ctx, account, runtime)
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return canceler, err
}

func (registry *runtimeRegistry) logisticsCreator(ctx context.Context, scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.LogisticsShipmentCreator, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	creator, err := registry.builtins.LogisticsCreator(ctx, account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return creator, err
}

func (registry *runtimeRegistry) logisticsReturnCreator(ctx context.Context, scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.LogisticsReturnCreator, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	creator, err := registry.builtins.LogisticsReturnCreator(ctx, account, runtime)
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return creator, err
}

func (registry *runtimeRegistry) orderStatus(scope tenancy.Scope, account sdk.Account, status string) (string, bool) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return "", false
	}
	return registry.builtins.OrderStatus(context.Background(), account, status, registry.configLoader(scope))
}

func (registry *runtimeRegistry) supportsInventoryWrite(account sdk.Account) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsInventoryWrite(account)
}

func (registry *runtimeRegistry) supportsOrderStatusWrite(account sdk.Account) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsOrderStatusWrite(account)
}

func (registry *runtimeRegistry) supportsWriteForEntity(account sdk.Account, entity string) bool {
	switch strings.TrimSuffix(entity, "s") {
	case "product":
		return registry.supportsProductWrite(account)
	case "order":
		return registry.supportsOrderStatusWrite(account)
	case "price":
		return registry.supportsPriceWrite(account)
	case "inventory":
		return registry.supportsInventoryWrite(account)
	default:
		return false
	}
}

func (registry *runtimeRegistry) supportsSync(account sdk.Account, entityType, direction string) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsSync(account.ConnectorID, entityType, direction)
}

func (registry *runtimeRegistry) socialPublisher(scope tenancy.Scope, account sdk.Account) (sdk.SocialPublisher, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	publisher, err := registry.builtins.SocialPublisher(account, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return publisher, err
}

type productReconciliationSource struct {
	database *sql.DB
	account  sdk.Account
	reader   builtins.ProductReader
	now      func() time.Time
}

type orderReconciliationSource struct {
	database *sql.DB
	account  sdk.Account
	reader   builtins.OrderReader
	now      func() time.Time
}

type priceReconciliationSource struct {
	database *sql.DB
	account  sdk.Account
	runtime  sdk.Runtime
	reader   builtins.PriceReader
	now      func() time.Time
}

type inventoryReconciliationSource struct {
	database *sql.DB
	account  sdk.Account
	runtime  sdk.Runtime
	reader   builtins.InventoryReader
	now      func() time.Time
}

// Scan reads the provider's regular price projection and compares it with the
// canonical price attached to the mapped offer. Price mappings deliberately
// stay offer-scoped: a provider variant is the remote identity of a local
// offer, while price rows remain a separate canonical aggregate.
func (source *priceReconciliationSource) Scan(ctx context.Context, scope tenancy.Scope, req reconciliation.ScanRequest) (reconciliation.ScanPage, error) {
	if source == nil || source.database == nil || source.reader == nil || source.runtime == nil || source.now == nil || !scope.Valid() || req.Validate() != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	limit := req.Limit
	if limit > 100 {
		limit = 100
	}
	page, err := source.reader.ReadPrices(ctx, source.account, source.runtime, sdk.PageRequest{Cursor: req.Cursor, Limit: limit})
	if err != nil {
		return reconciliation.ScanPage{}, err
	}
	if page.Validate(limit) != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	result := reconciliation.ScanPage{NextCursor: page.NextCursor, HasMore: page.NextCursor != "", RemoteObservedAt: source.now().UTC(), Subjects: make([]reconciliation.Subject, 0, len(page.Items))}
	for _, remote := range page.Items {
		subject, subjectErr := source.priceSubject(ctx, scope, remote)
		if subjectErr != nil {
			return reconciliation.ScanPage{}, subjectErr
		}
		result.Subjects = append(result.Subjects, subject)
		if remote.UpdatedAt.After(result.RemoteObservedAt) {
			result.RemoteObservedAt = remote.UpdatedAt
		}
	}
	if result.Validate(req.Limit) != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	return result, nil
}

type localPriceSnapshot struct {
	OfferID    string
	MinorUnits int64
	Currency   string
	Version    int64
}

func (source *priceReconciliationSource) priceSubject(ctx context.Context, scope tenancy.Scope, remote sdk.RemotePrice) (reconciliation.Subject, error) {
	localOfferID, mapped, err := source.mappingByRemote(ctx, scope, "offer", remote.VariantRemoteID)
	if err != nil {
		return reconciliation.Subject{}, err
	}
	subject := reconciliation.Subject{
		RemoteID: remote.VariantRemoteID, RemotePresent: true,
		RemoteFingerprint: priceFingerprint(remote.Value, remote.Currency),
		RemoteRevision:    remote.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ObservedAt:        source.now().UTC(),
	}
	if !mapped {
		if subject.Validate() != nil {
			return reconciliation.Subject{}, reconciliation.ErrInvalid
		}
		return subject, nil
	}
	subject.MappingLocalCount = 1
	subject.MappingRemoteCount = 1
	local, present, err := source.priceByOffer(ctx, scope, localOfferID, remote.Currency)
	if err != nil {
		return reconciliation.Subject{}, err
	}
	if present {
		subject.LocalEntityID = local.OfferID
		subject.LocalPresent = true
		subject.LocalFingerprint = priceFingerprint(minorUnitsToMajor(local.MinorUnits, local.Currency), local.Currency)
		subject.LocalVersion = local.Version
	}
	if subject.Validate() != nil {
		return reconciliation.Subject{}, reconciliation.ErrInvalid
	}
	return subject, nil
}

func (source *priceReconciliationSource) mappingByRemote(ctx context.Context, scope tenancy.Scope, entity, remoteID string) (string, bool, error) {
	var localID string
	err := readScopedTx(ctx, source.database, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT local_entity_id FROM connector_entity_mappings WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type=$4 AND remote_id=$5`, scope.OrganizationID().String(), scope.WorkspaceID().String(), source.account.ID, entity, remoteID).Scan(&localID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return localID, err == nil, err
}

func (source *priceReconciliationSource) priceByOffer(ctx context.Context, scope tenancy.Scope, offerID, currency string) (localPriceSnapshot, bool, error) {
	var result localPriceSnapshot
	err := readScopedTx(ctx, source.database, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT offer_id,minor_units,currency,version FROM prices WHERE organization_id=$1 AND workspace_id=$2 AND offer_id=$3 AND kind='regular' AND currency=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), offerID, currency).Scan(&result.OfferID, &result.MinorUnits, &result.Currency, &result.Version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return localPriceSnapshot{}, false, nil
	}
	if err != nil {
		return localPriceSnapshot{}, false, err
	}
	code, codeErr := pricing.NewCurrency(result.Currency)
	if codeErr != nil {
		return localPriceSnapshot{}, false, pricing.ErrInvalidRecord
	}
	if _, moneyErr := pricing.NewMoney(result.MinorUnits, code); moneyErr != nil || result.MinorUnits < 0 || result.Version < 1 {
		return localPriceSnapshot{}, false, pricing.ErrInvalidRecord
	}
	return result, true, nil
}

// Scan discovers mapped warehouses and offers, reads their exact available
// balances, and pages the resulting stable observation set. A remote stock
// row is never treated as a global balance: both warehouse and offer mappings
// must exist before it can affect the canonical position.
func (source *inventoryReconciliationSource) Scan(ctx context.Context, scope tenancy.Scope, req reconciliation.ScanRequest) (reconciliation.ScanPage, error) {
	if source == nil || source.database == nil || source.reader == nil || source.runtime == nil || source.now == nil || !scope.Valid() || req.Validate() != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	offset, err := inventoryCursorOffset(req.Cursor)
	if err != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	mappings, err := source.inventoryMappings(ctx, scope)
	if err != nil {
		return reconciliation.ScanPage{}, err
	}
	observations := make([]inventoryObservation, 0, len(mappings))
	for start := 0; start < len(mappings); {
		end := start + 1000
		if end > len(mappings) {
			end = len(mappings)
		}
		query := sdk.InventoryQuery{LocationRemoteID: mappings[start].LocationRemoteID, VariantRemoteIDs: make([]string, 0, end-start)}
		for _, mapping := range mappings[start:end] {
			if mapping.LocationRemoteID != query.LocationRemoteID {
				break
			}
			query.VariantRemoteIDs = append(query.VariantRemoteIDs, mapping.VariantRemoteID)
		}
		if len(query.VariantRemoteIDs) == 0 {
			start++
			continue
		}
		values, readErr := source.reader.ReadInventory(ctx, source.account, source.runtime, query)
		if readErr != nil {
			return reconciliation.ScanPage{}, readErr
		}
		quantities := make(map[string]int64, len(values))
		for _, value := range values {
			if value.LocationRemoteID != query.LocationRemoteID || value.Validate() != nil {
				return reconciliation.ScanPage{}, reconciliation.ErrInvalid
			}
			if _, duplicate := quantities[value.VariantRemoteID]; duplicate {
				return reconciliation.ScanPage{}, reconciliation.ErrInvalid
			}
			quantities[value.VariantRemoteID] = value.Quantity
		}
		for _, mapping := range mappings[start : start+len(query.VariantRemoteIDs)] {
			observations = append(observations, inventoryObservation{mapping: mapping, remote: sdk.RemoteInventory{LocationRemoteID: mapping.LocationRemoteID, VariantRemoteID: mapping.VariantRemoteID, Quantity: quantities[mapping.VariantRemoteID]}})
		}
		start += len(query.VariantRemoteIDs)
	}
	if offset > len(observations) {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	end := offset + req.Limit
	if end > len(observations) {
		end = len(observations)
	}
	result := reconciliation.ScanPage{RemoteObservedAt: source.now().UTC(), Subjects: make([]reconciliation.Subject, 0, end-offset)}
	for _, observation := range observations[offset:end] {
		subject, subjectErr := source.inventorySubject(ctx, scope, observation)
		if subjectErr != nil {
			return reconciliation.ScanPage{}, subjectErr
		}
		result.Subjects = append(result.Subjects, subject)
	}
	if end < len(observations) {
		result.NextCursor = "inventory:" + strconv.Itoa(end)
		result.HasMore = true
	}
	if result.Validate(req.Limit) != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	return result, nil
}

type inventoryMapping struct {
	OfferID, VariantRemoteID      string
	WarehouseID, LocationRemoteID string
}

type inventoryObservation struct {
	mapping inventoryMapping
	remote  sdk.RemoteInventory
}

func (source *inventoryReconciliationSource) inventoryMappings(ctx context.Context, scope tenancy.Scope) ([]inventoryMapping, error) {
	result := make([]inventoryMapping, 0)
	err := readScopedTx(ctx, source.database, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT offer.local_entity_id,offer.remote_id,warehouse.local_entity_id,warehouse.remote_id FROM connector_entity_mappings offer JOIN connector_entity_mappings warehouse ON warehouse.organization_id=offer.organization_id AND warehouse.workspace_id=offer.workspace_id AND warehouse.connector_account_id=offer.connector_account_id WHERE offer.organization_id=$1 AND offer.workspace_id=$2 AND offer.connector_account_id=$3 AND offer.entity_type='offer' AND warehouse.entity_type='warehouse' ORDER BY warehouse.remote_id,offer.remote_id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), source.account.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var mapping inventoryMapping
			if err := rows.Scan(&mapping.OfferID, &mapping.VariantRemoteID, &mapping.WarehouseID, &mapping.LocationRemoteID); err != nil {
				return err
			}
			result = append(result, mapping)
		}
		return rows.Err()
	})
	return result, err
}

func (source *inventoryReconciliationSource) inventorySubject(ctx context.Context, scope tenancy.Scope, observation inventoryObservation) (reconciliation.Subject, error) {
	position, present, err := source.positionByParents(ctx, scope, observation.mapping.OfferID, observation.mapping.WarehouseID)
	if err != nil {
		return reconciliation.Subject{}, err
	}
	remoteID := inventoryRemoteID(observation.remote.LocationRemoteID, observation.remote.VariantRemoteID)
	subject := reconciliation.Subject{RemoteID: remoteID, RemotePresent: true, RemoteFingerprint: inventoryFingerprint(strconv.FormatInt(observation.remote.Quantity, 10)), RemoteRevision: "inventory-v1", ObservedAt: source.now().UTC(), MappingLocalCount: 1, MappingRemoteCount: 1}
	if present {
		subject.LocalEntityID = position.ID
		subject.LocalPresent = true
		subject.LocalFingerprint = inventoryFingerprint(position.Available)
		subject.LocalVersion = position.Version
	} else {
		subject.MappingLocalCount = 0
		subject.MappingRemoteCount = 0
	}
	if subject.Validate() != nil {
		return reconciliation.Subject{}, reconciliation.ErrInvalid
	}
	return subject, nil
}

type localPositionSnapshot struct {
	ID, Available string
	Version       int64
}

func (source *inventoryReconciliationSource) positionByParents(ctx context.Context, scope tenancy.Scope, offerID, warehouseID string) (localPositionSnapshot, bool, error) {
	var result localPositionSnapshot
	var onCoefficient, reservedCoefficient int64
	var onScale, reservedScale uint8
	var unit string
	err := readScopedTx(ctx, source.database, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND offer_id=$3 AND warehouse_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), offerID, warehouseID).Scan(&result.ID, &onCoefficient, &onScale, &reservedCoefficient, &reservedScale, &unit, &result.Version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return localPositionSnapshot{}, false, nil
	}
	if err != nil {
		return localPositionSnapshot{}, false, err
	}
	onHand, err := inventory.NewDecimal(onCoefficient, onScale)
	if err != nil {
		return localPositionSnapshot{}, false, inventory.ErrInvalidRecord
	}
	reserved, err := inventory.NewDecimal(reservedCoefficient, reservedScale)
	if err != nil {
		return localPositionSnapshot{}, false, inventory.ErrInvalidRecord
	}
	available, err := onHand.Sub(reserved)
	if err != nil {
		return localPositionSnapshot{}, false, inventory.ErrInvalidRecord
	}
	if _, err := inventory.NewUnitCode(unit); err != nil || result.Version < 1 {
		return localPositionSnapshot{}, false, inventory.ErrInvalidRecord
	}
	result.Available = available.String()
	return result, true, nil
}

func inventoryCursorOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if !strings.HasPrefix(cursor, "inventory:") {
		return 0, reconciliation.ErrInvalid
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(cursor, "inventory:"))
	if err != nil || offset < 0 {
		return 0, reconciliation.ErrInvalid
	}
	return offset, nil
}

func inventoryRemoteID(location, variant string) string {
	return "inventory." + hex.EncodeToString([]byte(location)) + "." + hex.EncodeToString([]byte(variant))
}

func splitInventoryRemoteID(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "inventory.") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(value, "inventory."), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	location, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", "", false
	}
	variant, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}
	return string(location), string(variant), true
}

func priceFingerprint(value, currency string) string {
	return digestText("price-v1\x00" + normalizeDecimal(value) + "\x00" + currency)
}

func inventoryFingerprint(value string) string {
	return digestText("inventory-v1\x00" + value)
}

func normalizeDecimal(value string) string {
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	if value == "" {
		return "0"
	}
	return value
}

func minorUnitsToMajor(minor int64, currency string) string {
	scale := currencyMinorScale(currency)
	value := strconv.FormatInt(minor, 10)
	if scale == 0 {
		return value
	}
	if len(value) <= scale {
		value = strings.Repeat("0", scale+1-len(value)) + value
	}
	point := len(value) - scale
	return normalizeDecimal(value[:point] + "." + value[point:])
}

func (source *orderReconciliationSource) Scan(ctx context.Context, scope tenancy.Scope, req reconciliation.ScanRequest) (reconciliation.ScanPage, error) {
	if source == nil || source.database == nil || source.reader == nil || source.now == nil || !scope.Valid() || req.Validate() != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	limit := req.Limit
	if limit > 50 {
		limit = 50
	}
	page, err := source.reader.Read(ctx, sdk.PageRequest{Cursor: req.Cursor, Limit: limit})
	if err != nil {
		return reconciliation.ScanPage{}, err
	}
	result := reconciliation.ScanPage{NextCursor: page.NextCursor, HasMore: page.NextCursor != "", RemoteObservedAt: source.now().UTC(), Subjects: make([]reconciliation.Subject, 0, len(page.Items))}
	for _, remote := range page.Items {
		subject, subjectErr := source.subject(ctx, scope, remote)
		if subjectErr != nil {
			return reconciliation.ScanPage{}, subjectErr
		}
		result.Subjects = append(result.Subjects, subject)
		if remote.UpdatedAt.After(result.RemoteObservedAt) {
			result.RemoteObservedAt = remote.UpdatedAt
		}
	}
	if result.Validate(req.Limit) != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	return result, nil
}

type localOrderSnapshot struct {
	ID, Number, Status string
	Version            int64
}

func (source *orderReconciliationSource) subject(ctx context.Context, scope tenancy.Scope, remote sdk.RemoteOrder) (reconciliation.Subject, error) {
	mappingLocalID, mapped, err := source.mappingByRemote(ctx, scope, remote.RemoteID)
	if err != nil {
		return reconciliation.Subject{}, err
	}
	localPresent := false
	var local localOrderSnapshot
	if mapped {
		local, localPresent, err = source.orderByID(ctx, scope, mappingLocalID)
	} else if remote.ExternalID != "" {
		local, localPresent, err = source.orderByNumber(ctx, scope, remote.ExternalID)
	}
	if err != nil {
		return reconciliation.Subject{}, err
	}
	const fingerprint = "order-status-v1"
	remoteRevision := remote.UpdatedAt.UTC().Format(time.RFC3339Nano)
	subject := reconciliation.Subject{RemoteID: remote.RemoteID, RemotePresent: true, RemoteFingerprint: digestText(fingerprint), RemoteStatus: remote.StatusRemoteID, RemoteRevision: remoteRevision, ObservedAt: source.now().UTC(), CanAutoMap: !mapped && localPresent}
	if mapped {
		subject.LocalEntityID = mappingLocalID
		subject.MappingLocalCount = 1
		subject.MappingRemoteCount = 1
	}
	if localPresent {
		subject.LocalEntityID = local.ID
		subject.LocalPresent = true
		subject.LocalFingerprint = digestText(fingerprint)
		subject.LocalStatus = local.Status
		subject.LocalVersion = local.Version
	}
	if subject.Validate() != nil {
		return reconciliation.Subject{}, reconciliation.ErrInvalid
	}
	return subject, nil
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (source *orderReconciliationSource) mappingByRemote(ctx context.Context, scope tenancy.Scope, remoteID string) (string, bool, error) {
	var id string
	err := source.readTx(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT local_entity_id FROM connector_entity_mappings WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type='order' AND remote_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), source.account.ID, remoteID).Scan(&id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (source *orderReconciliationSource) orderByID(ctx context.Context, scope tenancy.Scope, id string) (localOrderSnapshot, bool, error) {
	return source.orderLookup(ctx, scope, `id=$3`, id)
}

func (source *orderReconciliationSource) orderByNumber(ctx context.Context, scope tenancy.Scope, number string) (localOrderSnapshot, bool, error) {
	return source.orderLookup(ctx, scope, `number=$3`, number)
}

func (source *orderReconciliationSource) orderLookup(ctx context.Context, scope tenancy.Scope, predicate, value string) (localOrderSnapshot, bool, error) {
	var order localOrderSnapshot
	statement := `SELECT id,number,status,version FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND ` + predicate + ` LIMIT 1`
	err := source.readTx(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, statement, scope.OrganizationID().String(), scope.WorkspaceID().String(), value).Scan(&order.ID, &order.Number, &order.Status, &order.Version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return localOrderSnapshot{}, false, nil
	}
	return order, err == nil, err
}

func (source *orderReconciliationSource) readTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, err := source.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var organization, workspace string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organization, &workspace); err != nil {
		return err
	}
	if organization != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (source *productReconciliationSource) Scan(ctx context.Context, scope tenancy.Scope, req reconciliation.ScanRequest) (reconciliation.ScanPage, error) {
	if source == nil || source.database == nil || source.reader == nil || source.now == nil || !scope.Valid() || req.Validate() != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	page, err := source.reader.Read(ctx, sdk.PageRequest{Cursor: req.Cursor, Limit: req.Limit})
	if err != nil {
		return reconciliation.ScanPage{}, err
	}
	result := reconciliation.ScanPage{NextCursor: page.NextCursor, HasMore: page.NextCursor != "", RemoteObservedAt: source.now().UTC(), Subjects: make([]reconciliation.Subject, 0, len(page.Items))}
	for _, remote := range page.Items {
		subject, err := source.subject(ctx, scope, remote)
		if err != nil {
			return reconciliation.ScanPage{}, err
		}
		result.Subjects = append(result.Subjects, subject)
		if remote.ObservedAt.After(result.RemoteObservedAt) {
			result.RemoteObservedAt = remote.ObservedAt
		}
	}
	return result, nil
}

type localProductSnapshot struct {
	ID, Code, Title, Status string
	Version                 int64
}

func (source *productReconciliationSource) subject(ctx context.Context, scope tenancy.Scope, remote builtins.Product) (reconciliation.Subject, error) {
	mappingLocalID, mapped, err := source.mappingByRemote(ctx, scope, remote.ID)
	if err != nil {
		return reconciliation.Subject{}, err
	}
	var local localProductSnapshot
	localPresent := false
	canAutoMap := false
	if mapped {
		local, localPresent, err = source.productByID(ctx, scope, mappingLocalID)
		if err != nil {
			return reconciliation.Subject{}, err
		}
	} else if remote.Code != "" {
		local, localPresent, err = source.productByCode(ctx, scope, remote.Code)
		if err != nil {
			return reconciliation.Subject{}, err
		}
		canAutoMap = localPresent
	}
	subject := reconciliation.Subject{RemoteID: remote.ID, RemotePresent: true, RemoteFingerprint: productFingerprint(remote.Title), RemoteStatus: remote.Status, RemoteRevision: remote.Revision, ObservedAt: source.now().UTC(), CanAutoMap: canAutoMap}
	if mapped {
		subject.LocalEntityID = mappingLocalID
		subject.MappingLocalCount = 1
		subject.MappingRemoteCount = 1
	}
	if localPresent {
		subject.LocalEntityID = local.ID
		subject.LocalPresent = true
		subject.LocalFingerprint = productFingerprint(local.Title)
		subject.LocalStatus = local.Status
		subject.LocalVersion = local.Version
	}
	return subject, nil
}

func productFingerprint(title string) string {
	data, _ := json.Marshal(struct {
		Title string `json:"title"`
	}{strings.TrimSpace(title)})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (source *productReconciliationSource) mappingByRemote(ctx context.Context, scope tenancy.Scope, remoteID string) (string, bool, error) {
	var id string
	err := source.readTx(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT local_entity_id FROM connector_entity_mappings WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type='product' AND remote_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), source.account.ID, remoteID).Scan(&id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (source *productReconciliationSource) productByID(ctx context.Context, scope tenancy.Scope, id string) (localProductSnapshot, bool, error) {
	return source.productLookup(ctx, scope, `id=$3`, id)
}

func (source *productReconciliationSource) productByCode(ctx context.Context, scope tenancy.Scope, code string) (localProductSnapshot, bool, error) {
	return source.productLookup(ctx, scope, `code=$3`, code)
}

func (source *productReconciliationSource) productLookup(ctx context.Context, scope tenancy.Scope, predicate, value string) (localProductSnapshot, bool, error) {
	var product localProductSnapshot
	statement := `SELECT id,code,title,status,version FROM products WHERE organization_id=$1 AND workspace_id=$2 AND ` + predicate + ` LIMIT 1`
	err := source.readTx(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, statement, scope.OrganizationID().String(), scope.WorkspaceID().String(), value).Scan(&product.ID, &product.Code, &product.Title, &product.Status, &product.Version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return localProductSnapshot{}, false, nil
	}
	return product, err == nil, err
}

func (source *productReconciliationSource) readTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, err := source.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return err
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func readScopedTx(ctx context.Context, database *sql.DB, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || database == nil || !scope.Valid() || fn == nil {
		return reconciliation.ErrInvalid
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var organization, workspace string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organization, &workspace); err != nil {
		return err
	}
	if organization != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
