package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
)

const (
	MobileWarehousePath        = "/api/v1/warehouse-mobile"
	MobileWarehousePlansPath   = MobileWarehousePath + "/plans"
	MobileWarehouseBatchesPath = MobileWarehousePath + "/batches"
	MobileWarehouseScansPath   = MobileWarehousePath + "/scans"
	MobileWarehousePacksPath   = MobileWarehousePath + "/packs"
	MobileWarehousePrintPath   = MobileWarehousePath + "/print-jobs"
	MobileWarehouseDevicesPath = MobileWarehousePath + "/devices"
	MobileWarehouseOfflinePath = MobileWarehousePath + "/offline-intents"
	MobileWarehouseObsPath     = MobileWarehousePath + "/observations"
)

type mobileWarehouseAPI struct{ repository *inventoryrepo.Repository }

type mobilePlanWrite struct {
	OrderID               string `json:"order_id"`
	WarehouseID           string `json:"warehouse_id"`
	Mode                  string `json:"mode"`
	RemoteReferenceDigest string `json:"remote_reference_digest"`
}

type mobilePlanAdvanceWrite struct {
	Stage                 string `json:"stage"`
	State                 string `json:"state"`
	RemoteReferenceDigest string `json:"remote_reference_digest"`
	Version               int64  `json:"version"`
}

type mobileBatchWrite struct {
	PlanID      string   `json:"plan_id"`
	OrderID     string   `json:"order_id"`
	WarehouseID string   `json:"warehouse_id"`
	Strategy    string   `json:"strategy"`
	RouteDigest string   `json:"route_digest"`
	TaskIDs     []string `json:"task_ids"`
}

type mobileScanWrite struct {
	PlanID       string `json:"plan_id"`
	TaskID       string `json:"task_id"`
	DeviceID     string `json:"device_id"`
	Kind         string `json:"kind"`
	Code         string `json:"code"`
	LocationCode string `json:"location_code"`
	Quantity     string `json:"quantity"`
	Version      int64  `json:"version"`
}

type mobilePackWrite struct {
	PlanID        string `json:"plan_id"`
	BatchID       string `json:"batch_id"`
	PackageCount  int    `json:"package_count"`
	WeightGrams   int64  `json:"weight_grams"`
	LengthMM      int64  `json:"length_mm"`
	WidthMM       int64  `json:"width_mm"`
	HeightMM      int64  `json:"height_mm"`
	PackagingType string `json:"packaging_type"`
	FactsDigest   string `json:"facts_digest"`
	Version       int64  `json:"version"`
}

type mobilePrintWrite struct {
	PlanID          string `json:"plan_id"`
	PackID          string `json:"pack_id"`
	PrinterID       string `json:"printer_id"`
	Document        string `json:"document"`
	TemplateVersion string `json:"template_version"`
	MediaSize       string `json:"media_size"`
	Language        string `json:"language"`
	Copies          int    `json:"copies"`
	Reprint         bool   `json:"reprint"`
	ReasonCode      string `json:"reason_code"`
	ArtifactDigest  string `json:"artifact_digest"`
	Version         int64  `json:"version"`
}

type mobilePrintStatusWrite struct {
	State     string `json:"state"`
	ErrorCode string `json:"error_code"`
	Version   int64  `json:"version"`
}

type mobileDeviceWrite struct {
	Label       string `json:"label"`
	WarehouseID string `json:"warehouse_id"`
	ZoneCode    string `json:"zone_code"`
	Kind        string `json:"kind"`
}

type mobileDeviceRevokeWrite struct {
	Version int64 `json:"version"`
}

type mobileOfflineWrite struct {
	IntentID     string `json:"intent_id"`
	PlanID       string `json:"plan_id"`
	TaskID       string `json:"task_id"`
	DeviceID     string `json:"device_id"`
	Kind         string `json:"kind"`
	Code         string `json:"code"`
	LocationCode string `json:"location_code"`
	Quantity     string `json:"quantity"`
	Version      int64  `json:"version"`
	SequenceNo   int64  `json:"sequence_no"`
}

type mobileObservationWrite struct {
	PlanID                string `json:"plan_id"`
	Stage                 string `json:"stage"`
	State                 string `json:"state"`
	RemoteReferenceDigest string `json:"remote_reference_digest"`
	Version               int64  `json:"version"`
}

type mobilePlanResponse struct {
	ID                    string `json:"id"`
	OrderID               string `json:"order_id,omitempty"`
	WarehouseID           string `json:"warehouse_id,omitempty"`
	Mode                  string `json:"mode"`
	Owner                 string `json:"owner"`
	Stage                 string `json:"stage"`
	State                 string `json:"state"`
	LocalExecution        bool   `json:"local_execution"`
	RemoteReferenceDigest string `json:"remote_reference_digest,omitempty"`
	Version               int64  `json:"version"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

type mobileBatchResponse struct {
	ID          string   `json:"id"`
	PlanID      string   `json:"plan_id"`
	WarehouseID string   `json:"warehouse_id"`
	Strategy    string   `json:"strategy"`
	State       string   `json:"state"`
	RouteDigest string   `json:"route_digest,omitempty"`
	TaskIDs     []string `json:"task_ids"`
	Version     int64    `json:"version"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type mobileScanResponse struct {
	ID           string              `json:"id"`
	PlanID       string              `json:"plan_id"`
	TaskID       string              `json:"task_id"`
	DeviceID     string              `json:"device_id"`
	Kind         string              `json:"kind"`
	CodeDigest   string              `json:"code_digest"`
	LocationCode string              `json:"location_code,omitempty"`
	Result       string              `json:"result"`
	Quantity     wmsQuantityResponse `json:"quantity"`
	OccurredAt   string              `json:"occurred_at"`
}

type mobilePackResponse struct {
	ID            string `json:"id"`
	PlanID        string `json:"plan_id"`
	BatchID       string `json:"batch_id,omitempty"`
	PackagingType string `json:"packaging_type,omitempty"`
	State         string `json:"state"`
	FactsDigest   string `json:"facts_digest"`
	PackageCount  int    `json:"package_count"`
	WeightGrams   int64  `json:"weight_grams"`
	LengthMM      int64  `json:"length_mm"`
	WidthMM       int64  `json:"width_mm"`
	HeightMM      int64  `json:"height_mm"`
	Version       int64  `json:"version"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type mobilePrintResponse struct {
	ID              string `json:"id"`
	PlanID          string `json:"plan_id"`
	PackID          string `json:"pack_id,omitempty"`
	PrinterID       string `json:"printer_id"`
	Document        string `json:"document"`
	TemplateVersion string `json:"template_version"`
	MediaSize       string `json:"media_size,omitempty"`
	Language        string `json:"language"`
	Copies          int    `json:"copies"`
	State           string `json:"state"`
	Reprint         bool   `json:"reprint"`
	ReasonCode      string `json:"reason_code,omitempty"`
	ArtifactDigest  string `json:"artifact_digest,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	Version         int64  `json:"version"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type mobileDeviceResponse struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	WarehouseID string  `json:"warehouse_id"`
	ZoneCode    string  `json:"zone_code,omitempty"`
	Kind        string  `json:"kind"`
	State       string  `json:"state"`
	OperatorID  string  `json:"operator_id"`
	LastSeenAt  *string `json:"last_seen_at,omitempty"`
	Version     int64   `json:"version"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type mobileIntentResponse struct {
	ID            string `json:"id"`
	DeviceID      string `json:"device_id"`
	PlanID        string `json:"plan_id"`
	Operation     string `json:"operation"`
	PayloadDigest string `json:"payload_digest"`
	State         string `json:"state"`
	ErrorCode     string `json:"error_code,omitempty"`
	SequenceNo    int64  `json:"sequence_no"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func newMobileWarehouseRoutes(repository *inventoryrepo.Repository) []ProtectedRoute {
	a := mobileWarehouseAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: MobileWarehousePath + "/summary", Permission: "stock.read", Handler: http.HandlerFunc(a.summary)},
		{Method: http.MethodGet, Path: MobileWarehousePlansPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listPlans)},
		{Method: http.MethodPost, Path: MobileWarehousePlansPath, Permission: "wms.write", Handler: http.HandlerFunc(a.createPlan)},
		{Method: http.MethodGet, Path: MobileWarehousePlansPath + "/", PathPrefix: true, Permission: "stock.read", Handler: http.HandlerFunc(a.planRoute)},
		{Method: http.MethodPost, Path: MobileWarehousePlansPath + "/", PathPrefix: true, Permission: "wms.write", Handler: http.HandlerFunc(a.planRoute)},
		{Method: http.MethodGet, Path: MobileWarehouseBatchesPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listBatches)},
		{Method: http.MethodPost, Path: MobileWarehouseBatchesPath, Permission: "wms.write", Handler: http.HandlerFunc(a.createBatch)},
		{Method: http.MethodPost, Path: MobileWarehouseScansPath, Permission: "wms.write", Handler: http.HandlerFunc(a.scan)},
		{Method: http.MethodGet, Path: MobileWarehousePacksPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listPacks)},
		{Method: http.MethodPost, Path: MobileWarehousePacksPath, Permission: "wms.write", Handler: http.HandlerFunc(a.createPack)},
		{Method: http.MethodPost, Path: MobileWarehousePacksPath + "/", PathPrefix: true, Permission: "wms.write", Handler: http.HandlerFunc(a.packRoute)},
		{Method: http.MethodGet, Path: MobileWarehousePrintPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listPrintJobs)},
		{Method: http.MethodPost, Path: MobileWarehousePrintPath, Permission: "wms.write", Handler: http.HandlerFunc(a.queuePrint)},
		{Method: http.MethodPost, Path: MobileWarehousePrintPath + "/", PathPrefix: true, Permission: "wms.write", Handler: http.HandlerFunc(a.printRoute)},
		{Method: http.MethodGet, Path: MobileWarehouseDevicesPath, Permission: "stock.read", Handler: http.HandlerFunc(a.listDevices)},
		{Method: http.MethodPost, Path: MobileWarehouseDevicesPath, Permission: "wms.write", Handler: http.HandlerFunc(a.registerDevice)},
		{Method: http.MethodPost, Path: MobileWarehouseDevicesPath + "/", PathPrefix: true, Permission: "wms.write", Handler: http.HandlerFunc(a.deviceRoute)},
		{Method: http.MethodGet, Path: MobileWarehouseOfflinePath, Permission: "stock.read", Handler: http.HandlerFunc(a.listOffline)},
		{Method: http.MethodPost, Path: MobileWarehouseOfflinePath, Permission: "wms.write", Handler: http.HandlerFunc(a.replayOffline)},
		{Method: http.MethodPost, Path: MobileWarehouseObsPath, Permission: "wms.write", Handler: http.HandlerFunc(a.observe)},
	}
}

func (a mobileWarehouseAPI) summary(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	value, err := a.repository.MobileSummary(r.Context(), s)
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (a mobileWarehouseAPI) listPlans(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	limit := mobileLimit(r, 100, 200)
	items, err := a.repository.ListMobilePlans(r.Context(), s, strings.TrimSpace(r.URL.Query().Get("mode")), strings.TrimSpace(r.URL.Query().Get("state")), strings.TrimSpace(r.URL.Query().Get("warehouse_id")), limit)
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]mobilePlanResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderMobilePlan(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (a mobileWarehouseAPI) createPlan(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobilePlanWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	mode := inventory.FulfillmentMode(strings.TrimSpace(input.Mode))
	owner := inventory.MobileOwnerForMode(mode)
	local := mode != inventory.FulfillmentFBO
	item, created, err := a.repository.CreateMobilePlan(r.Context(), s, inventoryrepo.CreateMobilePlan{ID: newApprovalID(), IdempotencyKey: key, OrderID: strings.TrimSpace(input.OrderID), WarehouseID: strings.TrimSpace(input.WarehouseID), Mode: mode, Owner: owner, LocalExecution: local, RemoteReferenceDigest: strings.TrimSpace(input.RemoteReferenceDigest)}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderMobilePlan(item))
}

func (a mobileWarehouseAPI) planRoute(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, MobileWarehousePlansPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	planID := parts[0]
	if r.Method == http.MethodGet && len(parts) == 1 {
		item, err := a.repository.MobilePlan(r.Context(), s, planID)
		if err != nil {
			writeWMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, renderMobilePlan(item))
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "pick-batches" {
		key, ok := mobileIdempotency(w, r)
		if !ok {
			return
		}
		var input mobileBatchWrite
		if !decodeMobile(w, r, &input) {
			return
		}
		plan, err := a.repository.MobilePlan(r.Context(), s, planID)
		if err != nil {
			writeWMSError(w, err)
			return
		}
		if input.OrderID != "" {
			if input.OrderID != plan.OrderID {
				writeProblem(w, http.StatusConflict, "Conflict")
				return
			}
			tasks, taskErr := a.repository.WMSCreateOrderPickTasks(r.Context(), s, inventoryrepo.CreateOrderPickTasks{OrderID: input.OrderID, WarehouseID: plan.WarehouseID, IdempotencyKey: key + ":wms"}, inventoryMutation(p, r))
			if taskErr != nil {
				writeWMSError(w, taskErr)
				return
			}
			input.TaskIDs = make([]string, 0, len(tasks))
			for _, task := range tasks {
				input.TaskIDs = append(input.TaskIDs, task.ID)
			}
		}
		item, created, err := a.repository.CreateMobilePickBatch(r.Context(), s, inventoryrepo.CreateMobilePickBatch{ID: newApprovalID(), IdempotencyKey: key, PlanID: planID, WarehouseID: plan.WarehouseID, Strategy: strings.TrimSpace(input.Strategy), RouteDigest: strings.TrimSpace(input.RouteDigest), TaskIDs: input.TaskIDs}, mobileMutation(p, r))
		if err != nil {
			writeWMSError(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, renderMobileBatch(item))
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "advance" {
		key, ok := mobileIdempotency(w, r)
		if !ok {
			return
		}
		var input mobilePlanAdvanceWrite
		if !decodeMobile(w, r, &input) {
			return
		}
		item, err := a.repository.AdvanceMobilePlan(r.Context(), s, inventoryrepo.AdvanceMobilePlanCommand{PlanID: planID, Stage: strings.TrimSpace(input.Stage), State: strings.TrimSpace(input.State), RemoteReferenceDigest: strings.TrimSpace(input.RemoteReferenceDigest)}, input.Version, mobileMutation(p, r))
		if err != nil {
			writeWMSError(w, err)
			return
		}
		_ = key
		writeJSON(w, http.StatusOK, renderMobilePlan(item))
		return
	}
	writeProblem(w, http.StatusNotFound, "Not Found")
}

func (a mobileWarehouseAPI) listBatches(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := a.repository.ListMobilePickBatches(r.Context(), s, strings.TrimSpace(r.URL.Query().Get("plan_id")), mobileLimit(r, 100, 100))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]mobileBatchResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderMobileBatch(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (a mobileWarehouseAPI) createBatch(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobileBatchWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, created, err := a.repository.CreateMobilePickBatch(r.Context(), s, inventoryrepo.CreateMobilePickBatch{ID: newApprovalID(), IdempotencyKey: key, PlanID: strings.TrimSpace(input.PlanID), WarehouseID: strings.TrimSpace(input.WarehouseID), Strategy: strings.TrimSpace(input.Strategy), RouteDigest: strings.TrimSpace(input.RouteDigest), TaskIDs: input.TaskIDs}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderMobileBatch(item))
}

func (a mobileWarehouseAPI) scan(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobileScanWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	quantity, err := inventory.ParseDecimal(strings.TrimSpace(input.Quantity))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	task, err := a.repository.WMSTask(r.Context(), s, strings.TrimSpace(input.TaskID))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	value, err := inventory.NewQuantity(quantity, task.ExpectedQuantity.Unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	kind := inventory.MobileScanKind(strings.TrimSpace(input.Kind))
	scanInput := inventory.MobileScanInput{TaskID: strings.TrimSpace(input.TaskID), DeviceID: strings.TrimSpace(input.DeviceID), Kind: kind, Code: input.Code, LocationCode: strings.TrimSpace(input.LocationCode), ExpectedVersion: input.Version, Quantity: value}
	if err := scanInput.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if kind == inventory.ScanProduct || kind == inventory.ScanSerial {
		if _, err := a.repository.WMSScanTask(r.Context(), s, scanInput.TaskID, p.Subject, key, scanInput.Code, scanInput.LocationCode, value, scanInput.ExpectedVersion, inventoryMutation(p, r)); err != nil {
			writeWMSError(w, err)
			return
		}
	}
	evidence, _, err := a.repository.RecordMobileScan(r.Context(), s, inventoryrepo.MobileScanEvidence{ID: newApprovalID(), IdempotencyKey: key, PlanID: strings.TrimSpace(input.PlanID), TaskID: scanInput.TaskID, DeviceID: scanInput.DeviceID, Kind: kind, CodeDigest: inventory.MobileCodeDigest(scanInput.Code), LocationCode: scanInput.LocationCode, Result: "applied", Quantity: value, OccurredAt: inventoryMutation(p, r).OccurredAt}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderMobileScan(evidence))
}

func (a mobileWarehouseAPI) listPacks(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := a.repository.ListMobilePacks(r.Context(), s, strings.TrimSpace(r.URL.Query().Get("plan_id")), mobileLimit(r, 100, 100))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]mobilePackResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderMobilePack(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (a mobileWarehouseAPI) createPack(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobilePackWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, created, err := a.repository.CreateMobilePack(r.Context(), s, inventoryrepo.CreateMobilePack{ID: newApprovalID(), IdempotencyKey: key, PlanID: strings.TrimSpace(input.PlanID), BatchID: strings.TrimSpace(input.BatchID), Facts: inventory.PackageFacts{PackageCount: input.PackageCount, WeightGrams: input.WeightGrams, LengthMM: input.LengthMM, WidthMM: input.WidthMM, HeightMM: input.HeightMM}, PackagingType: strings.TrimSpace(input.PackagingType), FactsDigest: strings.TrimSpace(input.FactsDigest)}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderMobilePack(item))
}

func (a mobileWarehouseAPI) packRoute(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, MobileWarehousePacksPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "close" || r.Method != http.MethodPost {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if _, ok := mobileIdempotency(w, r); !ok {
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if !decodeMobile(w, r, &input) {
		return
	}
	item, err := a.repository.CloseMobilePack(r.Context(), s, parts[0], input.Version, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderMobilePack(item))
}

func (a mobileWarehouseAPI) listPrintJobs(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := a.repository.ListMobilePrintJobs(r.Context(), s, strings.TrimSpace(r.URL.Query().Get("plan_id")), mobileLimit(r, 100, 100))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]mobilePrintResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderMobilePrint(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (a mobileWarehouseAPI) queuePrint(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobilePrintWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, created, err := a.repository.QueueMobilePrint(r.Context(), s, inventoryrepo.QueueMobilePrint{ID: newApprovalID(), IdempotencyKey: key, PlanID: strings.TrimSpace(input.PlanID), PackID: strings.TrimSpace(input.PackID), PrinterID: strings.TrimSpace(input.PrinterID), Document: inventory.MobilePrintDocument(strings.TrimSpace(input.Document)), TemplateVersion: strings.TrimSpace(input.TemplateVersion), MediaSize: strings.TrimSpace(input.MediaSize), Language: strings.TrimSpace(input.Language), Copies: input.Copies, Reprint: input.Reprint, ReasonCode: strings.TrimSpace(input.ReasonCode), ArtifactDigest: strings.TrimSpace(input.ArtifactDigest)}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderMobilePrint(item))
}

func (a mobileWarehouseAPI) printRoute(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, MobileWarehousePrintPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "status" || r.Method != http.MethodPost {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobilePrintStatusWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, err := a.repository.RecordMobilePrintStatus(r.Context(), s, parts[0], strings.TrimSpace(input.State), strings.TrimSpace(input.ErrorCode), input.Version, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	_ = key
	writeJSON(w, http.StatusOK, renderMobilePrint(item))
}

func (a mobileWarehouseAPI) listDevices(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := a.repository.ListMobileDevices(r.Context(), s, strings.TrimSpace(r.URL.Query().Get("warehouse_id")), mobileLimit(r, 100, 100))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]mobileDeviceResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderMobileDevice(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (a mobileWarehouseAPI) registerDevice(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobileDeviceWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, created, err := a.repository.RegisterMobileDevice(r.Context(), s, inventoryrepo.RegisterMobileDevice{ID: newApprovalID(), IdempotencyKey: key, Label: strings.TrimSpace(input.Label), WarehouseID: strings.TrimSpace(input.WarehouseID), ZoneCode: strings.TrimSpace(input.ZoneCode), Kind: strings.TrimSpace(input.Kind), OperatorID: p.Subject}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, renderMobileDevice(item))
}

func (a mobileWarehouseAPI) deviceRoute(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, MobileWarehouseDevicesPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" || r.Method != http.MethodPost {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	var input mobileDeviceRevokeWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, err := a.repository.RevokeMobileDevice(r.Context(), s, parts[0], input.Version, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderMobileDevice(item))
}

func (a mobileWarehouseAPI) listOffline(w http.ResponseWriter, r *http.Request) {
	s, _, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := a.repository.ListMobileOfflineIntents(r.Context(), s, strings.TrimSpace(r.URL.Query().Get("device_id")), strings.TrimSpace(r.URL.Query().Get("state")), mobileLimit(r, 100, 200))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	result := make([]mobileIntentResponse, 0, len(items))
	for _, item := range items {
		result = append(result, renderMobileIntent(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (a mobileWarehouseAPI) replayOffline(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobileOfflineWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.IntentID) == "" {
		input.IntentID = newApprovalID()
	}
	if strings.TrimSpace(input.Kind) == "" {
		input.Kind = string(inventory.ScanProduct)
	}
	quantity, err := inventory.ParseDecimal(strings.TrimSpace(input.Quantity))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	task, err := a.repository.WMSTask(r.Context(), s, input.TaskID)
	if err != nil {
		writeWMSError(w, err)
		return
	}
	value, err := inventory.NewQuantity(quantity, task.ExpectedQuantity.Unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scan := inventory.MobileScanInput{TaskID: strings.TrimSpace(input.TaskID), DeviceID: strings.TrimSpace(input.DeviceID), Kind: inventory.MobileScanKind(strings.TrimSpace(input.Kind)), Code: input.Code, LocationCode: strings.TrimSpace(input.LocationCode), ExpectedVersion: input.Version, Quantity: value}
	if err := scan.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if _, err := a.repository.WMSScanTask(r.Context(), s, scan.TaskID, p.Subject, key, scan.Code, scan.LocationCode, value, scan.ExpectedVersion, inventoryMutation(p, r)); err != nil {
		writeWMSError(w, err)
		return
	}
	payloadDigest := inventory.MobileCodeDigest(strings.Join([]string{input.IntentID, input.TaskID, input.DeviceID, input.Kind, input.LocationCode, input.Quantity, input.Code}, "|"))
	item, _, err := a.repository.RecordMobileOfflineIntent(r.Context(), s, inventoryrepo.RecordMobileOfflineIntent{ID: input.IntentID, IdempotencyKey: key, DeviceID: input.DeviceID, PlanID: input.PlanID, Operation: inventory.MobileOperationScan, PayloadDigest: payloadDigest, SequenceNo: input.SequenceNo, State: "applied"}, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderMobileIntent(item))
}

func (a mobileWarehouseAPI) observe(w http.ResponseWriter, r *http.Request) {
	s, p, ok := inventoryRequestScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key, ok := mobileIdempotency(w, r)
	if !ok {
		return
	}
	var input mobileObservationWrite
	if !decodeMobile(w, r, &input) {
		return
	}
	item, err := a.repository.RecordMobileObservation(r.Context(), s, inventoryrepo.RecordMobileObservation{ID: newApprovalID(), IdempotencyKey: key, PlanID: strings.TrimSpace(input.PlanID), Stage: strings.TrimSpace(input.Stage), State: strings.TrimSpace(input.State), RemoteReferenceDigest: strings.TrimSpace(input.RemoteReferenceDigest), ObservedAt: mobileNowUTC()}, input.Version, mobileMutation(p, r))
	if err != nil {
		writeWMSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderMobilePlan(item))
}

func mobileMutation(p Principal, r *http.Request) inventory.Mutation {
	return inventory.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: p.Subject, Source: "api.mobile_warehouse", CorrelationID: firstMobileKey(r), OccurredAt: mobileNowUTC()}
}
func firstMobileKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("Idempotency-Key")); value != "" {
		return value
	}
	return newApprovalID()
}
func mobileNowUTC() (now time.Time) { return time.Now().UTC() }
func mobileIdempotency(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if value == "" {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return "", false
	}
	return value, true
}
func mobileLimit(r *http.Request, fallback, maximum int) int {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maximum {
		return -1
	}
	return parsed
}
func decodeMobile(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(value); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return false
	}
	return true
}

func renderMobilePlan(item inventoryrepo.MobilePlan) mobilePlanResponse {
	return mobilePlanResponse{ID: item.ID, OrderID: item.OrderID, WarehouseID: item.WarehouseID, Mode: string(item.Mode), Owner: string(item.Owner), Stage: item.Stage, State: item.State, LocalExecution: item.LocalExecution, RemoteReferenceDigest: item.RemoteReferenceDigest, Version: item.Version, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func renderMobileBatch(item inventoryrepo.MobilePickBatch) mobileBatchResponse {
	return mobileBatchResponse{ID: item.ID, PlanID: item.PlanID, WarehouseID: item.WarehouseID, Strategy: item.Strategy, State: item.State, RouteDigest: item.RouteDigest, TaskIDs: item.TaskIDs, Version: item.Version, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func renderMobileScan(item inventoryrepo.MobileScanEvidence) mobileScanResponse {
	return mobileScanResponse{ID: item.ID, PlanID: item.PlanID, TaskID: item.TaskID, DeviceID: item.DeviceID, Kind: string(item.Kind), CodeDigest: item.CodeDigest, LocationCode: item.LocationCode, Result: item.Result, Quantity: wmsQuantityResponse{Value: item.Quantity.Value.String(), Unit: item.Quantity.Unit.String()}, OccurredAt: item.OccurredAt.UTC().Format(time.RFC3339Nano)}
}
func renderMobilePack(item inventoryrepo.MobilePackSession) mobilePackResponse {
	return mobilePackResponse{ID: item.ID, PlanID: item.PlanID, BatchID: item.BatchID, PackagingType: item.PackagingType, State: item.State, FactsDigest: item.FactsDigest, PackageCount: item.Facts.PackageCount, WeightGrams: item.Facts.WeightGrams, LengthMM: item.Facts.LengthMM, WidthMM: item.Facts.WidthMM, HeightMM: item.Facts.HeightMM, Version: item.Version, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func renderMobilePrint(item inventoryrepo.MobilePrintJob) mobilePrintResponse {
	return mobilePrintResponse{ID: item.ID, PlanID: item.PlanID, PackID: item.PackID, PrinterID: item.PrinterID, Document: string(item.Document), TemplateVersion: item.TemplateVersion, MediaSize: item.MediaSize, Language: item.Language, Copies: item.Copies, State: item.State, Reprint: item.Reprint, ReasonCode: item.ReasonCode, ArtifactDigest: item.ArtifactDigest, ErrorCode: item.ErrorCode, Version: item.Version, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func renderMobileDevice(item inventoryrepo.MobileDevice) mobileDeviceResponse {
	var last *string
	if item.LastSeenAt != nil {
		value := item.LastSeenAt.UTC().Format(time.RFC3339Nano)
		last = &value
	}
	return mobileDeviceResponse{ID: item.ID, Label: item.Label, WarehouseID: item.WarehouseID, ZoneCode: item.ZoneCode, Kind: item.Kind, State: item.State, OperatorID: item.OperatorID, LastSeenAt: last, Version: item.Version, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func renderMobileIntent(item inventoryrepo.MobileOfflineIntent) mobileIntentResponse {
	return mobileIntentResponse{ID: item.ID, DeviceID: item.DeviceID, PlanID: item.PlanID, Operation: string(item.Operation), PayloadDigest: item.PayloadDigest, SequenceNo: item.SequenceNo, State: item.State, ErrorCode: item.ErrorCode, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
