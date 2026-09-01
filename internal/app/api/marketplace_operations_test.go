package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/integrationcenter"
	"github.com/torgnexa/torgnexa/internal/core/marketplaceoperations"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

func TestMarketplaceOperationsListProjectsOnlyMarketplaceAccounts(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	marketplace, err := integrationcenter.Reduce(integrationcenter.Input{
		AccountID: "marketplace-account", ConnectorID: "marketplace-a", Family: "marketplace", Surface: "integrations", Version: 1,
		Dimensions: integrationcenterTestDimensions(now), Capabilities: []integrationcenter.Capability{{Name: "orders.read", Direction: "read", Risk: "read", Status: integrationcenter.CapabilityEnabled}}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := marketplaceOperationsSource{center: &centerReaderStub{result: integrationCenterReadResult{Rows: []integrationcenter.Snapshot{marketplace}, GeneratedAt: now}}}
	req := httptest.NewRequest(http.MethodGet, MarketplaceOperationsPath+"?limit=1", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestScopeKey{}, validTestScope(t)))
	res := httptest.NewRecorder()
	marketplaceOperationsList(res, req, reader)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body marketplaceOperationsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].AccountID != "marketplace-account" {
		t.Fatalf("body=%+v", body)
	}
	if body.Items[0].Operations == nil || strings.Contains(res.Body.String(), "Authorization") {
		t.Fatalf("response contains an invalid or sensitive projection: %s", res.Body.String())
	}
}

func TestMarketplaceOperationsDetailRejectsUnsafePathAndMissingScope(t *testing.T) {
	reader := marketplaceOperationsSource{center: &centerReaderStub{result: integrationCenterReadResult{}}}
	unsafe := httptest.NewRequest(http.MethodGet, MarketplaceOperationsAccountPath+"account-1/extra", nil).WithContext(context.WithValue(context.Background(), requestScopeKey{}, validTestScope(t)))
	unsafeResult := httptest.NewRecorder()
	marketplaceOperationsDetail(unsafeResult, unsafe, reader)
	if unsafeResult.Code != http.StatusBadRequest {
		t.Fatalf("unsafe path status=%d", unsafeResult.Code)
	}
	missingScope := httptest.NewRequest(http.MethodGet, MarketplaceOperationsPath, nil)
	missingResult := httptest.NewRecorder()
	marketplaceOperationsList(missingResult, missingScope, reader)
	if missingResult.Code != http.StatusForbidden {
		t.Fatalf("missing scope status=%d", missingResult.Code)
	}
}

var _ marketplaceOperationsReader = marketplaceOperationsSource{}

type marketplaceFlowReaderStub struct {
	page marketplaceoperations.FlowPage
}

func (stub marketplaceFlowReaderStub) List(context.Context, tenancy.Scope, string, int) (marketplaceoperations.FlowPage, error) {
	return stub.page, nil
}

func TestMarketplaceOperationFlowsListIsTenantScopedAndRedacted(t *testing.T) {
	scope := validTestScope(t)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	flow, err := marketplaceoperations.New("flow-1", scope.OrganizationID().String(), scope.WorkspaceID().String(), "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	reader := marketplaceFlowReaderStub{page: marketplaceoperations.FlowPage{Items: []marketplaceoperations.Flow{flow}}}
	req := httptest.NewRequest(http.MethodGet, MarketplaceOperationFlowsPath+"?limit=1", nil).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	res := httptest.NewRecorder()
	marketplaceOperationFlowsList(res, req, reader)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "token") || strings.Contains(res.Body.String(), "Authorization") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body marketplaceOperationFlowsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].AccountID != "account-1" || body.Consistency != "atomic" {
		t.Fatalf("unexpected flow response: %+v", body)
	}
}

type marketplaceFlowStoreStub struct {
	flows    map[string]marketplaceoperations.Flow
	findings map[string]marketplaceoperations.Finding
	actions  map[string]marketplaceoperations.FindingAction
	timeline map[string][]marketplaceoperations.CommandRecord
}

func (stub *marketplaceFlowStoreStub) List(context.Context, tenancy.Scope, string, int) (marketplaceoperations.FlowPage, error) {
	return marketplaceoperations.FlowPage{}, nil
}

func (stub *marketplaceFlowStoreStub) Create(_ context.Context, _ tenancy.Scope, flow marketplaceoperations.Flow) error {
	if stub.flows == nil {
		stub.flows = map[string]marketplaceoperations.Flow{}
	}
	if _, exists := stub.flows[flow.ID]; exists {
		return marketplaceoperations.ErrFlowConflict
	}
	stub.flows[flow.ID] = flow
	return nil
}

func (stub *marketplaceFlowStoreStub) Flow(_ context.Context, _ tenancy.Scope, id string) (marketplaceoperations.Flow, error) {
	flow, ok := stub.flows[id]
	if !ok {
		return marketplaceoperations.Flow{}, marketplaceoperations.ErrFlowNotFound
	}
	return flow, nil
}

func (stub *marketplaceFlowStoreStub) ListCommands(_ context.Context, _ tenancy.Scope, flowID string, _ int) ([]marketplaceoperations.CommandRecord, error) {
	return append([]marketplaceoperations.CommandRecord(nil), stub.timeline[flowID]...), nil
}

func (stub *marketplaceFlowStoreStub) Apply(_ context.Context, _ tenancy.Scope, id string, command marketplaceoperations.Command) (marketplaceoperations.Flow, bool, error) {
	flow, err := stub.Flow(context.Background(), tenancy.Scope{}, id)
	if err != nil {
		return marketplaceoperations.Flow{}, false, err
	}
	updated, duplicate, err := marketplaceoperations.Apply(flow, command)
	if err != nil {
		return marketplaceoperations.Flow{}, false, err
	}
	if !duplicate {
		stub.flows[id] = updated
	}
	return updated, duplicate, nil
}

func (stub *marketplaceFlowStoreStub) RecordFinding(_ context.Context, _ tenancy.Scope, finding marketplaceoperations.Finding) error {
	if stub.findings == nil {
		stub.findings = map[string]marketplaceoperations.Finding{}
	}
	if _, exists := stub.findings[finding.ID]; exists {
		return marketplaceoperations.ErrFindingConflict
	}
	stub.findings[finding.ID] = finding
	return nil
}

func (stub *marketplaceFlowStoreStub) Finding(_ context.Context, _ tenancy.Scope, id string) (marketplaceoperations.Finding, error) {
	finding, ok := stub.findings[id]
	if !ok {
		return marketplaceoperations.Finding{}, marketplaceoperations.ErrFlowNotFound
	}
	for _, action := range stub.actions {
		if action.FindingID == id && action.Action == marketplaceoperations.FindingActionResolve {
			finding.Status = marketplaceoperations.FindingResolved
		}
	}
	return finding, nil
}

func (stub *marketplaceFlowStoreStub) ListFindings(_ context.Context, _ tenancy.Scope, query marketplaceoperations.FindingQuery) (marketplaceoperations.FindingPage, error) {
	if query.Validate() != nil {
		return marketplaceoperations.FindingPage{}, marketplaceoperations.ErrInvalidFinding
	}
	items := make([]marketplaceoperations.Finding, 0, len(stub.findings))
	for _, finding := range stub.findings {
		current, _ := stub.Finding(context.Background(), tenancy.Scope{}, finding.ID)
		if query.FlowID != "" && current.FlowID != query.FlowID {
			continue
		}
		if query.Status != "" && current.Status != query.Status {
			continue
		}
		items = append(items, current)
	}
	return marketplaceoperations.FindingPage{Items: items}, nil
}

func (stub *marketplaceFlowStoreStub) ApplyFindingAction(_ context.Context, _ tenancy.Scope, findingID string, action marketplaceoperations.FindingAction) (marketplaceoperations.FindingAction, bool, error) {
	if _, err := stub.Finding(context.Background(), tenancy.Scope{}, findingID); err != nil {
		return marketplaceoperations.FindingAction{}, false, err
	}
	if stub.actions == nil {
		stub.actions = map[string]marketplaceoperations.FindingAction{}
	}
	key := findingID + "\x00" + action.IdempotencyKey
	if existing, ok := stub.actions[key]; ok {
		if existing.Action != action.Action || existing.ActorID != action.ActorID {
			return marketplaceoperations.FindingAction{}, false, marketplaceoperations.ErrDuplicateConflict
		}
		return existing, true, nil
	}
	stub.actions[key] = action
	return action, false, nil
}

func TestMarketplaceOperationFlowCreateAndCommandAreIdempotent(t *testing.T) {
	scope := validTestScope(t)
	store := &marketplaceFlowStoreStub{timeline: map[string][]marketplaceoperations.CommandRecord{}}
	create := httptest.NewRequest(http.MethodPost, MarketplaceOperationFlowsPath, strings.NewReader(`{"account_id":"account-1"}`)).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	create.Header.Set("Idempotency-Key", "flow-start")
	create.Header.Set("Content-Type", "application/json")
	createdResponse := httptest.NewRecorder()
	marketplaceOperationFlowCreate(createdResponse, create, store)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created marketplaceOperationFlowCreateResponse
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Flow.ID == "" || created.Flow.AccountID != "account-1" {
		t.Fatalf("unexpected flow=%+v", created.Flow)
	}

	occurredAt := time.Now().UTC().Add(time.Second)
	body := `{"operation_id":"account.connected","stage":"account","outcome":"succeeded","references":[{"kind":"account","id":"account-1"}],"occurred_at":"` + occurredAt.Format(time.RFC3339Nano) + `"}`
	commandRequest := httptest.NewRequest(http.MethodPost, MarketplaceOperationFlowsPath+"/"+created.Flow.ID+"/commands", strings.NewReader(body)).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	commandRequest.Header.Set("Idempotency-Key", "account-op-1")
	commandRequest.Header.Set("Content-Type", "application/json")
	commandResponse := httptest.NewRecorder()
	marketplaceOperationFlowCommand(commandResponse, commandRequest, store)
	if commandResponse.Code != http.StatusOK {
		t.Fatalf("command status=%d body=%s", commandResponse.Code, commandResponse.Body.String())
	}
	var applied marketplaceOperationFlowCommandResponse
	if err := json.Unmarshal(commandResponse.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Duplicate || applied.Flow.Stage != marketplaceoperations.StageProduct {
		t.Fatalf("unexpected command response=%+v", applied)
	}

	replay := httptest.NewRequest(http.MethodPost, MarketplaceOperationFlowsPath+"/"+created.Flow.ID+"/commands", strings.NewReader(body)).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	replay.Header.Set("Idempotency-Key", "account-op-1")
	replay.Header.Set("Content-Type", "application/json")
	replayResponse := httptest.NewRecorder()
	marketplaceOperationFlowCommand(replayResponse, replay, store)
	if replayResponse.Code != http.StatusOK || !strings.Contains(replayResponse.Body.String(), `"duplicate":true`) {
		t.Fatalf("replay status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
}

func TestMarketplaceOperationFlowCanStartAtOrderAndExposeTimeline(t *testing.T) {
	scope := validTestScope(t)
	store := &marketplaceFlowStoreStub{timeline: map[string][]marketplaceoperations.CommandRecord{}}
	create := httptest.NewRequest(http.MethodPost, MarketplaceOperationFlowsPath, strings.NewReader(`{"account_id":"account-1","start_stage":"order","references":[{"kind":"order","id":"order-1"}]}`)).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	create.Header.Set("Idempotency-Key", "golden-flow")
	create.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	marketplaceOperationFlowCreate(response, create, store)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created marketplaceOperationFlowCreateResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Flow.Stage != marketplaceoperations.StageOrder || len(created.Flow.References) != 1 {
		t.Fatalf("golden path was not seeded at order: %+v", created.Flow)
	}
	at := time.Now().UTC()
	store.timeline[created.Flow.ID] = []marketplaceoperations.CommandRecord{{FlowID: created.Flow.ID, OperationID: "order.accepted", IdempotencyKey: "order-command", Stage: marketplaceoperations.StageOrder, Outcome: marketplaceoperations.OutcomeSucceeded, References: []marketplaceoperations.Reference{{Kind: "order", ID: "order-1"}}, OccurredAt: at}}
	request := httptest.NewRequest(http.MethodGet, MarketplaceOperationFlowsPath+"/"+created.Flow.ID, nil).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	detail := httptest.NewRecorder()
	marketplaceOperationFlowDetail(detail, request, store, created.Flow.ID)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"timeline"`) || !strings.Contains(detail.Body.String(), "order.accepted") {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestMarketplaceOperationFindingsAndActionsAreTenantScopedAndDurable(t *testing.T) {
	scope := validTestScope(t)
	store := &marketplaceFlowStoreStub{findings: map[string]marketplaceoperations.Finding{}}
	finding := marketplaceoperations.Finding{ID: "finding-1", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), FlowID: "flow-1", AccountID: "account-1", Stage: marketplaceoperations.StageInventory, Kind: marketplaceoperations.FindingPriceStockMismatch, EntityKind: "inventory", EntityID: "inventory-1", Severity: marketplaceoperations.FindingWarn, Status: marketplaceoperations.FindingOpen, ReasonCode: "stock_mismatch", Expected: "3", Observed: "1", DetectedAt: time.Now().UTC()}
	if err := finding.Validate(); err != nil {
		t.Fatal(err)
	}
	store.findings[finding.ID] = finding
	listRequest := httptest.NewRequest(http.MethodGet, MarketplaceOperationFindingsPath+"?status=open", nil).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	listResponse := httptest.NewRecorder()
	marketplaceOperationFindingsList(listResponse, listRequest, store)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "stock_mismatch") {
		t.Fatalf("findings list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	actionRequest := httptest.NewRequest(http.MethodPost, MarketplaceOperationFindingsPath+"/finding-1/actions", strings.NewReader(`{"action":"reconcile"}`))
	ctx := context.WithValue(context.Background(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "issuer", Subject: "operator-1"})
	actionRequest = actionRequest.WithContext(ctx)
	actionRequest.Header.Set("Idempotency-Key", "finding-action-1")
	actionRequest.Header.Set("Content-Type", "application/json")
	actionResponse := httptest.NewRecorder()
	marketplaceOperationFindingAction(actionResponse, actionRequest, store)
	if actionResponse.Code != http.StatusOK || !strings.Contains(actionResponse.Body.String(), `"action":"reconcile"`) {
		t.Fatalf("finding action status=%d body=%s", actionResponse.Code, actionResponse.Body.String())
	}
}
