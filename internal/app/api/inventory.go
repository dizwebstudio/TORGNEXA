package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
)

const (
	InventoryPositionsPath   = "/api/v1/inventory/positions"
	InventoryWarehousesPath  = "/api/v1/inventory/warehouses"
	InventoryIncidentsPath   = "/api/v1/inventory/warehouse-incidents"
	InventoryAllocationsPath = "/api/v1/inventory/fulfillment-allocations"
	InventoryImportPath      = "/api/v1/inventory/import"
)

type inventoryAPI struct{ repository *inventoryrepo.Repository }
type warehouseInput struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version int64  `json:"version"`
}
type adjustmentInput struct {
	Quantity string `json:"quantity"`
	Reason   string `json:"reason"`
	Version  int64  `json:"version"`
}
type operationalStateInput struct {
	State   string `json:"state"`
	Reason  string `json:"reason"`
	Version int64  `json:"version"`
}
type failoverRouteInput struct {
	Priority int   `json:"priority"`
	Enabled  bool  `json:"enabled"`
	Version  int64 `json:"version"`
}
type fulfillmentAllocationInput struct {
	OrderItemID string `json:"order_item_id"`
	WarehouseID string `json:"warehouse_id"`
}
type importInput struct {
	Rows []importRow `json:"rows"`
}
type importRow struct {
	SKU           string `json:"sku"`
	WarehouseCode string `json:"warehouse_code"`
	Quantity      string `json:"quantity"`
	Unit          string `json:"unit"`
	Reason        string `json:"reason"`
}

func newInventoryRoutes(repository *inventoryrepo.Repository) []ProtectedRoute {
	a := inventoryAPI{repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: InventoryPositionsPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listPositions)},
		{Method: http.MethodGet, Path: InventoryPositionsPath + "/", PathPrefix: true, Permission: "stock.read", Handler: http.HandlerFunc(a.positionRoute)},
		{Method: http.MethodPost, Path: InventoryPositionsPath + "/", PathPrefix: true, Permission: "stock.write", Handler: http.HandlerFunc(a.positionRoute)},
		{Method: http.MethodGet, Path: InventoryWarehousesPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listWarehouses)},
		{Method: http.MethodGet, Path: InventoryWarehousesPath + "/", PathPrefix: true, Permission: "stock.read", Handler: http.HandlerFunc(a.warehouseRoute)},
		{Method: http.MethodPost, Path: InventoryWarehousesPath, Permission: "stock.write", Handler: http.HandlerFunc(a.createWarehouse)},
		{Method: http.MethodPatch, Path: InventoryWarehousesPath + "/", PathPrefix: true, Permission: "stock.write", Handler: http.HandlerFunc(a.warehouseRoute)},
		{Method: http.MethodPut, Path: InventoryWarehousesPath + "/", PathPrefix: true, Permission: "stock.write", Handler: http.HandlerFunc(a.warehouseRoute)},
		{Method: http.MethodGet, Path: InventoryIncidentsPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listWarehouseIncidents)},
		{Method: http.MethodGet, Path: InventoryIncidentsPath + "/", PathPrefix: true, Permission: "stock.read", Handler: http.HandlerFunc(a.warehouseIncidentRoute)},
		{Method: http.MethodGet, Path: InventoryAllocationsPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listFulfillmentAllocations)},
		{Method: http.MethodPost, Path: InventoryAllocationsPath, Permission: "stock.write", Handler: http.HandlerFunc(a.reserveFulfillmentAllocation)},
		{Method: http.MethodGet, Path: InventoryAllocationsPath + "/", PathPrefix: true, Permission: "stock.read", Handler: http.HandlerFunc(a.fulfillmentAllocationRoute)},
		{Method: http.MethodPost, Path: InventoryImportPath, Permission: "stock.write", Handler: http.HandlerFunc(a.importRows)},
	}
}

func inventoryRequestScope(r *http.Request) (inventory.Scope, Principal, bool) {
	s, ok := ScopeFromContext(r.Context())
	p, pok := PrincipalFromContext(r.Context())
	if !ok || !pok {
		return inventory.Scope{}, Principal{}, false
	}
	v, e := inventory.ParseScope(s.OrganizationID().String(), s.WorkspaceID().String())
	return v, p, e == nil
}
func inventoryMutation(p Principal, r *http.Request) inventory.Mutation {
	id := newApprovalID()
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if correlation == "" {
		correlation = id
	}
	return inventory.Mutation{EventID: id, AuditID: newApprovalID(), ActorID: p.Subject, Source: "api", CorrelationID: correlation, OccurredAt: time.Now().UTC()}
}
func decodeInventory(w http.ResponseWriter, r *http.Request, v any) bool {
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); e != nil {
		writeProblem(w, 400, "Bad Request")
		return false
	}
	return true
}

func (a inventoryAPI) listPositions(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	items, e := a.repository.ListPositionViews(r.Context(), s, 100)
	writeInventory(w, 200, map[string]any{"items": items}, e)
}
func (a inventoryAPI) listWarehouses(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	items, e := a.repository.ListWarehouses(r.Context(), s, 500)
	writeInventory(w, 200, map[string]any{"items": items}, e)
}
func (a inventoryAPI) createWarehouse(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	var in warehouseInput
	if !decodeInventory(w, r, &in) {
		return
	}
	v, e := a.repository.CreateWarehouse(r.Context(), s, inventory.CreateWarehouse{ID: inventory.WarehouseID(newApprovalID()), Code: in.Code, Name: in.Name}, inventoryMutation(p, r))
	writeInventory(w, 201, v, e)
}
func (a inventoryAPI) warehouseRoute(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, InventoryWarehousesPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeProblem(w, 404, "Not Found")
		return
	}
	warehouseID := inventory.WarehouseID(parts[0])
	if !warehouseID.Valid() {
		writeProblem(w, 404, "Not Found")
		return
	}
	if r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "operational-state" {
		v, e := a.repository.OperationalState(r.Context(), s, warehouseID)
		writeInventory(w, 200, v, e)
		return
	}
	if r.Method == http.MethodPatch && len(parts) == 1 {
		var in warehouseInput
		if !decodeInventory(w, r, &in) {
			return
		}
		v, e := a.repository.ChangeWarehouseStatus(r.Context(), s, inventory.ChangeWarehouseStatus{ID: warehouseID, ExpectedVersion: in.Version, Status: inventory.WarehouseStatus(in.Status)}, inventoryMutation(p, r))
		writeInventory(w, 200, v, e)
		return
	}
	if r.Method == http.MethodPatch && len(parts) == 2 && parts[1] == "operational-state" {
		var in operationalStateInput
		if !decodeInventory(w, r, &in) {
			return
		}
		v, e := a.repository.SetOperationalState(r.Context(), s, warehouseID, inventory.OperationalState(in.State), in.Reason, in.Version)
		writeInventory(w, 200, v, e)
		return
	}
	if r.Method == http.MethodPut && len(parts) == 3 && parts[1] == "failover-routes" {
		destination := inventory.WarehouseID(parts[2])
		if !destination.Valid() {
			writeProblem(w, 404, "Not Found")
			return
		}
		var in failoverRouteInput
		if !decodeInventory(w, r, &in) {
			return
		}
		v, e := a.repository.PutFailoverRoute(r.Context(), s, inventory.FailoverRoute{SourceWarehouseID: warehouseID, DestinationWarehouseID: destination, Priority: in.Priority, Enabled: in.Enabled}, in.Version)
		writeInventory(w, 200, v, e)
		return
	}
	writeProblem(w, 404, "Not Found")
}

func (a inventoryAPI) listWarehouseIncidents(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	items, e := a.repository.ListWarehouseIncidents(r.Context(), s, 100)
	writeInventory(w, 200, map[string]any{"items": items}, e)
}

func (a inventoryAPI) warehouseIncidentRoute(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, InventoryIncidentsPath+"/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeProblem(w, 404, "Not Found")
		return
	}
	v, e := a.repository.WarehouseIncident(r.Context(), s, id)
	writeInventory(w, 200, v, e)
}

func (a inventoryAPI) listFulfillmentAllocations(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	items, e := a.repository.ListFulfillmentAllocations(r.Context(), s, 200)
	writeInventory(w, 200, map[string]any{"items": items}, e)
}

func (a inventoryAPI) reserveFulfillmentAllocation(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeProblem(w, 400, "Idempotency-Key Required")
		return
	}
	var in fulfillmentAllocationInput
	if !decodeInventory(w, r, &in) {
		return
	}
	value, e := a.repository.ReserveOrderItem(r.Context(), s, inventory.ReserveOrderItem{AllocationID: newApprovalID(), OrderItemID: in.OrderItemID, IdempotencyKey: key, WarehouseID: inventory.WarehouseID(in.WarehouseID)}, inventoryMutation(p, r))
	writeInventory(w, 201, value, e)
}

func (a inventoryAPI) fulfillmentAllocationRoute(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, InventoryAllocationsPath+"/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeProblem(w, 404, "Not Found")
		return
	}
	value, e := a.repository.FulfillmentAllocation(r.Context(), s, id)
	writeInventory(w, 200, value, e)
}

func (a inventoryAPI) positionRoute(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, InventoryPositionsPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeProblem(w, 404, "Not Found")
		return
	}
	id := parts[0]
	if r.Method == http.MethodGet && len(parts) == 1 {
		v, e := a.repository.PositionViewByID(r.Context(), s, id)
		if e != nil {
			writeInventory(w, 0, nil, e)
			return
		}
		history, e := a.repository.MovementHistory(r.Context(), s, id, 200)
		writeInventory(w, 200, map[string]any{"position": v, "movements": history}, e)
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "adjustments" {
		var in adjustmentInput
		if !decodeInventory(w, r, &in) {
			return
		}
		q, e := inventory.ParseDecimal(in.Quantity)
		if e != nil {
			writeProblem(w, 400, "Bad Request")
			return
		}
		current, e := a.repository.Position(r.Context(), s, inventory.PositionID(id))
		if e != nil {
			writeInventory(w, 0, nil, e)
			return
		}
		quantity, _ := inventory.NewQuantity(q, current.OnHand.Unit)
		v, e := a.repository.SetOnHand(r.Context(), s, inventory.ChangeQuantity{ID: inventory.PositionID(id), ExpectedVersion: in.Version, Quantity: quantity, Reason: in.Reason}, inventoryMutation(p, r))
		writeInventory(w, 200, v, e)
		return
	}
	writeProblem(w, 404, "Not Found")
}

func (a inventoryAPI) importRows(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	var in importInput
	if !decodeInventory(w, r, &in) {
		return
	}
	if len(in.Rows) < 1 || len(in.Rows) > 500 {
		writeProblem(w, 400, "Bad Request")
		return
	}
	created, updated := 0, 0
	for _, row := range in.Rows {
		offer, warehouse, e := a.repository.ResolveImportParents(r.Context(), s, row.SKU, row.WarehouseCode)
		if e != nil {
			writeInventory(w, 0, nil, e)
			return
		}
		unit, e := inventory.NewUnitCode(strings.ToUpper(row.Unit))
		if e != nil {
			writeProblem(w, 400, "Bad Request")
			return
		}
		position, e := a.repository.PositionByParents(r.Context(), s, offer, warehouse)
		if errors.Is(e, inventory.ErrNotFound) {
			position, e = a.repository.CreatePosition(r.Context(), s, inventory.CreatePosition{ID: inventory.PositionID(newApprovalID()), OfferID: offer, WarehouseID: warehouse, Unit: unit}, inventoryMutation(p, r))
			if e == nil {
				created++
			}
		}
		if e != nil {
			writeInventory(w, 0, nil, e)
			return
		}
		decimal, e := inventory.ParseDecimal(row.Quantity)
		if e != nil {
			writeProblem(w, 400, "Bad Request")
			return
		}
		quantity, _ := inventory.NewQuantity(decimal, unit)
		reason := row.Reason
		if reason == "" {
			reason = "inventory_import"
		}
		_, e = a.repository.SetOnHand(r.Context(), s, inventory.ChangeQuantity{ID: position.ID, ExpectedVersion: position.Version, Quantity: quantity, Reason: reason}, inventoryMutation(p, r))
		if e != nil {
			writeInventory(w, 0, nil, e)
			return
		}
		updated++
	}
	writeJSON(w, 200, map[string]int{"created": created, "updated": updated})
}

func writeInventory(w http.ResponseWriter, status int, v any, e error) {
	if e == nil {
		writeJSON(w, status, v)
		return
	}
	switch {
	case errors.Is(e, inventory.ErrNotFound), errors.Is(e, sql.ErrNoRows):
		writeProblem(w, 404, "Not Found")
	case errors.Is(e, inventory.ErrConflict):
		writeProblem(w, 409, "Conflict")
	case errors.Is(e, inventory.ErrInvalidRecord), errors.Is(e, inventory.ErrInsufficientAvailable), errors.Is(e, inventory.ErrInsufficientReserved), errors.Is(e, inventory.ErrWarehouseInactive):
		writeProblem(w, 400, "Bad Request")
	default:
		writeProblem(w, 500, "Internal Server Error")
	}
}
