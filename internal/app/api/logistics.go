package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	logisticsPickupPointsPath   = "/api/v1/logistics/pickup-points"
	logisticsBatchesPath        = "/api/v1/logistics/batches"
	logisticsBatchSubmitPath    = "/api/v1/logistics/batches/"
	logisticsBatchArchivePath   = "/api/v1/logistics/batches/archive/"
	logisticsRatesPath          = "/api/v1/logistics/rates"
	logisticsTrackingPath       = "/api/v1/logistics/tracking"
	logisticsLabelsPath         = "/api/v1/logistics/labels"
	logisticsShipmentCreatePath = "/api/v1/logistics/shipments"
	logisticsShipmentsPath      = "/api/v1/logistics/shipments/"
	logisticsSeparateReturnPath = "/api/v1/logistics/returns/separate"
)

var logisticsRemoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

// logisticsCapabilityStore is kept separate from the concrete PostgreSQL
// repository so the operation remains testable without a database.
type logisticsCapabilityStore interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type logisticsShipmentStore interface {
	BeginCreate(context.Context, tenancy.Scope, logistics.CreateCommand, logistics.Mutation) (logistics.Shipment, bool, error)
	BeginCancel(context.Context, tenancy.Scope, logistics.ShipmentID, string, logistics.Mutation) (logistics.Shipment, bool, error)
}

type logisticsApprovalStore interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
}

type logisticsRuntime interface {
	SupportsCapability(string, string) bool
	PickupPoints(context.Context, sdk.Account, sdk.Runtime, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
	LogisticsRates(context.Context, sdk.Account, sdk.Runtime, sdk.RateRequest) ([]sdk.RateQuote, error)
	LogisticsTracking(context.Context, sdk.Account, sdk.Runtime, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	LogisticsLabel(context.Context, sdk.Account, sdk.Runtime, sdk.LabelRequest) (sdk.LabelResult, error)
}

type logisticsBatchRuntime interface {
	LogisticsBatches(context.Context, sdk.Account, sdk.Runtime, sdk.LogisticsBatchQuery) ([]sdk.LogisticsBatch, error)
}

type logisticsBatchCreateRuntime interface {
	LogisticsBatchCreator(context.Context, sdk.Account, sdk.Runtime) (sdk.LogisticsBatchCreator, error)
}

type logisticsBatchSubmitRuntime interface {
	LogisticsBatchSubmitter(context.Context, sdk.Account, sdk.Runtime) (sdk.LogisticsBatchSubmitter, error)
}

type logisticsBatchArchiveRuntime interface {
	LogisticsBatchArchiver(context.Context, sdk.Account, sdk.Runtime) (sdk.LogisticsBatchArchiver, error)
}

type logisticsSeparateReturnRuntime interface {
	LogisticsSeparateReturnCreator(context.Context, sdk.Account, sdk.Runtime) (sdk.LogisticsSeparateReturnCreator, error)
}

type logisticsOperationStore interface {
	BeginOperation(context.Context, tenancy.Scope, string, string, [32]byte) (logistics.OperationReceipt, bool, error)
	CompleteOperation(context.Context, tenancy.Scope, string, string, json.RawMessage) error
}

type logisticsAPI struct {
	accounts   logisticsCapabilityStore
	secrets    secrets.SecretProvider
	runtime    logisticsRuntime
	shipments  logisticsShipmentStore
	approvals  logisticsApprovalStore
	operations logisticsOperationStore
}

type logisticsRouteDependency struct {
	shipments  logisticsShipmentStore
	approvals  logisticsApprovalStore
	operations logisticsOperationStore
}

type pickupPointView struct {
	RemoteID string `json:"remote_id"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	City     string `json:"city"`
	Address  string `json:"address"`
	Active   bool   `json:"active"`
}

type logisticsAddressInput struct {
	Country    string `json:"country"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
	Line1      string `json:"line1"`
}

type logisticsParcelInput struct {
	WeightGrams int64 `json:"weight_grams"`
	LengthMM    int64 `json:"length_mm"`
	WidthMM     int64 `json:"width_mm"`
	HeightMM    int64 `json:"height_mm"`
}

type logisticsRatesInput struct {
	ConnectorAccountID string                 `json:"connector_account_id"`
	From               logisticsAddressInput  `json:"from"`
	To                 logisticsAddressInput  `json:"to"`
	Parcels            []logisticsParcelInput `json:"parcels"`
}

type logisticsContactInput struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email"`
}

type logisticsShipmentCreateInput struct {
	ShipmentID         string                 `json:"shipment_id"`
	ConnectorAccountID string                 `json:"connector_account_id"`
	ExternalID         string                 `json:"external_id"`
	ServiceCode        string                 `json:"service_code"`
	From               logisticsAddressInput  `json:"from"`
	To                 logisticsAddressInput  `json:"to"`
	Parcels            []logisticsParcelInput `json:"parcels"`
	PickupPointRef     string                 `json:"pickup_point_ref"`
	Sender             logisticsContactInput  `json:"sender"`
	Recipient          logisticsContactInput  `json:"recipient"`
}

type logisticsRateView struct {
	OptionID      string    `json:"option_id"`
	Cost          moneyView `json:"cost"`
	MinDeliveryAt time.Time `json:"min_delivery_at"`
	MaxDeliveryAt time.Time `json:"max_delivery_at"`
	ObservedAt    time.Time `json:"observed_at"`
}

type logisticsTrackingView struct {
	RemoteID       string    `json:"remote_id"`
	Status         string    `json:"status"`
	TrackingNumber string    `json:"tracking_number,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

type logisticsLabelView struct {
	ArtifactRef string    `json:"artifact_ref"`
	MediaType   string    `json:"media_type"`
	ObservedAt  time.Time `json:"observed_at"`
}

type logisticsBatchView struct {
	RemoteID      string    `json:"remote_id"`
	Status        string    `json:"status"`
	ShipmentCount int       `json:"shipment_count"`
	ObservedAt    time.Time `json:"observed_at"`
}

type logisticsBatchSubmissionView struct {
	RemoteID   string    `json:"remote_id"`
	Status     string    `json:"status"`
	Accepted   bool      `json:"accepted"`
	ObservedAt time.Time `json:"observed_at"`
}

type logisticsBatchArchiveView struct {
	RemoteID   string    `json:"remote_id"`
	Status     string    `json:"status"`
	Archived   bool      `json:"archived"`
	ObservedAt time.Time `json:"observed_at"`
}

type logisticsBatchCreateInput struct {
	ConnectorAccountID string   `json:"connector_account_id"`
	OrderIDs           []string `json:"order_ids"`
	SendingDate        string   `json:"sending_date"`
	UseOnlineBalance   bool     `json:"use_online_balance"`
}

type logisticsBatchSubmitInput struct {
	ConnectorAccountID string `json:"connector_account_id"`
	UseOnlineBalance   bool   `json:"use_online_balance"`
}

type logisticsBatchArchiveInput struct {
	ConnectorAccountID string `json:"connector_account_id"`
}

type logisticsSeparateReturnInput struct {
	ConnectorAccountID string                 `json:"connector_account_id"`
	From               logisticsAddressInput  `json:"from"`
	To                 *logisticsAddressInput `json:"to"`
	InsuredValueMinor  int64                  `json:"insured_value_minor"`
	MailType           string                 `json:"mail_type"`
	OrderNumber        string                 `json:"order_number"`
	PostOfficeCode     string                 `json:"postoffice_code"`
	RecipientName      string                 `json:"recipient_name"`
	SenderName         string                 `json:"sender_name"`
}

type logisticsSeparateReturnView struct {
	RemoteID       string    `json:"remote_id"`
	Status         string    `json:"status"`
	TrackingNumber string    `json:"tracking_number"`
	ObservedAt     time.Time `json:"observed_at"`
}

func newLogisticsRoutes(accounts logisticsCapabilityStore, secretProvider secrets.SecretProvider, runtime logisticsRuntime, dependencies ...logisticsRouteDependency) []ProtectedRoute {
	api := logisticsAPI{accounts: accounts, secrets: secretProvider, runtime: runtime}
	if len(dependencies) > 0 {
		api.shipments = dependencies[0].shipments
		api.approvals = dependencies[0].approvals
		api.operations = dependencies[0].operations
	}
	routes := []ProtectedRoute{
		{Method: http.MethodGet, Path: logisticsPickupPointsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listPickupPoints)},
		{Method: http.MethodGet, Path: logisticsBatchesPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.listBatches)},
		{Method: http.MethodPost, Path: logisticsRatesPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.calculateRates)},
		{Method: http.MethodGet, Path: logisticsTrackingPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.readTracking)},
		{Method: http.MethodGet, Path: logisticsLabelsPath, Permission: "connectors.read", Handler: http.HandlerFunc(api.readLabel)},
	}
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: logisticsShipmentCreatePath, Permission: "logistics.shipment.create", Handler: http.HandlerFunc(api.createShipment)})
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: logisticsBatchesPath, Permission: "logistics.batches.create", Handler: http.HandlerFunc(api.createBatch)})
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: logisticsBatchSubmitPath, PathPrefix: true, Permission: "logistics.batches.submit", Handler: http.HandlerFunc(api.submitBatch)})
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: logisticsBatchArchivePath, PathPrefix: true, Permission: "logistics.batches.archive", Handler: http.HandlerFunc(api.archiveBatch)})
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: logisticsSeparateReturnPath, Permission: "logistics.return.separate.create", Handler: http.HandlerFunc(api.createSeparateReturn)})
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: logisticsShipmentsPath, PathPrefix: true, Permission: "logistics.shipment.cancel", Handler: http.HandlerFunc(api.cancelShipment)})
	return routes
}

func (api logisticsAPI) listBatches(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	batchRuntime, runtimeOK := api.runtime.(logisticsBatchRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Batch operation is unavailable")
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("connector_account_id"))
	mailType := strings.TrimSpace(r.URL.Query().Get("mail_type"))
	mailCategory := strings.TrimSpace(r.URL.Query().Get("mail_category"))
	limit, page := 50, 0
	for name, target := range map[string]*int{"limit": &limit, "page": &page} {
		if raw := strings.TrimSpace(r.URL.Query().Get(name)); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			*target = value
		}
	}
	query := sdk.LogisticsBatchQuery{MailType: mailType, MailCategory: mailCategory, Limit: limit, Page: page}
	if accountID == "" || query.Validate(100) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.batches.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Batch operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Batch capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	batches, err := batchRuntime.LogisticsBatches(r.Context(), account, runtime, query)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	if len(batches) > query.Limit {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	views := make([]logisticsBatchView, 0, len(batches))
	for _, batch := range batches {
		if batch.Validate() != nil {
			writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
			return
		}
		views = append(views, logisticsBatchView{RemoteID: batch.RemoteID, Status: batch.Status, ShipmentCount: batch.ShipmentCount, ObservedAt: batch.ObservedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api logisticsAPI) createBatch(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.accounts == nil || api.runtime == nil || api.operations == nil || api.approvals == nil || api.secrets == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	if !validIdempotencyKey(key) || !logisticsRemoteIDPattern.MatchString(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	var input logisticsBatchCreateInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	accountID := strings.TrimSpace(input.ConnectorAccountID)
	input.ConnectorAccountID = accountID
	request := input.toSDK(key)
	if request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if accountID == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.batches.create")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Batch formation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Batch formation capability is not enabled")
		return
	}
	digest, err := logisticsBatchCreateDigest(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	approvalResourceID := logisticsBatchApprovalResourceID(digest)
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "fulfillment.batch.create" || requested.ResourceType != "logistics_batch" || requested.ResourceID != approvalResourceID || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, "fulfillment.batch.create", key, digest)
	if err != nil {
		if errors.Is(err, logistics.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		if errors.Is(err, logistics.ErrInvalidRecord) || errors.Is(err, logistics.ErrInvalidScope) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !fresh {
		if receipt.State == "pending" {
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "pending": true, "fresh": false})
			return
		}
		var view logisticsBatchView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !logisticsBatchViewValid(view) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	creatorRuntime, runtimeOK := api.runtime.(logisticsBatchCreateRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Batch formation is unavailable")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	creator, err := creatorRuntime.LogisticsBatchCreator(r.Context(), account, runtime)
	if err != nil || creator == nil {
		writeLogisticsError(w, err)
		return
	}
	batch, err := creator.CreateLogisticsBatch(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	view := logisticsBatchView{RemoteID: batch.RemoteID, Status: batch.Status, ShipmentCount: batch.ShipmentCount, ObservedAt: batch.ObservedAt}
	if batch.Validate() != nil || !logisticsBatchViewValid(view) || batch.ShipmentCount != len(request.OrderIDs) {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	result, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, "fulfillment.batch.create", key, result) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (api logisticsAPI) submitBatch(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.accounts == nil || api.runtime == nil || api.operations == nil || api.approvals == nil || api.secrets == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, logisticsBatchSubmitPath), "/"), "/")
	if len(parts) != 2 || parts[1] != "submit" || !logisticsRemoteIDPattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	batchID := parts[0]
	if !validIdempotencyKey(key) || !logisticsRemoteIDPattern.MatchString(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	var input logisticsBatchSubmitInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	accountID := strings.TrimSpace(input.ConnectorAccountID)
	input.ConnectorAccountID = accountID
	request := input.toSDK(batchID, key)
	if request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if accountID == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.batches.submit")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Batch hand-off is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Batch hand-off capability is not enabled")
		return
	}
	digest, err := logisticsBatchSubmitDigest(input, batchID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "fulfillment.batch.submit" || requested.ResourceType != "logistics_batch" || requested.ResourceID != logisticsBatchSubmitApprovalResourceID(digest) || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	submitterRuntime, runtimeOK := api.runtime.(logisticsBatchSubmitRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Batch hand-off is unavailable")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	submitter, err := submitterRuntime.LogisticsBatchSubmitter(r.Context(), account, runtime)
	if err != nil || submitter == nil {
		writeLogisticsError(w, err)
		return
	}
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, "fulfillment.batch.submit", key, digest)
	if err != nil {
		if errors.Is(err, logistics.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		if errors.Is(err, logistics.ErrInvalidRecord) || errors.Is(err, logistics.ErrInvalidScope) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !fresh {
		if receipt.State == "pending" {
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "pending": true, "fresh": false})
			return
		}
		var view logisticsBatchSubmissionView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !logisticsBatchSubmissionViewValid(view) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	submission, err := submitter.SubmitLogisticsBatch(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	view := logisticsBatchSubmissionView{RemoteID: submission.RemoteID, Status: submission.Status, Accepted: submission.Accepted, ObservedAt: submission.ObservedAt}
	if submission.Validate() != nil || submission.RemoteID != batchID || !logisticsBatchSubmissionViewValid(view) {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	result, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, "fulfillment.batch.submit", key, result) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (api logisticsAPI) archiveBatch(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.accounts == nil || api.runtime == nil || api.operations == nil || api.approvals == nil || api.secrets == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, logisticsBatchArchivePath), "/"), "/")
	if len(parts) != 1 || !logisticsRemoteIDPattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	batchID := parts[0]
	if !validIdempotencyKey(key) || !logisticsRemoteIDPattern.MatchString(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	var input logisticsBatchArchiveInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	accountID := strings.TrimSpace(input.ConnectorAccountID)
	input.ConnectorAccountID = accountID
	request := input.toSDK(batchID, key)
	if accountID == "" || request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.batches.archive")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Batch archive is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Batch archive capability is not enabled")
		return
	}
	digest, err := logisticsBatchArchiveDigest(input, batchID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "fulfillment.batch.archive" || requested.ResourceType != "logistics_batch" || requested.ResourceID != logisticsBatchArchiveApprovalResourceID(digest) || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	archiverRuntime, runtimeOK := api.runtime.(logisticsBatchArchiveRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Batch archive is unavailable")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	archiver, err := archiverRuntime.LogisticsBatchArchiver(r.Context(), account, runtime)
	if err != nil || archiver == nil {
		writeLogisticsError(w, err)
		return
	}
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, "fulfillment.batch.archive", key, digest)
	if err != nil {
		if errors.Is(err, logistics.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		if errors.Is(err, logistics.ErrInvalidRecord) || errors.Is(err, logistics.ErrInvalidScope) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !fresh {
		if receipt.State == "pending" {
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "pending": true, "fresh": false})
			return
		}
		var view logisticsBatchArchiveView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !logisticsBatchArchiveViewValid(view) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	archive, err := archiver.ArchiveLogisticsBatch(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	view := logisticsBatchArchiveView{RemoteID: archive.RemoteID, Status: archive.Status, Archived: archive.Archived, ObservedAt: archive.ObservedAt}
	if archive.Validate() != nil || archive.RemoteID != batchID || !logisticsBatchArchiveViewValid(view) {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	result, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, "fulfillment.batch.archive", key, result) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (api logisticsAPI) createSeparateReturn(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.accounts == nil || api.runtime == nil || api.operations == nil || api.approvals == nil || api.secrets == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	if !validIdempotencyKey(key) || !logisticsRemoteIDPattern.MatchString(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	var input logisticsSeparateReturnInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	accountID := strings.TrimSpace(input.ConnectorAccountID)
	input.ConnectorAccountID = accountID
	request := input.toSDK(key)
	if accountID == "" || request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.return.separate.create")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Separate return creation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Separate return capability is not enabled")
		return
	}
	digest, err := logisticsSeparateReturnDigest(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "fulfillment.return.separate.create" || requested.ResourceType != "logistics_return" || requested.ResourceID != logisticsSeparateReturnApprovalResourceID(digest) || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	creatorRuntime, runtimeOK := api.runtime.(logisticsSeparateReturnRuntime)
	if !runtimeOK {
		writeProblem(w, http.StatusConflict, "Separate return creation is unavailable")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	creator, err := creatorRuntime.LogisticsSeparateReturnCreator(r.Context(), account, runtime)
	if err != nil || creator == nil {
		writeLogisticsError(w, err)
		return
	}
	receipt, fresh, err := api.operations.BeginOperation(r.Context(), scope, "fulfillment.return.separate.create", key, digest)
	if err != nil {
		if errors.Is(err, logistics.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		if errors.Is(err, logistics.ErrInvalidRecord) || errors.Is(err, logistics.ErrInvalidScope) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !fresh {
		if receipt.State == "pending" {
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "pending": true, "fresh": false})
			return
		}
		var view logisticsSeparateReturnView
		if receipt.State != "completed" || json.Unmarshal(receipt.Result, &view) != nil || !logisticsSeparateReturnViewValid(view) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	result, err := creator.CreateLogisticsSeparateReturn(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	view := logisticsSeparateReturnView{RemoteID: result.RemoteID, Status: result.Status, TrackingNumber: result.TrackingNumber, ObservedAt: result.ObservedAt}
	if result.RemoteID == "" || result.TrackingNumber != result.RemoteID || result.Cost.Validate() != nil || !logisticsSeparateReturnViewValid(view) {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	encoded, err := json.Marshal(view)
	if err != nil || api.operations.CompleteOperation(r.Context(), scope, "fulfillment.return.separate.create", key, encoded) != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (api logisticsAPI) createShipment(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.accounts == nil || api.runtime == nil || api.shipments == nil || api.approvals == nil || api.secrets == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	if !validIdempotencyKey(key) || !logisticsRemoteIDPattern.MatchString(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and Approval-Request-ID Required")
		return
	}
	var input logisticsShipmentCreateInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	shipmentID, err := logistics.ParseShipmentID(strings.TrimSpace(input.ShipmentID))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	request := input.toSDK(key)
	if request.Validate() != nil || len(request.Parcels) > 50 || !validLogisticsContact(request.Sender) || !validLogisticsContact(request.Recipient) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), strings.TrimSpace(input.ConnectorAccountID))
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.shipment.create")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Shipment creation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Shipment creation capability is not enabled")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "fulfillment.shipment.create" || requested.ResourceType != "shipment" || requested.ResourceID != shipmentID.String() || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	payload, err := json.Marshal(request)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	digest := sha256.Sum256(payload)
	metadata, err := api.secrets.Create(r.Context(), scope, secrets.ClassLogisticsShipment, payload)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	defer func() { wipeBytes(payload) }()
	shipment, fresh, err := api.shipments.BeginCreate(r.Context(), scope, logistics.CreateCommand{ID: shipmentID, AccountID: strings.TrimSpace(input.ConnectorAccountID), ExternalID: request.ExternalID, ServiceCode: request.ServiceCode, IdempotencyKey: key, PayloadReference: metadata.Reference.String(), PayloadDigest: hex.EncodeToString(digest[:])}, logistics.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: principal.Subject, Source: "api.logistics", CorrelationID: key, ApprovalRequestID: approvalID, OccurredAt: time.Now().UTC()})
	if err != nil {
		_, _ = api.secrets.Revoke(r.Context(), scope, metadata.Reference)
		switch {
		case errors.Is(err, logistics.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "Not Found")
		case errors.Is(err, logistics.ErrConflict), errors.Is(err, logistics.ErrInProgress):
			writeProblem(w, http.StatusConflict, "Conflict")
		default:
			writeProblem(w, http.StatusBadRequest, "Bad Request")
		}
		return
	}
	if !fresh {
		_, _ = api.secrets.Revoke(r.Context(), scope, metadata.Reference)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": shipment.ID.String(), "status": string(shipment.Status), "version": shipment.Version, "accepted": true, "fresh": fresh})
}

func (api logisticsAPI) cancelShipment(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if api.shipments == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if api.approvals == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	if !logisticsRemoteIDPattern.MatchString(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Approval-Request-ID Required")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, logisticsShipmentsPath), "/"), "/")
	if len(parts) != 2 || parts[1] != "cancel" || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	shipmentID, err := logistics.ParseShipmentID(parts[0])
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	requested, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || requested.Action != "fulfillment.shipment.cancel" || requested.ResourceType != "shipment" || requested.ResourceID != shipmentID.String() || requested.Risk != approval.RiskWriteSensitive || requested.State != approval.StateApproved {
		writeProblem(w, http.StatusConflict, "Approved matching request required")
		return
	}
	shipment, fresh, err := api.shipments.BeginCancel(r.Context(), scope, shipmentID, key, logistics.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: principal.Subject, Source: "api.logistics", CorrelationID: key, ApprovalRequestID: approvalID, OccurredAt: time.Now().UTC()})
	if err != nil {
		switch {
		case errors.Is(err, logistics.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "Not Found")
		case errors.Is(err, logistics.ErrConflict), errors.Is(err, logistics.ErrInProgress), errors.Is(err, logistics.ErrInvalidState):
			writeProblem(w, http.StatusConflict, "Conflict")
		case errors.Is(err, logistics.ErrInvalidRecord), errors.Is(err, logistics.ErrInvalidScope):
			writeProblem(w, http.StatusBadRequest, "Bad Request")
		default:
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": shipment.ID.String(), "status": string(shipment.Status), "version": shipment.Version, "accepted": true, "fresh": fresh})
}

func (api logisticsAPI) listPickupPoints(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("connector_account_id"))
	country := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country")))
	city := strings.TrimSpace(r.URL.Query().Get("city"))
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = value
	}
	query := sdk.PickupPointQuery{Country: country, City: city, Limit: limit}
	if accountID == "" || query.Validate(500) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("pickup.points.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Pickup-point operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Pickup-point capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	points, err := api.runtime.PickupPoints(r.Context(), account, runtime, query)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	views := make([]pickupPointView, 0, len(points))
	for _, point := range points {
		views = append(views, pickupPointView{RemoteID: point.RemoteID, Name: point.Name, Country: point.Country, City: point.City, Address: point.Address, Active: point.Active})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api logisticsAPI) calculateRates(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var input logisticsRatesInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	request := input.toSDK()
	accountID := strings.TrimSpace(input.ConnectorAccountID)
	if accountID == "" || request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.rates.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Rate operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Rate capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	quotes, err := api.runtime.LogisticsRates(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	views := make([]logisticsRateView, 0, len(quotes))
	for _, quote := range quotes {
		if quote.Validate() != nil {
			writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
			return
		}
		views = append(views, logisticsRateView{
			OptionID:      logisticsRateOptionID(account.ID, quote.ServiceCode),
			Cost:          moneyView{MinorUnits: quote.Cost.MinorUnits, Currency: quote.Cost.Currency},
			MinDeliveryAt: quote.MinDeliveryAt,
			MaxDeliveryAt: quote.MaxDeliveryAt,
			ObservedAt:    quote.ObservedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api logisticsAPI) readTracking(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("connector_account_id"))
	remoteID := strings.TrimSpace(r.URL.Query().Get("remote_id"))
	if accountID == "" || !logisticsRemoteIDPattern.MatchString(remoteID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.track.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Tracking operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Tracking capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	result, err := api.runtime.LogisticsTracking(r.Context(), account, runtime, sdk.ShipmentStatusRequest{RemoteID: remoteID})
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	if result.RemoteID == "" || len(result.RemoteID) > 192 || result.Status == "" || len(result.Status) > 64 || len(result.TrackingNumber) > 192 || result.ObservedAt.IsZero() {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	writeJSON(w, http.StatusOK, logisticsTrackingView{RemoteID: result.RemoteID, Status: result.Status, TrackingNumber: result.TrackingNumber, ObservedAt: result.ObservedAt})
}

func (api logisticsAPI) readLabel(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.accounts == nil || api.secrets == nil || api.runtime == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("connector_account_id"))
	remoteID := strings.TrimSpace(r.URL.Query().Get("remote_id"))
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "pdf"
	}
	request := sdk.LabelRequest{RemoteID: remoteID, Format: format}
	if accountID == "" || !logisticsRemoteIDPattern.MatchString(remoteID) || request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		writeProblem(w, http.StatusConflict, "Logistics connector account unavailable")
		return
	}
	const capability = sdk.Capability("logistics.label.read")
	if !supportsLogisticsCapability(api.runtime, account, capability) {
		writeProblem(w, http.StatusConflict, "Label operation is unavailable")
		return
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		writeProblem(w, http.StatusConflict, "Label capability is not enabled")
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	result, err := api.runtime.LogisticsLabel(r.Context(), account, runtime, request)
	if err != nil {
		writeLogisticsError(w, err)
		return
	}
	if result.ArtifactRef == "" || len(result.ArtifactRef) > 192 || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
		return
	}
	writeJSON(w, http.StatusOK, logisticsLabelView{ArtifactRef: result.ArtifactRef, MediaType: result.MediaType, ObservedAt: result.ObservedAt})
}

func (input logisticsRatesInput) toSDK() sdk.RateRequest {
	address := func(value logisticsAddressInput) sdk.Address {
		return sdk.Address{Country: strings.ToUpper(strings.TrimSpace(value.Country)), PostalCode: strings.TrimSpace(value.PostalCode), City: strings.TrimSpace(value.City), Line1: strings.TrimSpace(value.Line1)}
	}
	parcels := make([]sdk.Parcel, 0, len(input.Parcels))
	for _, parcel := range input.Parcels {
		parcels = append(parcels, sdk.Parcel{WeightGrams: parcel.WeightGrams, LengthMM: parcel.LengthMM, WidthMM: parcel.WidthMM, HeightMM: parcel.HeightMM})
	}
	return sdk.RateRequest{From: address(input.From), To: address(input.To), Parcels: parcels}
}

func (input logisticsShipmentCreateInput) toSDK(idempotencyKey string) sdk.ShipmentCreateRequest {
	address := func(value logisticsAddressInput) sdk.Address {
		return sdk.Address{Country: strings.ToUpper(strings.TrimSpace(value.Country)), PostalCode: strings.TrimSpace(value.PostalCode), City: strings.TrimSpace(value.City), Line1: strings.TrimSpace(value.Line1)}
	}
	parcels := make([]sdk.Parcel, 0, len(input.Parcels))
	for _, parcel := range input.Parcels {
		parcels = append(parcels, sdk.Parcel{WeightGrams: parcel.WeightGrams, LengthMM: parcel.LengthMM, WidthMM: parcel.WidthMM, HeightMM: parcel.HeightMM})
	}
	contact := func(value logisticsContactInput) sdk.LogisticsContact {
		return sdk.LogisticsContact{Name: strings.TrimSpace(value.Name), Phone: strings.TrimSpace(value.Phone), Email: strings.TrimSpace(value.Email)}
	}
	return sdk.ShipmentCreateRequest{ExternalID: strings.TrimSpace(input.ExternalID), ServiceCode: strings.TrimSpace(input.ServiceCode), IdempotencyKey: idempotencyKey, From: address(input.From), To: address(input.To), Parcels: parcels, PickupPointRef: strings.TrimSpace(input.PickupPointRef), Sender: contact(input.Sender), Recipient: contact(input.Recipient)}
}

func (input logisticsBatchCreateInput) toSDK(idempotencyKey string) sdk.LogisticsBatchCreateRequest {
	orderIDs := make([]string, len(input.OrderIDs))
	for index, orderID := range input.OrderIDs {
		orderIDs[index] = strings.TrimSpace(orderID)
	}
	return sdk.LogisticsBatchCreateRequest{OrderIDs: orderIDs, SendingDate: strings.TrimSpace(input.SendingDate), UseOnlineBalance: input.UseOnlineBalance, IdempotencyKey: idempotencyKey}
}

func (input logisticsBatchSubmitInput) toSDK(batchID, idempotencyKey string) sdk.LogisticsBatchSubmitRequest {
	return sdk.LogisticsBatchSubmitRequest{BatchID: strings.TrimSpace(batchID), UseOnlineBalance: input.UseOnlineBalance, IdempotencyKey: idempotencyKey}
}

func (input logisticsBatchArchiveInput) toSDK(batchID, idempotencyKey string) sdk.LogisticsBatchArchiveRequest {
	return sdk.LogisticsBatchArchiveRequest{BatchID: strings.TrimSpace(batchID), IdempotencyKey: idempotencyKey}
}

func (input logisticsSeparateReturnInput) toSDK(idempotencyKey string) sdk.LogisticsSeparateReturnRequest {
	address := func(value logisticsAddressInput) sdk.Address {
		return sdk.Address{Country: strings.ToUpper(strings.TrimSpace(value.Country)), PostalCode: strings.TrimSpace(value.PostalCode), City: strings.TrimSpace(value.City), Line1: strings.TrimSpace(value.Line1)}
	}
	var to *sdk.Address
	if input.To != nil {
		value := address(*input.To)
		to = &value
	}
	return sdk.LogisticsSeparateReturnRequest{
		From: address(input.From), To: to, InsuredValueMinor: input.InsuredValueMinor,
		MailType: strings.TrimSpace(input.MailType), OrderNumber: strings.TrimSpace(input.OrderNumber), PostOfficeCode: strings.TrimSpace(input.PostOfficeCode),
		RecipientName: strings.TrimSpace(input.RecipientName), SenderName: strings.TrimSpace(input.SenderName), IdempotencyKey: idempotencyKey,
	}
}

func logisticsBatchCreateDigest(input logisticsBatchCreateInput) ([32]byte, error) {
	orderIDs := make([]string, len(input.OrderIDs))
	for index, orderID := range input.OrderIDs {
		orderIDs[index] = strings.TrimSpace(orderID)
	}
	canonical := struct {
		ConnectorAccountID string   `json:"connector_account_id"`
		OrderIDs           []string `json:"order_ids"`
		SendingDate        string   `json:"sending_date,omitempty"`
		UseOnlineBalance   bool     `json:"use_online_balance"`
	}{ConnectorAccountID: strings.TrimSpace(input.ConnectorAccountID), OrderIDs: orderIDs, SendingDate: strings.TrimSpace(input.SendingDate), UseOnlineBalance: input.UseOnlineBalance}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

func logisticsBatchApprovalResourceID(digest [32]byte) string {
	return "batch:" + hex.EncodeToString(digest[:])
}

func logisticsBatchSubmitDigest(input logisticsBatchSubmitInput, batchID string) ([32]byte, error) {
	canonical := struct {
		ConnectorAccountID string `json:"connector_account_id"`
		BatchID            string `json:"batch_id"`
		UseOnlineBalance   bool   `json:"use_online_balance"`
	}{ConnectorAccountID: strings.TrimSpace(input.ConnectorAccountID), BatchID: strings.TrimSpace(batchID), UseOnlineBalance: input.UseOnlineBalance}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

func logisticsBatchSubmitApprovalResourceID(digest [32]byte) string {
	return "batch-submit:" + hex.EncodeToString(digest[:])
}

func logisticsBatchArchiveDigest(input logisticsBatchArchiveInput, batchID string) ([32]byte, error) {
	canonical := struct {
		ConnectorAccountID string `json:"connector_account_id"`
		BatchID            string `json:"batch_id"`
	}{ConnectorAccountID: strings.TrimSpace(input.ConnectorAccountID), BatchID: strings.TrimSpace(batchID)}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

func logisticsBatchArchiveApprovalResourceID(digest [32]byte) string {
	return "batch-archive:" + hex.EncodeToString(digest[:])
}

func logisticsSeparateReturnDigest(input logisticsSeparateReturnInput) ([32]byte, error) {
	canonical := struct {
		ConnectorAccountID string                 `json:"connector_account_id"`
		From               logisticsAddressInput  `json:"from"`
		To                 *logisticsAddressInput `json:"to,omitempty"`
		InsuredValueMinor  int64                  `json:"insured_value_minor"`
		MailType           string                 `json:"mail_type"`
		OrderNumber        string                 `json:"order_number,omitempty"`
		PostOfficeCode     string                 `json:"postoffice_code,omitempty"`
		RecipientName      string                 `json:"recipient_name"`
		SenderName         string                 `json:"sender_name"`
	}{
		ConnectorAccountID: strings.TrimSpace(input.ConnectorAccountID), From: input.From, To: input.To, InsuredValueMinor: input.InsuredValueMinor,
		MailType: strings.ToUpper(strings.TrimSpace(input.MailType)), OrderNumber: strings.TrimSpace(input.OrderNumber), PostOfficeCode: strings.TrimSpace(input.PostOfficeCode),
		RecipientName: strings.TrimSpace(input.RecipientName), SenderName: strings.TrimSpace(input.SenderName),
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

func logisticsSeparateReturnApprovalResourceID(digest [32]byte) string {
	return "separate-return:" + hex.EncodeToString(digest[:])
}

func logisticsBatchViewValid(view logisticsBatchView) bool {
	return logisticsRemoteIDPattern.MatchString(view.RemoteID) && safeLogisticsStatus(view.Status) && view.ShipmentCount >= 0 && view.ShipmentCount <= 1000000 && !view.ObservedAt.IsZero() && view.ObservedAt.Location() == time.UTC
}

func logisticsBatchSubmissionViewValid(view logisticsBatchSubmissionView) bool {
	return logisticsRemoteIDPattern.MatchString(view.RemoteID) && safeLogisticsStatus(view.Status) && view.Accepted && !view.ObservedAt.IsZero() && view.ObservedAt.Location() == time.UTC
}

func logisticsBatchArchiveViewValid(view logisticsBatchArchiveView) bool {
	return logisticsRemoteIDPattern.MatchString(view.RemoteID) && view.Status == "ARCHIVED" && view.Archived && !view.ObservedAt.IsZero() && view.ObservedAt.Location() == time.UTC
}

func logisticsSeparateReturnViewValid(view logisticsSeparateReturnView) bool {
	return logisticsRemoteIDPattern.MatchString(view.RemoteID) && safeLogisticsStatus(view.Status) && view.Status == "created" && view.TrackingNumber == view.RemoteID && !view.ObservedAt.IsZero() && view.ObservedAt.Location() == time.UTC
}

func safeLogisticsStatus(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, symbol := range value {
		if (symbol >= 'A' && symbol <= 'Z') || (symbol >= 'a' && symbol <= 'z') || (symbol >= '0' && symbol <= '9') || symbol == '.' || symbol == '_' || symbol == ':' || symbol == '-' {
			continue
		}
		return false
	}
	return true
}

func validLogisticsContact(contact sdk.LogisticsContact) bool {
	return strings.TrimSpace(contact.Name) != "" && strings.TrimSpace(contact.Name) == contact.Name && len(contact.Name) <= 255 && strings.TrimSpace(contact.Phone) != "" && strings.TrimSpace(contact.Phone) == contact.Phone && len(contact.Phone) <= 32 && strings.TrimSpace(contact.Email) == contact.Email && len(contact.Email) <= 254 && !strings.ContainsAny(contact.Email, "\r\n\t ")
}

func logisticsRateOptionID(accountID, serviceCode string) string {
	digest := sha256.Sum256([]byte(accountID + "\x00" + serviceCode))
	return "option-" + hex.EncodeToString(digest[:12])
}

// supportsLogisticsCapability keeps the route capability-driven without
// making the provider identity part of an authorization branch. The runtime
// registry owns the connector lookup; this API only asks whether the declared
// capability has an executable route.
func supportsLogisticsCapability(runtime logisticsRuntime, account sdk.Account, capability sdk.Capability) bool {
	if runtime == nil {
		return false
	}
	return runtime.SupportsCapability(account.ConnectorID, string(capability))
}

func writeLogisticsError(w http.ResponseWriter, err error) {
	var remote *sdk.RemoteError
	switch {
	case errors.Is(err, builtinruntime.ErrUnavailable), errors.Is(err, builtinruntime.ErrConfigurationNeeded):
		writeProblem(w, http.StatusConflict, "Logistics operation unavailable")
	case errors.Is(err, sdk.ErrInvalidLogisticsRequest), errors.Is(err, sdk.ErrInvalidPickupRequest):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.As(err, &remote) && remote.Category == sdk.ErrorInvalidRequest:
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	default:
		writeProblem(w, http.StatusBadGateway, "Logistics provider unavailable")
	}
}
