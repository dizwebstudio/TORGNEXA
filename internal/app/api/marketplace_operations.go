package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/integrationcenter"
	"github.com/torgnexa/torgnexa/internal/core/marketplaceoperations"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	MarketplaceOperationsPath        = "/api/v1/marketplace-operations"
	MarketplaceOperationsAccountPath = MarketplaceOperationsPath + "/"
	MarketplaceOperationFlowsPath    = MarketplaceOperationsPath + "/flows"
	MarketplaceOperationFindingsPath = MarketplaceOperationsPath + "/findings"
)

type marketplaceOperationsReadRequest struct {
	Cursor    string
	Limit     int
	AccountID string
}

type marketplaceOperationsReadResult struct {
	Items       []marketplaceoperations.AccountMatrix
	NextCursor  string
	Partial     bool
	GeneratedAt time.Time
}

type marketplaceOperationsReader interface {
	ReadMarketplaceOperations(context.Context, tenancy.Scope, marketplaceOperationsReadRequest) (marketplaceOperationsReadResult, error)
}

type marketplaceOperationFlowReader interface {
	List(context.Context, tenancy.Scope, string, int) (marketplaceoperations.FlowPage, error)
}

type marketplaceOperationFlowDetailReader interface {
	Flow(context.Context, tenancy.Scope, string) (marketplaceoperations.Flow, error)
	ListCommands(context.Context, tenancy.Scope, string, int) ([]marketplaceoperations.CommandRecord, error)
}

type marketplaceOperationFlowStore interface {
	marketplaceOperationFlowReader
	Create(context.Context, tenancy.Scope, marketplaceoperations.Flow) error
	Flow(context.Context, tenancy.Scope, string) (marketplaceoperations.Flow, error)
	Apply(context.Context, tenancy.Scope, string, marketplaceoperations.Command) (marketplaceoperations.Flow, bool, error)
}

type marketplaceOperationFindingStore interface {
	marketplaceoperations.FindingRepository
}

// marketplaceOperationsSource projects the authoritative integration center
// into the marketplace-specific matrix. No remote call or secret lookup is
// performed by this read path.
type marketplaceOperationsSource struct {
	center integrationCenterReader
}

func (source marketplaceOperationsSource) ReadMarketplaceOperations(ctx context.Context, scope tenancy.Scope, request marketplaceOperationsReadRequest) (marketplaceOperationsReadResult, error) {
	if source.center == nil {
		return marketplaceOperationsReadResult{}, nil
	}
	result, err := source.center.Read(ctx, scope, integrationCenterReadRequest{
		Cursor: request.Cursor, Limit: request.Limit, Family: string(sdk.FamilyMarketplace), AccountID: request.AccountID,
	})
	if err != nil {
		return marketplaceOperationsReadResult{}, err
	}
	items := make([]marketplaceoperations.AccountMatrix, 0, len(result.Rows))
	for _, row := range result.Rows {
		items = append(items, marketplaceoperations.Evaluate(marketplaceoperations.AccountInput{
			AccountID: row.AccountID, ConnectorID: row.ConnectorID, DisplayName: row.DisplayName,
			AccountStatus: row.Dimensions.Account.Status, HealthStatus: row.Dimensions.Health.Status,
			CredentialState: row.Dimensions.Credential.Status, RuntimeStatus: row.Dimensions.Runtime.Status,
			Partial:      row.Partial,
			Capabilities: marketplaceCapabilities(row.Capabilities),
		}))
	}
	return marketplaceOperationsReadResult{Items: items, NextCursor: result.NextCursor, Partial: result.Partial, GeneratedAt: result.GeneratedAt}, nil
}

func marketplaceCapabilities(values []integrationcenter.Capability) []marketplaceoperations.Capability {
	result := make([]marketplaceoperations.Capability, 0, len(values))
	for _, value := range values {
		result = append(result, marketplaceoperations.Capability{Name: value.Name, Status: string(value.Status), Enabled: value.Status == integrationcenter.CapabilityEnabled})
	}
	return result
}

func newMarketplaceOperationsRoutes(reader marketplaceOperationsReader, flowReaders ...marketplaceOperationFlowReader) []ProtectedRoute {
	list := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationsList(w, r, reader) })
	detail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationsDetail(w, r, reader) })
	routes := []ProtectedRoute{
		{Method: http.MethodGet, Path: MarketplaceOperationsPath, Permission: "integrations.center.read", Handler: list},
		{Method: http.MethodGet, Path: MarketplaceOperationsAccountPath, PathPrefix: true, Permission: "integrations.center.read", Handler: detail},
	}
	var flowReader marketplaceOperationFlowReader
	if len(flowReaders) > 0 {
		flowReader = flowReaders[0]
	}
	routes = append(routes, ProtectedRoute{Method: http.MethodGet, Path: MarketplaceOperationFlowsPath, Permission: "integrations.center.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationFlowsList(w, r, flowReader) })})
	var flowStore marketplaceOperationFlowStore
	if candidate, ok := flowReader.(marketplaceOperationFlowStore); ok {
		flowStore = candidate
	}
	var flowDetailReader marketplaceOperationFlowDetailReader
	if candidate, ok := flowReader.(marketplaceOperationFlowDetailReader); ok {
		flowDetailReader = candidate
	}
	var findingStore marketplaceOperationFindingStore
	if candidate, ok := flowReader.(marketplaceOperationFindingStore); ok {
		findingStore = candidate
	}
	routes = append(routes,
		ProtectedRoute{Method: http.MethodPost, Path: MarketplaceOperationFlowsPath, Permission: "marketplace.operations.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationFlowCreate(w, r, flowStore) })},
		ProtectedRoute{Method: http.MethodPost, Path: MarketplaceOperationFlowsPath + "/", PathPrefix: true, Permission: "marketplace.operations.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationFlowCommand(w, r, flowStore) })},
		ProtectedRoute{Method: http.MethodGet, Path: MarketplaceOperationFindingsPath, Permission: "integrations.center.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationFindingsList(w, r, findingStore) })},
		ProtectedRoute{Method: http.MethodGet, Path: MarketplaceOperationFlowsPath + "/", PathPrefix: true, Permission: "integrations.center.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			marketplaceOperationFlowSubroute(w, r, flowDetailReader, findingStore)
		})},
		ProtectedRoute{Method: http.MethodPost, Path: MarketplaceOperationFindingsPath + "/", PathPrefix: true, Permission: "marketplace.operations.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { marketplaceOperationFindingAction(w, r, findingStore) })},
	)
	return routes
}

type marketplaceOperationsResponse struct {
	SnapshotVersion int                                   `json:"snapshot_version"`
	GeneratedAt     time.Time                             `json:"generated_at"`
	Partial         bool                                  `json:"partial"`
	Consistency     string                                `json:"consistency"`
	Items           []marketplaceoperations.AccountMatrix `json:"items"`
	NextCursor      string                                `json:"next_cursor,omitempty"`
}

func marketplaceOperationsList(w http.ResponseWriter, r *http.Request, reader marketplaceOperationsReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || reader == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	request, valid := parseMarketplaceOperationsRequest(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	result, err := reader.ReadMarketplaceOperations(r.Context(), scope, request)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationsResponse{SnapshotVersion: 1, GeneratedAt: result.GeneratedAt, Partial: result.Partial, Consistency: "best_effort", Items: result.Items, NextCursor: result.NextCursor})
}

func marketplaceOperationsDetail(w http.ResponseWriter, r *http.Request, reader marketplaceOperationsReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || reader == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	accountID := strings.TrimPrefix(r.URL.Path, MarketplaceOperationsAccountPath)
	if accountID == "" || strings.Contains(accountID, "/") || len(accountID) > 128 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	result, err := reader.ReadMarketplaceOperations(r.Context(), scope, marketplaceOperationsReadRequest{Limit: 1, AccountID: accountID})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if len(result.Items) != 1 || result.Items[0].AccountID != accountID {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	writeJSON(w, http.StatusOK, result.Items[0])
}

func parseMarketplaceOperationsRequest(r *http.Request) (marketplaceOperationsReadRequest, bool) {
	values := r.URL.Query()
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return marketplaceOperationsReadRequest{}, false
		}
		limit = parsed
	}
	if limit < 1 || limit > 100 {
		return marketplaceOperationsReadRequest{}, false
	}
	cursor := values.Get("cursor")
	if len(cursor) > 256 || strings.ContainsAny(cursor, "\r\n") {
		return marketplaceOperationsReadRequest{}, false
	}
	accountID := values.Get("account_id")
	if len(accountID) > 128 || strings.ContainsAny(accountID, "\r\n") {
		return marketplaceOperationsReadRequest{}, false
	}
	return marketplaceOperationsReadRequest{Cursor: cursor, Limit: limit, AccountID: accountID}, true
}

type marketplaceOperationFlowsResponse struct {
	SnapshotVersion int                          `json:"snapshot_version"`
	GeneratedAt     time.Time                    `json:"generated_at"`
	Consistency     string                       `json:"consistency"`
	Items           []marketplaceoperations.Flow `json:"items"`
	NextCursor      string                       `json:"next_cursor,omitempty"`
}

func marketplaceOperationFlowsList(w http.ResponseWriter, r *http.Request, reader marketplaceOperationFlowReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || reader == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	request, valid := parseMarketplaceOperationsRequest(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	page, err := reader.List(r.Context(), scope, request.Cursor, request.Limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFlowsResponse{SnapshotVersion: 1, GeneratedAt: time.Now().UTC(), Consistency: "atomic", Items: page.Items, NextCursor: page.NextCursor})
}

type marketplaceOperationFlowCreateRequest struct {
	ID         string                            `json:"id,omitempty"`
	AccountID  string                            `json:"account_id"`
	StartStage marketplaceoperations.FlowStage   `json:"start_stage,omitempty"`
	References []marketplaceoperations.Reference `json:"references,omitempty"`
}

type marketplaceOperationFlowCreateResponse struct {
	Flow     marketplaceoperations.Flow `json:"flow"`
	Replayed bool                       `json:"replayed"`
}

func marketplaceOperationFlowCreate(w http.ResponseWriter, r *http.Request, store marketplaceOperationFlowStore) {
	scope, scoped := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scoped || store == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated tenant are required")
		return
	}
	var input marketplaceOperationFlowCreateRequest
	if decodeStrictJSON(r, &input) != nil || strings.TrimSpace(input.AccountID) == "" || len(input.AccountID) > 192 || strings.ContainsAny(input.AccountID, "\r\n/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	flowID := stableID("mkt_flow_", 40, scope, key)
	if suppliedID := strings.TrimSpace(input.ID); suppliedID != "" && suppliedID != flowID {
		writeProblem(w, http.StatusBadRequest, "id must be derived from Idempotency-Key")
		return
	}
	if len(flowID) > 192 || strings.ContainsAny(flowID, "\r\n/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	startStage := input.StartStage
	if startStage == "" {
		startStage = marketplaceoperations.StageAccount
	}
	flow, err := marketplaceoperations.NewAtStage(flowID, scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID, startStage, input.References, time.Now().UTC())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if err = store.Create(r.Context(), scope, flow); err == nil {
		writeJSON(w, http.StatusCreated, marketplaceOperationFlowCreateResponse{Flow: flow})
		return
	}
	if !errors.Is(err, marketplaceoperations.ErrFlowConflict) {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	existing, lookupErr := store.Flow(r.Context(), scope, flowID)
	if lookupErr != nil || existing.AccountID != flow.AccountID {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFlowCreateResponse{Flow: existing, Replayed: true})
}

type marketplaceOperationFlowDetailResponse struct {
	Flow     marketplaceoperations.Flow            `json:"flow"`
	Timeline []marketplaceoperations.CommandRecord `json:"timeline"`
}

func marketplaceOperationFlowSubroute(w http.ResponseWriter, r *http.Request, detail marketplaceOperationFlowDetailReader, findings marketplaceOperationFindingStore) {
	pathTail := strings.TrimPrefix(r.URL.Path, MarketplaceOperationFlowsPath+"/")
	parts := strings.Split(pathTail, "/")
	if len(parts) == 1 {
		marketplaceOperationFlowDetail(w, r, detail, parts[0])
		return
	}
	marketplaceOperationFlowFindings(w, r, findings)
}

func marketplaceOperationFlowDetail(w http.ResponseWriter, r *http.Request, reader marketplaceOperationFlowDetailReader, flowID string) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || reader == nil || flowID == "" || len(flowID) > 192 || strings.ContainsAny(flowID, "\r\n/") {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	flow, err := reader.Flow(r.Context(), scope, flowID)
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	timeline, err := reader.ListCommands(r.Context(), scope, flowID, 200)
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFlowDetailResponse{Flow: flow, Timeline: timeline})
}

type marketplaceOperationFlowCommandRequest struct {
	OperationID string                            `json:"operation_id"`
	Stage       marketplaceoperations.FlowStage   `json:"stage"`
	Outcome     marketplaceoperations.Outcome     `json:"outcome"`
	ReasonCode  string                            `json:"reason_code,omitempty"`
	References  []marketplaceoperations.Reference `json:"references,omitempty"`
	OccurredAt  time.Time                         `json:"occurred_at"`
}

type marketplaceOperationFlowCommandResponse struct {
	Flow      marketplaceoperations.Flow `json:"flow"`
	Duplicate bool                       `json:"duplicate"`
}

func marketplaceOperationFlowCommand(w http.ResponseWriter, r *http.Request, store marketplaceOperationFlowStore) {
	scope, scoped := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	pathTail := strings.TrimPrefix(r.URL.Path, MarketplaceOperationFlowsPath+"/")
	parts := strings.Split(pathTail, "/")
	if !scoped || store == nil || !validIdempotencyKey(key) || len(parts) != 2 || parts[0] == "" || parts[1] != "commands" || len(parts[0]) > 192 || strings.ContainsAny(parts[0], "\r\n/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input marketplaceOperationFlowCommandRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	command := marketplaceoperations.Command{OperationID: input.OperationID, IdempotencyKey: key, Stage: input.Stage, Outcome: input.Outcome, ReasonCode: input.ReasonCode, References: input.References, OccurredAt: input.OccurredAt}
	if command.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	flow, duplicate, err := store.Apply(r.Context(), scope, parts[0], command)
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFlowCommandResponse{Flow: flow, Duplicate: duplicate})
}

func writeMarketplaceOperationFlowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplaceoperations.ErrInvalidFlow), errors.Is(err, marketplaceoperations.ErrMissingReference), errors.Is(err, marketplaceoperations.ErrInvalidFinding):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, marketplaceoperations.ErrFlowNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, marketplaceoperations.ErrDuplicateConflict), errors.Is(err, marketplaceoperations.ErrFlowConflict), errors.Is(err, marketplaceoperations.ErrFindingConflict), errors.Is(err, marketplaceoperations.ErrInvalidTransition):
		writeProblem(w, http.StatusConflict, "Conflict")
	default:
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
	}
}

type marketplaceOperationFindingsResponse struct {
	SnapshotVersion int                             `json:"snapshot_version"`
	GeneratedAt     time.Time                       `json:"generated_at"`
	Consistency     string                          `json:"consistency"`
	Items           []marketplaceoperations.Finding `json:"items"`
	NextCursor      string                          `json:"next_cursor,omitempty"`
}

func parseMarketplaceOperationFindingQuery(r *http.Request) (marketplaceoperations.FindingQuery, bool) {
	base, valid := parseMarketplaceOperationsRequest(r)
	if !valid {
		return marketplaceoperations.FindingQuery{}, false
	}
	query := marketplaceoperations.FindingQuery{Cursor: base.Cursor, Limit: base.Limit, FlowID: r.URL.Query().Get("flow_id"), Status: marketplaceoperations.FindingStatus(r.URL.Query().Get("status"))}
	return query, query.Validate() == nil
}

func marketplaceOperationFindingsList(w http.ResponseWriter, r *http.Request, store marketplaceOperationFindingStore) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || store == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	query, valid := parseMarketplaceOperationFindingQuery(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	page, err := store.ListFindings(r.Context(), scope, query)
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFindingsResponse{SnapshotVersion: 1, GeneratedAt: time.Now().UTC(), Consistency: "atomic", Items: page.Items, NextCursor: page.NextCursor})
}

func marketplaceOperationFlowFindings(w http.ResponseWriter, r *http.Request, store marketplaceOperationFindingStore) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || store == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	pathTail := strings.TrimPrefix(r.URL.Path, MarketplaceOperationFlowsPath+"/")
	parts := strings.Split(pathTail, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "findings" || len(parts[0]) > 192 || strings.ContainsAny(parts[0], "\r\n/") {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	query, valid := parseMarketplaceOperationFindingQuery(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	query.FlowID = parts[0]
	page, err := store.ListFindings(r.Context(), scope, query)
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFindingsResponse{SnapshotVersion: 1, GeneratedAt: time.Now().UTC(), Consistency: "atomic", Items: page.Items, NextCursor: page.NextCursor})
}

type marketplaceOperationFindingActionRequest struct {
	Action marketplaceoperations.FindingActionKind `json:"action"`
}

type marketplaceOperationFindingActionResponse struct {
	Action    marketplaceoperations.FindingAction `json:"action"`
	Finding   marketplaceoperations.Finding       `json:"finding"`
	Duplicate bool                                `json:"duplicate"`
}

func marketplaceOperationFindingAction(w http.ResponseWriter, r *http.Request, store marketplaceOperationFindingStore) {
	scope, scoped := ScopeFromContext(r.Context())
	principal, identified := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	pathTail := strings.TrimPrefix(r.URL.Path, MarketplaceOperationFindingsPath+"/")
	parts := strings.Split(pathTail, "/")
	if !scoped || !identified || store == nil || !validIdempotencyKey(key) || len(parts) != 2 || parts[0] == "" || parts[1] != "actions" || len(parts[0]) > 192 || strings.ContainsAny(parts[0], "\r\n/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input marketplaceOperationFindingActionRequest
	if decodeStrictJSON(r, &input) != nil || !input.Action.Valid() {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if strings.TrimSpace(principal.Subject) == "" {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	action := marketplaceoperations.FindingAction{ID: stableID("mfa_", 32, scope, key), FindingID: parts[0], Action: input.Action, IdempotencyKey: key, ActorID: principal.Subject, OccurredAt: time.Now().UTC()}
	if action.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	created, duplicate, err := store.ApplyFindingAction(r.Context(), scope, parts[0], action)
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	finding, err := store.Finding(r.Context(), scope, parts[0])
	if err != nil {
		writeMarketplaceOperationFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marketplaceOperationFindingActionResponse{Action: created, Finding: finding, Duplicate: duplicate})
}
