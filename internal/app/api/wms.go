package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
)

const WarehouseTasksPath = "/api/v1/warehouse-tasks"
const WarehouseTaskBatchesPath = "/api/v1/warehouse-task-batches"

type wmsAPI struct{ repository *inventoryrepo.Repository }

type wmsTaskCommandInput struct {
	Version    int64  `json:"version"`
	ReasonCode string `json:"reason_code"`
}

type wmsScanInput struct {
	Version      int64  `json:"version"`
	Barcode      string `json:"barcode"`
	LocationCode string `json:"location_code"`
	Quantity     string `json:"quantity"`
}

type wmsCreateFromOrderInput struct {
	OrderID     string `json:"order_id"`
	WarehouseID string `json:"warehouse_id"`
}

type wmsCreateTaskInput struct {
	TaskType           string `json:"task_type"`
	WarehouseID        string `json:"warehouse_id"`
	SKU                string `json:"sku"`
	SourceLocationCode string `json:"source_location_code"`
	TargetLocationCode string `json:"target_location_code"`
	Quantity           string `json:"quantity"`
	Unit               string `json:"unit"`
}

type wmsCreateBatchInput struct {
	WarehouseID string   `json:"warehouse_id"`
	TaskIDs     []string `json:"task_ids"`
}

type wmsQuantityResponse struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

type wmsTaskResponse struct {
	ID                      string              `json:"id"`
	TaskType                string              `json:"task_type"`
	State                   string              `json:"state"`
	WarehouseID             string              `json:"warehouse_id"`
	SKU                     string              `json:"sku"`
	OrderID                 string              `json:"order_id,omitempty"`
	OrderItemID             string              `json:"order_item_id,omitempty"`
	FulfillmentAllocationID string              `json:"fulfillment_allocation_id,omitempty"`
	SourceLocationCode      string              `json:"source_location_code,omitempty"`
	TargetLocationCode      string              `json:"target_location_code,omitempty"`
	ExpectedQuantity        wmsQuantityResponse `json:"expected_quantity"`
	ProcessedQuantity       wmsQuantityResponse `json:"processed_quantity"`
	AssignedTo              string              `json:"assigned_to,omitempty"`
	ExceptionCode           string              `json:"exception_code,omitempty"`
	CancelReason            string              `json:"cancel_reason,omitempty"`
	Version                 int64               `json:"version"`
	ClaimedAt               *string             `json:"claimed_at,omitempty"`
	StartedAt               *string             `json:"started_at,omitempty"`
	CompletedAt             *string             `json:"completed_at,omitempty"`
	CreatedAt               string              `json:"created_at"`
	UpdatedAt               string              `json:"updated_at"`
}

type wmsTaskEventResponse struct {
	ID            string              `json:"id"`
	TaskID        string              `json:"task_id"`
	Kind          string              `json:"kind"`
	BarcodeDigest string              `json:"barcode_digest,omitempty"`
	LocationCode  string              `json:"location_code,omitempty"`
	Quantity      wmsQuantityResponse `json:"quantity"`
	ReasonCode    string              `json:"reason_code,omitempty"`
	ActorID       string              `json:"actor_id"`
	OccurredAt    string              `json:"occurred_at"`
}

type wmsBatchResponse struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	State       string   `json:"state"`
	WarehouseID string   `json:"warehouse_id"`
	TaskIDs     []string `json:"task_ids"`
	Version     int64    `json:"version"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func newWMSTaskRoutes(repository *inventoryrepo.Repository) []ProtectedRoute {
	a := wmsAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: WarehouseTasksPath, Permission: "wms.read", Handler: http.HandlerFunc(a.list)},
		{Method: http.MethodPost, Path: WarehouseTasksPath, Permission: "wms.write", Handler: http.HandlerFunc(a.create)},
		{Method: http.MethodPost, Path: WarehouseTasksPath + "/from-order", Permission: "wms.write", Handler: http.HandlerFunc(a.createFromOrder)},
		{Method: http.MethodGet, Path: WarehouseTasksPath + "/", PathPrefix: true, Permission: "wms.read", Handler: http.HandlerFunc(a.taskRoute)},
		{Method: http.MethodPost, Path: WarehouseTasksPath + "/", PathPrefix: true, Permission: "wms.write", Handler: http.HandlerFunc(a.taskRoute)},
		{Method: http.MethodPost, Path: WarehouseTaskBatchesPath, Permission: "wms.write", Handler: http.HandlerFunc(a.createBatch)},
		{Method: http.MethodGet, Path: WarehouseTaskBatchesPath + "/", PathPrefix: true, Permission: "wms.read", Handler: http.HandlerFunc(a.batchRoute)},
		{Method: http.MethodPost, Path: WarehouseTaskBatchesPath + "/", PathPrefix: true, Permission: "wms.write", Handler: http.HandlerFunc(a.batchRoute)},
	}
}

func (a wmsAPI) list(w http.ResponseWriter, r *http.Request) {
	scope, _, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	limit := 50
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = parsed
	}
	items, next, err := a.repository.ListWMSTasks(r.Context(), scope, inventoryrepo.WMSTaskListOptions{State: strings.TrimSpace(r.URL.Query().Get("state")), TaskType: strings.TrimSpace(r.URL.Query().Get("task_type")), WarehouseID: strings.TrimSpace(r.URL.Query().Get("warehouse_id")), Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")), Limit: limit})
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]wmsTaskResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderWMSTask(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result, "next_cursor": next})
}

func (a wmsAPI) create(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input wmsCreateTaskInput
	if !decodeWMS(w, r, &input) {
		return
	}
	quantity, err := inventory.ParseDecimal(strings.TrimSpace(input.Quantity))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	unit, err := inventory.NewUnitCode(strings.TrimSpace(input.Unit))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	expected, err := inventory.NewQuantity(quantity, unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	item, created, err := a.repository.WMSCreateTask(r.Context(), scope, inventoryrepo.CreateWMSTask{ID: newApprovalID(), IdempotencyKey: key, TaskType: strings.TrimSpace(input.TaskType), WarehouseID: strings.TrimSpace(input.WarehouseID), SKU: strings.TrimSpace(input.SKU), SourceLocationCode: strings.TrimSpace(input.SourceLocationCode), TargetLocationCode: strings.TrimSpace(input.TargetLocationCode), ExpectedQuantity: expected}, inventoryMutation(principal, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderWMSTask(item))
}

func (a wmsAPI) createFromOrder(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input wmsCreateFromOrderInput
	if !decodeWMS(w, r, &input) {
		return
	}
	items, err := a.repository.WMSCreateOrderPickTasks(r.Context(), scope, inventoryrepo.CreateOrderPickTasks{OrderID: strings.TrimSpace(input.OrderID), WarehouseID: strings.TrimSpace(input.WarehouseID), IdempotencyKey: key}, inventoryMutation(principal, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]wmsTaskResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderWMSTask(item))
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": result})
}

func (a wmsAPI) taskRoute(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, WarehouseTasksPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) < 1 || parts[0] == "" || parts[0] == "from-order" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	taskID := parts[0]
	if r.Method == http.MethodGet {
		if len(parts) == 1 {
			item, err := a.repository.WMSTask(r.Context(), scope, taskID)
			if err != nil {
				writeWMSError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, renderWMSTask(item))
			return
		}
		if len(parts) == 2 && parts[1] == "history" {
			events, err := a.repository.WMSHistory(r.Context(), scope, taskID, 200)
			if err != nil {
				writeWMSError(w, err)
				return
			}
			result := make([]wmsTaskEventResponse, 0, len(events))
			for _, event := range events {
				result = append(result, renderWMSTaskEvent(event))
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": result})
			return
		}
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if r.Method != http.MethodPost || len(parts) != 2 {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input wmsTaskCommandInput
	if parts[1] == "scan" {
		var scan wmsScanInput
		if !decodeWMS(w, r, &scan) {
			return
		}
		quantity, err := inventory.ParseDecimal(strings.TrimSpace(scan.Quantity))
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		task, err := a.repository.WMSTask(r.Context(), scope, taskID)
		if err != nil {
			writeWMSError(w, err)
			return
		}
		value, err := inventory.NewQuantity(quantity, task.ExpectedQuantity.Unit)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		item, err := a.repository.WMSScanTask(r.Context(), scope, taskID, principal.Subject, key, strings.TrimSpace(scan.Barcode), strings.TrimSpace(scan.LocationCode), value, scan.Version, inventoryMutation(principal, r))
		if err != nil {
			writeWMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, renderWMSTask(item))
		return
	}
	if !decodeWMS(w, r, &input) {
		return
	}
	var (
		item inventoryrepo.WMSTask
		err  error
	)
	mutation := inventoryMutation(principal, r)
	switch parts[1] {
	case "claim":
		item, err = a.repository.WMSClaimTask(r.Context(), scope, taskID, principal.Subject, key, input.Version, mutation)
	case "start":
		item, err = a.repository.WMSStartTask(r.Context(), scope, taskID, principal.Subject, key, input.Version, mutation)
	case "complete":
		item, err = a.repository.WMSCompleteTask(r.Context(), scope, taskID, principal.Subject, key, input.Version, mutation)
	case "exception":
		item, err = a.repository.WMSExceptionTask(r.Context(), scope, taskID, principal.Subject, key, strings.TrimSpace(input.ReasonCode), input.Version, mutation)
	case "cancel":
		item, err = a.repository.WMSCancelTask(r.Context(), scope, taskID, principal.Subject, key, strings.TrimSpace(input.ReasonCode), input.Version, mutation)
	default:
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderWMSTask(item))
}

func (a wmsAPI) createBatch(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input wmsCreateBatchInput
	if !decodeWMS(w, r, &input) {
		return
	}
	item, created, err := a.repository.WMSCreateBatch(r.Context(), scope, inventoryrepo.CreateWMSBatch{ID: newApprovalID(), IdempotencyKey: key, WarehouseID: strings.TrimSpace(input.WarehouseID), TaskIDs: input.TaskIDs}, inventoryMutation(principal, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderWMSBatch(item))
}

func (a wmsAPI) batchRoute(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := inventoryRequestScope(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, WarehouseTaskBatchesPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if r.Method == http.MethodGet && len(parts) == 1 {
		item, err := a.repository.WMSBatch(r.Context(), scope, parts[0])
		if err != nil {
			writeWMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, renderWMSBatch(item))
		return
	}
	if r.Method != http.MethodPost || len(parts) != 2 || parts[1] != "handoff" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input wmsTaskCommandInput
	if !decodeWMS(w, r, &input) {
		return
	}
	item, err := a.repository.WMSHandoffBatch(r.Context(), scope, parts[0], principal.Subject, key, input.Version, inventoryMutation(principal, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderWMSBatch(item))
}

func decodeWMS(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(value); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return false
	}
	return true
}

func renderWMSTask(item inventoryrepo.WMSTask) wmsTaskResponse {
	return wmsTaskResponse{ID: item.ID, TaskType: item.TaskType, State: item.State, WarehouseID: item.WarehouseID, SKU: item.SKU, OrderID: item.OrderID, OrderItemID: item.OrderItemID, FulfillmentAllocationID: item.FulfillmentAllocationID, SourceLocationCode: item.SourceLocationCode, TargetLocationCode: item.TargetLocationCode, ExpectedQuantity: wmsQuantityResponse{Value: item.ExpectedQuantity.Value.String(), Unit: item.ExpectedQuantity.Unit.String()}, ProcessedQuantity: wmsQuantityResponse{Value: item.ProcessedQuantity.Value.String(), Unit: item.ProcessedQuantity.Unit.String()}, AssignedTo: item.AssignedTo, ExceptionCode: item.ExceptionCode, CancelReason: item.CancelReason, Version: item.Version, ClaimedAt: renderTime(item.ClaimedAt), StartedAt: renderTime(item.StartedAt), CompletedAt: renderTime(item.CompletedAt), CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func renderWMSBatch(item inventoryrepo.WMSBatch) wmsBatchResponse {
	return wmsBatchResponse{ID: item.ID, Kind: item.Kind, State: item.State, WarehouseID: item.WarehouseID, TaskIDs: item.TaskIDs, Version: item.Version, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func renderWMSTaskEvent(item inventoryrepo.WMSTaskEvent) wmsTaskEventResponse {
	return wmsTaskEventResponse{ID: item.ID, TaskID: item.TaskID, Kind: item.Kind, BarcodeDigest: item.BarcodeDigest, LocationCode: item.LocationCode, Quantity: wmsQuantityResponse{Value: item.Quantity.Value.String(), Unit: item.Quantity.Unit.String()}, ReasonCode: item.ReasonCode, ActorID: item.ActorID, OccurredAt: item.OccurredAt.UTC().Format(time.RFC3339Nano)}
}

func renderTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	result := value.UTC().Format(time.RFC3339Nano)
	return &result
}

func writeWMSError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, inventory.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, inventory.ErrConflict), errors.Is(err, inventoryrepo.ErrWMSTaskState), errors.Is(err, inventoryrepo.ErrWMSTaskAssignment), errors.Is(err, inventoryrepo.ErrWMSBatchState), errors.Is(err, inventoryrepo.ErrMobilePlanMode), errors.Is(err, inventoryrepo.ErrMobileDeviceRevoked), errors.Is(err, inventoryrepo.ErrMobilePrintState), errors.Is(err, inventoryrepo.ErrMobilePlanState):
		writeProblem(w, http.StatusConflict, "Conflict")
	case errors.Is(err, inventory.ErrInvalidRecord), errors.Is(err, inventory.ErrInsufficientAvailable), errors.Is(err, inventory.ErrWarehouseInactive):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
