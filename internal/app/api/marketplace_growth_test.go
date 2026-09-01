package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacegrowth"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

type marketplaceGrowthStoreStub struct {
	preview   marketplacegrowth.Preview
	operation marketplacegrowth.Operation
	drifts    []marketplacegrowth.Drift
	control   marketplacegrowth.KillSwitch
}

func (stub *marketplaceGrowthStoreStub) SaveRule(context.Context, tenancy.Scope, marketplacegrowth.Rule) error {
	return nil
}
func (stub *marketplaceGrowthStoreStub) ListRules(context.Context, tenancy.Scope, string, int) ([]marketplacegrowth.Rule, error) {
	return marketplacegrowth.DemoRules(time.Now().UTC()), nil
}
func (stub *marketplaceGrowthStoreStub) SavePreview(_ context.Context, _ tenancy.Scope, preview marketplacegrowth.Preview) error {
	stub.preview = preview
	return nil
}
func (stub *marketplaceGrowthStoreStub) Preview(context.Context, tenancy.Scope, string) (marketplacegrowth.Preview, error) {
	return stub.preview, nil
}
func (stub *marketplaceGrowthStoreStub) SaveOperation(_ context.Context, _ tenancy.Scope, operation marketplacegrowth.Operation) (marketplacegrowth.Operation, error) {
	stub.operation = operation
	return operation, nil
}
func (stub *marketplaceGrowthStoreStub) Operation(context.Context, tenancy.Scope, string) (marketplacegrowth.Operation, error) {
	return stub.operation, nil
}
func (stub *marketplaceGrowthStoreStub) ListOperations(context.Context, tenancy.Scope, int) ([]marketplacegrowth.Operation, error) {
	if stub.operation.ID == "" {
		return []marketplacegrowth.Operation{}, nil
	}
	return []marketplacegrowth.Operation{stub.operation}, nil
}
func (stub *marketplaceGrowthStoreStub) SaveDrifts(_ context.Context, _ tenancy.Scope, drifts []marketplacegrowth.Drift) error {
	stub.drifts = append(stub.drifts, drifts...)
	return nil
}
func (stub *marketplaceGrowthStoreStub) ListDrifts(context.Context, tenancy.Scope, int) ([]marketplacegrowth.Drift, error) {
	return stub.drifts, nil
}
func (stub *marketplaceGrowthStoreStub) SetKillSwitch(_ context.Context, _ tenancy.Scope, control marketplacegrowth.KillSwitch) error {
	stub.control = control
	return nil
}
func (stub *marketplaceGrowthStoreStub) KillSwitch(context.Context, tenancy.Scope) (marketplacegrowth.KillSwitch, error) {
	return stub.control, nil
}

func growthTestRequest() marketplacegrowth.PreviewRequest {
	return marketplacegrowth.PreviewRequest{Operation: marketplacegrowth.OperationPromotionApply, ChannelID: "demo", AccountID: "account-1", TargetID: "promo-1", Currency: "RUB", FloorPriceMinor: 9000, MinimumMarginBPS: 1000, Items: []marketplacegrowth.Candidate{{SKU: "SKU-1", Currency: "RUB", CurrentPriceMinor: 20000, ProposedPriceMinor: 20000, UnitCostMinor: 9000, CommissionBPS: 1500, LogisticsMinor: 1000, AdvertisingMinor: 500, DiscountBPS: 1000, Stock: 5, FactsFresh: true, Eligible: true}}}
}

func growthRequest(t *testing.T, contextValue context.Context, method, path string, body any) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(method, path, strings.NewReader(string(data))).WithContext(contextValue)
}

func TestMarketplaceGrowthPreviewAndApplyRemainApprovalAndQualificationBound(t *testing.T) {
	scope := validTestScope(t)
	ctx := context.WithValue(context.Background(), requestScopeKey{}, scope)
	store := &marketplaceGrowthStoreStub{}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	api := marketplaceGrowthAPI{store: store, now: func() time.Time { return now }}
	previewRequest := growthRequest(t, ctx, http.MethodPost, MarketplaceGrowthPreviewsPath, growthTestRequest())
	previewRequest.Header.Set("Content-Type", "application/json")
	previewResponse := httptest.NewRecorder()
	api.preview(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview marketplacegrowth.Preview
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.State != marketplacegrowth.PreviewApprovalRequired || preview.EligibleCount != 1 {
		t.Fatalf("preview=%+v", preview)
	}
	api.approvals = logisticsApprovalStub{request: approval.Request{ID: "approval-1", Action: "marketplace.growth.apply", ResourceType: "marketplace_growth_preview", ResourceID: preview.ID, State: approval.StateApproved}}
	applyContext := context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "issuer", Subject: "operator"})
	applyRequest := growthRequest(t, applyContext, http.MethodPost, MarketplaceGrowthOperationsPath, marketplaceGrowthApplyRequest{PreviewID: preview.ID})
	applyRequest.Header.Set("Content-Type", "application/json")
	applyRequest.Header.Set("Idempotency-Key", "growth-retry-1")
	applyRequest.Header.Set("Approval-Request-ID", "approval-1")
	applyResponse := httptest.NewRecorder()
	api.apply(applyResponse, applyRequest)
	if applyResponse.Code != http.StatusAccepted || store.operation.State != marketplacegrowth.StateQualificationRequired {
		t.Fatalf("apply status=%d body=%s operation=%+v", applyResponse.Code, applyResponse.Body.String(), store.operation)
	}
}

func TestMarketplaceGrowthReconciliationPersistsUnknownDrift(t *testing.T) {
	scope := validTestScope(t)
	store := &marketplaceGrowthStoreStub{}
	preview, err := marketplacegrowth.BuildPreview("preview-1", growthTestRequest(), 1, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	operation, err := marketplacegrowth.NewOperation("operation-1", "retry-1", "approval-1", preview, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store.preview, store.operation = preview, operation
	api := marketplaceGrowthAPI{store: store}
	request := growthRequest(t, context.WithValue(context.Background(), requestScopeKey{}, scope), http.MethodPost, MarketplaceGrowthReconciliationPath, marketplacegrowth.Observation{OperationID: operation.ID, State: marketplacegrowth.StateUnknown, InputDigest: operation.InputDigest, ObservedAt: time.Now().UTC()})
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.reconcile(response, request)
	if response.Code != http.StatusOK || len(store.drifts) != 1 || store.drifts[0].Kind != "state_mismatch" {
		t.Fatalf("reconcile status=%d body=%s drifts=%+v", response.Code, response.Body.String(), store.drifts)
	}
}
