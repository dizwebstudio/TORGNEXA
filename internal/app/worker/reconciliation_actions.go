package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	coreinventory "github.com/torgnexa/torgnexa/internal/core/inventory"
	coreorders "github.com/torgnexa/torgnexa/internal/core/orders"
	corepricing "github.com/torgnexa/torgnexa/internal/core/pricing"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	builtins "github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/approvalrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectormaprepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/notificationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/ordersrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pricingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

type reconciliationActionExecutor struct {
	syncRepo      syncengine.Repository
	accounts      *connectorrepo.Repository
	mappings      *connectormaprepo.Repository
	catalog       *catalogrepo.Repository
	orders        *ordersrepo.Repository
	prices        *pricingrepo.Repository
	inventory     *inventoryrepo.Repository
	approvals     *approvalrepo.Repository
	notifications *notificationrepo.Repository
	secrets       secrets.SecretProvider
	oauthRefresh  connectorauth.RefreshCoordinator
	registry      *runtimeRegistry
}

func newReconciliationActionExecutor(syncRepo syncengine.Repository, accounts *connectorrepo.Repository, mappings *connectormaprepo.Repository, catalogRepository *catalogrepo.Repository, orderRepository *ordersrepo.Repository, priceRepository *pricingrepo.Repository, inventoryRepository *inventoryrepo.Repository, approvals *approvalrepo.Repository, notificationRepository *notificationrepo.Repository, secretSource secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry) (*reconciliationActionExecutor, error) {
	if syncRepo == nil || accounts == nil || mappings == nil || catalogRepository == nil || orderRepository == nil || priceRepository == nil || inventoryRepository == nil || approvals == nil || notificationRepository == nil || secretSource == nil || refreshCoordinator == nil || registry == nil {
		return nil, errors.New("worker: reconciliation action dependencies required")
	}
	return &reconciliationActionExecutor{syncRepo: syncRepo, accounts: accounts, mappings: mappings, catalog: catalogRepository, orders: orderRepository, prices: priceRepository, inventory: inventoryRepository, approvals: approvals, notifications: notificationRepository, secrets: secretSource, oauthRefresh: refreshCoordinator, registry: registry}, nil
}

type actionContext struct {
	policy  syncengine.Policy
	account sdk.Account
	runtime sdk.Runtime
}

func (e *reconciliationActionExecutor) resolve(ctx context.Context, scope tenancy.Scope, policyID string) (actionContext, error) {
	p, err := e.syncRepo.Policy(ctx, scope, policyID)
	if err != nil {
		return actionContext{}, err
	}
	a, err := e.accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), p.ConnectorAccountID)
	if err != nil {
		return actionContext{}, err
	}
	runtime, err := connectorruntime.NewForAccount(e.secrets, e.oauthRefresh, scope, a)
	if err != nil {
		return actionContext{}, err
	}
	return actionContext{policy: p, account: a, runtime: runtime}, nil
}

func (e *reconciliationActionExecutor) AutoFix(ctx context.Context, scope tenancy.Scope, req reconciliation.RepairRequest) error {
	if ctx == nil || !scope.Valid() || req.IdempotencyKey == "" {
		return reconciliation.ErrInvalid
	}
	ac, err := e.resolve(ctx, scope, req.Drift.PolicyID)
	if err != nil {
		return err
	}
	entity := trimEntity(ac.policy.EntityType)
	if entity != "product" && entity != "order" && entity != "price" && entity != "inventory" {
		return reconciliation.ErrActionUnavailable
	}
	switch req.Direction {
	case reconciliation.RepairCreateMapping:
		if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
			return reconciliation.ErrActionUnsafe
		}
		_, err = e.mappings.UpsertMapping(ctx, sdk.MappingUpsert{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorAccountID: ac.account.ID, EntityType: entity, LocalEntityID: req.Drift.LocalEntityID, RemoteID: req.Drift.RemoteID, ExpectedVersion: 0})
		if errors.Is(err, sdk.ErrMappingConflict) {
			existing, lookup := e.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), ac.account.ID, entity, req.Drift.LocalEntityID)
			if lookup == nil && existing.RemoteID == req.Drift.RemoteID {
				return nil
			}
		}
		return err
	case reconciliation.RepairRemoteToLocal:
		switch entity {
		case "order":
			return e.autoFixOrderRemoteToLocal(ctx, scope, ac, req)
		case "price":
			return e.autoFixPriceRemoteToLocal(ctx, scope, ac, req)
		case "inventory":
			return e.autoFixInventoryRemoteToLocal(ctx, scope, ac, req)
		}
		remote, err := e.findRemoteProduct(ctx, scope, ac, req.Drift.RemoteID)
		if err != nil {
			return err
		}
		coreScope, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		pid, err := catalog.ParseProductID(req.Drift.LocalEntityID)
		if err != nil {
			return err
		}
		current, err := e.catalog.Product(ctx, coreScope, pid)
		if err != nil {
			return err
		}
		mutation := catalog.Mutation{EventID: stableID("evt_rec_", req.IdempotencyKey), OccurredAt: time.Now().UTC(), Source: "worker.reconciliation", CorrelationID: stableID("corr_rec_", req.IdempotencyKey), CausationID: req.Drift.ID}
		switch req.Drift.Kind {
		case reconciliation.DriftContent:
			if current.Title == remote.Title {
				return nil
			}
			_, err = e.catalog.UpdateProduct(ctx, coreScope, catalog.UpdateProduct{ID: pid, ExpectedVersion: current.Version, Title: remote.Title, Description: current.Description}, mutation)
			return err
		case reconciliation.DriftStatusMismatch:
			target, ok := catalogStatus(remote.Status)
			if !ok {
				return reconciliation.ErrActionUnsafe
			}
			if current.Status == target {
				return nil
			}
			_, err = e.catalog.ChangeProductStatus(ctx, coreScope, catalog.ChangeProductStatus{ID: pid, ExpectedVersion: current.Version, Status: target}, mutation)
			return err
		default:
			return reconciliation.ErrActionUnsafe
		}
	case reconciliation.RepairLocalToRemote:
		switch entity {
		case "order":
			return e.autoFixOrderLocalToRemote(ctx, scope, ac, req)
		case "price":
			return e.autoFixPriceLocalToRemote(ctx, scope, ac, req)
		case "inventory":
			return e.autoFixInventoryLocalToRemote(ctx, scope, ac, req)
		}
		writer, err := e.registry.productWriter(scope, ac.account, ac.runtime)
		if err != nil {
			return err
		}
		coreScope, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		pid, err := catalog.ParseProductID(req.Drift.LocalEntityID)
		if err != nil {
			return err
		}
		current, err := e.catalog.Product(ctx, coreScope, pid)
		if err != nil {
			return err
		}
		status, ok := e.registry.productStatus(ac.account, current.Status)
		if !ok {
			return reconciliation.ErrActionUnsafe
		}
		_, err = writer.UpsertProduct(ctx, ac.account, ac.runtime, sdk.ProductWriteRequest{RemoteID: req.Drift.RemoteID, SellerSKU: current.Code, Title: current.Title, Description: current.Description, StatusRemoteID: status, IdempotencyKey: req.IdempotencyKey})
		return err
	default:
		return reconciliation.ErrActionUnsafe
	}
}

func (e *reconciliationActionExecutor) autoFixOrderRemoteToLocal(ctx context.Context, scope tenancy.Scope, ac actionContext, req reconciliation.RepairRequest) error {
	if req.Drift.LocalEntityID == "" || req.Drift.RemoteStatus == "" {
		return reconciliation.ErrActionUnsafe
	}
	orderScope, err := coreorders.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	orderID, err := coreorders.ParseOrderID(req.Drift.LocalEntityID)
	if err != nil {
		return err
	}
	current, err := e.orders.Order(ctx, orderScope, orderID)
	if err != nil {
		return err
	}
	target := coreorders.Status(req.Drift.RemoteStatus)
	if current.Status == target {
		return nil
	}
	_, err = e.orders.ChangeStatus(ctx, orderScope, coreorders.ChangeStatus{ID: orderID, ExpectedVersion: current.Version, Status: target}, orderMutation(req.IdempotencyKey, req.Drift.ID))
	return err
}

func (e *reconciliationActionExecutor) autoFixOrderLocalToRemote(ctx context.Context, scope tenancy.Scope, ac actionContext, req reconciliation.RepairRequest) error {
	if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
		return reconciliation.ErrActionUnsafe
	}
	orderScope, err := coreorders.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	orderID, err := coreorders.ParseOrderID(req.Drift.LocalEntityID)
	if err != nil {
		return err
	}
	current, err := e.orders.Order(ctx, orderScope, orderID)
	if err != nil {
		return err
	}
	status, ok := e.registry.orderStatus(scope, ac.account, string(current.Status))
	if !ok {
		return reconciliation.ErrActionUnsafe
	}
	writer, err := e.registry.orderStatusWriter(scope, ac.account, ac.runtime)
	if err != nil {
		return err
	}
	_, err = writer.WriteOrderStatus(ctx, ac.account, ac.runtime, sdk.OrderStatusWriteRequest{OrderRemoteID: req.Drift.RemoteID, StatusRemoteID: status, IdempotencyKey: req.IdempotencyKey})
	return err
}

func (e *reconciliationActionExecutor) autoFixPriceRemoteToLocal(ctx context.Context, scope tenancy.Scope, ac actionContext, req reconciliation.RepairRequest) error {
	if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
		return reconciliation.ErrActionUnsafe
	}
	offerID, err := corepricing.ParseOfferID(req.Drift.LocalEntityID)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	remote, err := e.findRemotePrice(ctx, scope, ac, req.Drift.RemoteID)
	if err != nil {
		return err
	}
	currency, err := corepricing.NewCurrency(remote.Currency)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	minorUnits, err := remotePriceToMinor(remote.Value, remote.Currency)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	amount, err := corepricing.NewMoney(minorUnits, currency)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	priceScope, err := corepricing.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	prices, err := e.prices.PricesByOffer(ctx, priceScope, offerID, 100)
	if err != nil {
		return err
	}
	for _, current := range prices {
		if current.Kind != corepricing.KindRegular || current.Amount.Currency() != currency {
			continue
		}
		if current.Amount.MinorUnits() == amount.MinorUnits() {
			return nil
		}
		_, err = e.prices.Update(ctx, priceScope, corepricing.UpdatePrice{ID: current.ID, ExpectedVersion: current.Version, Amount: amount}, priceMutation(req.IdempotencyKey, req.Drift.ID))
		return err
	}
	// A price can be absent after a remote-only import. Creating it is safe only
	// for the already mapped offer and uses a deterministic identity for replay.
	_, err = e.prices.Create(ctx, priceScope, corepricing.CreatePrice{ID: corepricing.PriceID(stableUUID("price:" + req.Drift.PolicyID + ":" + req.Drift.RemoteID + ":" + remote.Currency)), OfferID: offerID, Kind: corepricing.KindRegular, Amount: amount}, priceMutation(req.IdempotencyKey, req.Drift.ID))
	return err
}

func (e *reconciliationActionExecutor) autoFixPriceLocalToRemote(ctx context.Context, scope tenancy.Scope, ac actionContext, req reconciliation.RepairRequest) error {
	if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
		return reconciliation.ErrActionUnsafe
	}
	offerID, err := corepricing.ParseOfferID(req.Drift.LocalEntityID)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	remote, err := e.findRemotePrice(ctx, scope, ac, req.Drift.RemoteID)
	if err != nil {
		return err
	}
	priceScope, err := corepricing.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	prices, err := e.prices.PricesByOffer(ctx, priceScope, offerID, 100)
	if err != nil {
		return err
	}
	for _, current := range prices {
		if current.Kind != corepricing.KindRegular || current.Amount.Currency().String() != remote.Currency {
			continue
		}
		writer, err := e.registry.priceWriter(scope, ac.account, ac.runtime)
		if err != nil {
			return err
		}
		value := minorUnitsToMajor(current.Amount.MinorUnits(), current.Amount.Currency().String())
		receipt, err := writer.WritePrice(ctx, ac.account, ac.runtime, sdk.PriceWriteRequest{VariantRemoteID: req.Drift.RemoteID, Value: value, Currency: current.Amount.Currency().String(), IdempotencyKey: req.IdempotencyKey})
		if err != nil {
			return err
		}
		return receipt.Validate()
	}
	return reconciliation.ErrActionUnsafe
}

func (e *reconciliationActionExecutor) autoFixInventoryRemoteToLocal(ctx context.Context, scope tenancy.Scope, ac actionContext, req reconciliation.RepairRequest) error {
	if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
		return reconciliation.ErrActionUnsafe
	}
	locationID, variantID, ok := splitInventoryRemoteID(req.Drift.RemoteID)
	if !ok {
		return reconciliation.ErrActionUnsafe
	}
	offerMapping, err := e.mappings.MappingByRemote(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), ac.account.ID, "offer", variantID)
	if err != nil || offerMapping.LocalEntityID == "" {
		return reconciliation.ErrActionUnsafe
	}
	warehouseMapping, err := e.mappings.MappingByRemote(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), ac.account.ID, "warehouse", locationID)
	if err != nil || warehouseMapping.LocalEntityID == "" {
		return reconciliation.ErrActionUnsafe
	}
	positionID := coreinventory.PositionID(req.Drift.LocalEntityID)
	if !positionID.Valid() {
		return reconciliation.ErrActionUnsafe
	}
	inventoryScope, err := coreinventory.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	current, err := e.inventory.Position(ctx, inventoryScope, positionID)
	if err != nil || current.OfferID.String() != offerMapping.LocalEntityID || current.WarehouseID.String() != warehouseMapping.LocalEntityID {
		return reconciliation.ErrActionUnsafe
	}
	remote, err := e.findRemoteInventory(ctx, scope, ac, locationID, variantID)
	if err != nil {
		return err
	}
	available, err := coreinventory.NewDecimal(remote.Quantity, 0)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	reserved, err := current.Reserved.Value.Add(available)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	onHand, err := coreinventory.NewQuantity(reserved, current.OnHand.Unit)
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	currentAvailable, err := current.Available()
	if err != nil {
		return reconciliation.ErrActionUnsafe
	}
	if cmp, _ := currentAvailable.Value.Cmp(available); cmp == 0 {
		return nil
	}
	_, err = e.inventory.SetOnHand(ctx, inventoryScope, coreinventory.ChangeQuantity{ID: positionID, ExpectedVersion: current.Version, Quantity: onHand, Reason: "remote_sync"}, inventoryMutation(req.IdempotencyKey, req.Drift.ID))
	return err
}

func (e *reconciliationActionExecutor) autoFixInventoryLocalToRemote(ctx context.Context, scope tenancy.Scope, ac actionContext, req reconciliation.RepairRequest) error {
	if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
		return reconciliation.ErrActionUnsafe
	}
	positionID := coreinventory.PositionID(req.Drift.LocalEntityID)
	if !positionID.Valid() {
		return reconciliation.ErrActionUnsafe
	}
	locationID, variantID, ok := splitInventoryRemoteID(req.Drift.RemoteID)
	if !ok {
		return reconciliation.ErrActionUnsafe
	}
	inventoryScope, err := coreinventory.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	current, err := e.inventory.Position(ctx, inventoryScope, positionID)
	if err != nil {
		return err
	}
	available, err := current.Available()
	if err != nil || available.Value.Scale() != 0 || available.Value.Coefficient() < 0 {
		return reconciliation.ErrActionUnsafe
	}
	writer, err := e.registry.inventoryWriter(scope, ac.account, ac.runtime)
	if err != nil {
		return err
	}
	receipt, err := writer.WriteInventory(ctx, ac.account, ac.runtime, sdk.InventoryWriteRequest{VariantRemoteID: variantID, LocationRemoteID: locationID, Quantity: available.Value.Coefficient(), IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return err
	}
	return receipt.Validate()
}

func (e *reconciliationActionExecutor) findRemotePrice(ctx context.Context, scope tenancy.Scope, ac actionContext, remoteID string) (sdk.RemotePrice, error) {
	reader, err := e.registry.priceReader(scope, ac.account, ac.runtime)
	if err != nil {
		return sdk.RemotePrice{}, err
	}
	cursor := ""
	for pageNo := 0; pageNo < 100; pageNo++ {
		page, err := reader.ReadPrices(ctx, ac.account, ac.runtime, sdk.PageRequest{Cursor: cursor, Limit: 100})
		if err != nil {
			return sdk.RemotePrice{}, err
		}
		for _, item := range page.Items {
			if item.VariantRemoteID == remoteID {
				return item, nil
			}
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			return sdk.RemotePrice{}, errors.New("worker: remote price cursor did not advance")
		}
		cursor = page.NextCursor
	}
	return sdk.RemotePrice{}, errors.New("worker: remote price not found")
}

func (e *reconciliationActionExecutor) findRemoteInventory(ctx context.Context, scope tenancy.Scope, ac actionContext, locationID, variantID string) (sdk.RemoteInventory, error) {
	reader, err := e.registry.inventoryReader(scope, ac.account, ac.runtime)
	if err != nil {
		return sdk.RemoteInventory{}, err
	}
	values, err := reader.ReadInventory(ctx, ac.account, ac.runtime, sdk.InventoryQuery{LocationRemoteID: locationID, VariantRemoteIDs: []string{variantID}})
	if err != nil {
		return sdk.RemoteInventory{}, err
	}
	for _, item := range values {
		if item.LocationRemoteID == locationID && item.VariantRemoteID == variantID {
			return item, nil
		}
	}
	return sdk.RemoteInventory{}, errors.New("worker: remote inventory not found")
}

func priceMutation(key, causation string) corepricing.Mutation {
	return corepricing.Mutation{AuditID: stableUUID("audit:" + key), EventID: stableID("evt_rec_", key), ActorID: "system:reconciliation", Source: "worker.reconciliation", CorrelationID: stableID("corr_rec_", key), CausationID: causation, OccurredAt: time.Now().UTC()}
}

func inventoryMutation(key, causation string) coreinventory.Mutation {
	return coreinventory.Mutation{AuditID: stableUUID("audit:" + key), EventID: stableID("evt_rec_", key), ActorID: "system:reconciliation", Source: "worker.reconciliation", CorrelationID: stableID("corr_rec_", key), CausationID: causation, OccurredAt: time.Now().UTC()}
}

func remotePriceToMinor(value, currency string) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts) == 0 || parts[0] == "" {
		return 0, errors.New("invalid remote price")
	}
	scale := currencyMinorScale(currency)
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > scale {
		if strings.Trim(frac[scale:], "0") != "" {
			return 0, errors.New("remote price has unsupported precision")
		}
		frac = frac[:scale]
	}
	frac += strings.Repeat("0", scale-len(frac))
	combined := strings.TrimLeft(parts[0]+frac, "0")
	if combined == "" {
		return 0, nil
	}
	valueInt := new(big.Int)
	if _, ok := valueInt.SetString(combined, 10); !ok || !valueInt.IsInt64() {
		return 0, errors.New("remote price overflows canonical money")
	}
	return valueInt.Int64(), nil
}

func orderMutation(key, causation string) coreorders.Mutation {
	return coreorders.Mutation{AuditID: stableUUID("audit:" + key), EventID: stableID("evt_rec_", key), ActorID: "system:reconciliation", Source: "worker.reconciliation", CorrelationID: stableID("corr_rec_", key), CausationID: causation, OccurredAt: time.Now().UTC()}
}

func (e *reconciliationActionExecutor) findRemoteProduct(ctx context.Context, scope tenancy.Scope, ac actionContext, remoteID string) (builtins.Product, error) {
	reader, err := e.registry.productReader(scope, ac.account, ac.runtime)
	if err != nil {
		return builtins.Product{}, err
	}
	cursor := ""
	for pageNo := 0; pageNo < 100; pageNo++ {
		page, err := reader.Read(ctx, sdk.PageRequest{Cursor: cursor, Limit: 1000})
		if err != nil {
			return builtins.Product{}, err
		}
		for _, item := range page.Items {
			if item.ID == remoteID {
				return item, nil
			}
		}
		if page.NextCursor == "" {
			return builtins.Product{}, errors.New("worker: remote product not found")
		}
		if page.NextCursor == cursor {
			return builtins.Product{}, errors.New("worker: remote cursor did not advance")
		}
		cursor = page.NextCursor
	}
	return builtins.Product{}, errors.New("worker: remote product lookup page limit exceeded")
}

func (e *reconciliationActionExecutor) Notify(ctx context.Context, scope tenancy.Scope, req reconciliation.NotifyRequest) error {
	if ctx == nil || !scope.Valid() || req.IdempotencyKey == "" {
		return reconciliation.ErrInvalid
	}
	// Persist a workspace-level operator notification. The reserved recipient is
	// also useful to webhook/projector consumers; per-user fan-out remains a P1 delivery concern.
	now := time.Now().UTC()
	n := notifications.Notification{ID: stableID("ntf_rec_", req.IdempotencyKey), RecipientID: "workspace-operators", DedupeKey: req.IdempotencyKey, Severity: notifications.SeverityWarning, Title: "Reconciliation requires attention", Body: fmt.Sprintf("Drift %s (%s) was detected for policy %s.", req.Drift.ID, req.Drift.Kind, req.Drift.PolicyID), EntityType: "reconciliation_drift", EntityID: req.Drift.ID, OccurrenceCount: 1, FirstOccurredAt: now, LastOccurredAt: now, CreatedAt: now, UpdatedAt: now}
	_, _, err := e.notifications.Upsert(ctx, scope, n)
	return err
}

func (e *reconciliationActionExecutor) RequestApproval(ctx context.Context, scope tenancy.Scope, req reconciliation.ApprovalRequest) error {
	if ctx == nil || !scope.Valid() || req.IdempotencyKey == "" {
		return reconciliation.ErrInvalid
	}
	requestID := stableUUID("approval:" + req.IdempotencyKey)
	if existing, err := e.approvals.Request(ctx, scope, requestID); err == nil && existing.ID == requestID {
		return nil
	}
	now := time.Now().UTC()
	mutation := approval.Mutation{AuditID: stableUUID("audit:" + req.IdempotencyKey), EventID: stableUUID("event:" + req.IdempotencyKey), ActorID: "system:reconciliation", Source: "worker.reconciliation", CorrelationID: stableUUID("correlation:" + req.IdempotencyKey), CausationID: req.Drift.ID, OccurredAt: now}
	_, err := e.approvals.CreateRequest(ctx, scope, "reconciliation.repair", "reconciliation_drift", approval.RequestCommand{RequestID: requestID, ResourceID: req.Drift.ID, Risk: approval.RiskWriteSensitive, Mutation: mutation})
	return err
}

func actionPolicyFor(policy syncengine.Policy, outboundWritable bool) reconciliation.ActionPolicy {
	result := reconciliation.ActionPolicy{MissingMapping: reconciliation.ActionAutoFix, OrphanMapping: reconciliation.ActionNotify, DuplicateMapping: reconciliation.ActionNotify, StaleConnector: reconciliation.ActionNotify}
	switch policy.SourceOfTruth {
	case syncengine.SourceRemote:
		if policy.Direction.AllowsInbound() {
			result.Content = reconciliation.ActionAutoFix
			result.StatusMismatch = reconciliation.ActionAutoFix
		} else {
			result.Content = reconciliation.ActionApproval
			result.StatusMismatch = reconciliation.ActionApproval
		}
	case syncengine.SourceLocal:
		if policy.Direction.AllowsOutbound() && outboundWritable {
			result.Content = reconciliation.ActionAutoFix
			result.StatusMismatch = reconciliation.ActionAutoFix
		} else {
			// Do not create approvals for an operation that the runtime cannot
			// execute. Persist operator-visible evidence and finish the run instead
			// of creating an infinite approval/retry loop.
			result.Content = reconciliation.ActionNotify
			result.StatusMismatch = reconciliation.ActionNotify
		}
	default:
		result.Content = reconciliation.ActionApproval
		result.StatusMismatch = reconciliation.ActionApproval
	}
	return result
}

func catalogStatus(remote string) (catalog.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(remote)) {
	case "active", "publish", "published":
		return catalog.StatusActive, true
	case "draft":
		return catalog.StatusDraft, true
	case "archived", "archive", "private":
		return catalog.StatusArchived, true
	default:
		return "", false
	}
}

func trimEntity(v string) string {
	if len(v) > 1 && v[len(v)-1] == 's' {
		return v[:len(v)-1]
	}
	return v
}
func stableID(prefix, key string) string {
	sum := sha256.Sum256([]byte(key))
	return prefix + hex.EncodeToString(sum[:16])
}

func stableUUID(key string) string {
	sum := sha256.Sum256([]byte(key))
	b := append([]byte(nil), sum[:16]...)
	// RFC 4122-shaped deterministic UUID. The bytes are derived solely from
	// the idempotency key, so retries resolve to exactly the same evidence IDs.
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

var _ reconciliation.ActionExecutor = (*reconciliationActionExecutor)(nil)
