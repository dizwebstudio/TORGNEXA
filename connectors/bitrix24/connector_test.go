package bitrix24

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{PortalHost: "tenant.bitrix24.com"}, nil
}

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	v := []byte("b24_oauth_123456789012345678901234567890")
	defer clear(v)
	return cb(v)
}

type transportFunc func(context.Context, Request) (Response, error)

func (f transportFunc) Do(ctx context.Context, r Request) (Response, error) { return f(ctx, r) }
func account() sdk.Account {
	at := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "bitrix24-test-account", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "bitrix24", Family: sdk.FamilyCRM, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

func TestManifestAndCRMFamily(t *testing.T) {
	m := Manifest()
	if e := m.Validate(); e != nil {
		t.Fatal(e)
	}
	if m.Family != sdk.FamilyCRM || !m.Supports("crm.entities.read") || !m.Supports("crm.productrows.write") {
		t.Fatalf("manifest: %#v", m)
	}
}
func TestHealthUsesBearerAndDoesNotPutTokenInBody(t *testing.T) {
	c := New(transportFunc(func(_ context.Context, r Request) (Response, error) {
		if r.Path != "/rest/crm.item.fields" || len(r.Bearer) == 0 {
			t.Fatalf("request %#v", r)
		}
		if bytes.Contains(r.Body, []byte("b24_oauth")) {
			t.Fatal("token leaked into body")
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"fields":{"id":{"type":"integer"}}}}`)}, nil
	}), testConfig{}, func() time.Time { return time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC) })
	h, e := c.Health(context.Background(), account(), testRuntime{})
	if e != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health %#v %v", h, e)
	}
}

func TestReadDealsPaginatesInsideFixedBitrixPage(t *testing.T) {
	calls := 0
	c := New(transportFunc(func(_ context.Context, r Request) (Response, error) {
		calls++
		var b map[string]any
		if json.Unmarshal(r.Body, &b) != nil {
			t.Fatal("body")
		}
		if b["start"].(float64) != 0 {
			t.Fatalf("start %v", b["start"])
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"items":[{"id":1,"title":"D1","stageId":"NEW","categoryId":0,"opportunity":10,"currencyId":"RUB","originatorId":"TORGNEXA","originId":"o1","createdTime":"2026-08-12T08:00:00+00:00","updatedTime":"2026-08-12T08:01:00+00:00"},{"id":2,"title":"D2","stageId":"NEW","categoryId":0,"opportunity":20,"currencyId":"RUB","originatorId":"TORGNEXA","originId":"o2","createdTime":"2026-08-12T08:00:00+00:00","updatedTime":"2026-08-12T08:02:00+00:00"}]},"total":2}`)}, nil
	}), testConfig{}, nil)
	q := sdk.CRMEntityQuery{Kind: sdk.CRMDeal, Page: sdk.PageRequest{Limit: 1}}
	p, e := c.ReadCRMEntities(context.Background(), account(), testRuntime{}, q)
	if e != nil || len(p.Items) != 1 || p.Items[0].RemoteID != "1" || p.NextCursor == "" {
		t.Fatalf("page1 %#v %v", p, e)
	}
	q.Page.Cursor = p.NextCursor
	p, e = c.ReadCRMEntities(context.Background(), account(), testRuntime{}, q)
	if e != nil || len(p.Items) != 1 || p.Items[0].RemoteID != "2" || p.NextCursor != "" {
		t.Fatalf("page2 %#v %v", p, e)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestUpsertDealReturnsDuplicateAfterOriginReconciliation(t *testing.T) {
	c := New(transportFunc(func(_ context.Context, r Request) (Response, error) {
		if r.Path != "/rest/crm.item.list" {
			t.Fatalf("unexpected %s", r.Path)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"items":[{"id":9,"title":"Order #42","stageId":"NEW","categoryId":1,"companyId":7,"contactIds":[8],"opportunity":100.50,"currencyId":"RUB","originatorId":"TORGNEXA","originId":"order-42","createdTime":"2026-08-12T08:00:00+00:00","updatedTime":"2026-08-12T08:01:00+00:00"}]},"total":1}`)}, nil
	}), testConfig{}, nil)
	req := sdk.CRMEntityWriteRequest{Kind: sdk.CRMDeal, ExternalID: "order-42", Title: "Order #42", StageRemoteID: "NEW", PipelineRemoteID: "1", CompanyRemoteID: "7", ContactRemoteIDs: []string{"8"}, Opportunity: "100.50", Currency: "RUB", IdempotencyKey: "idem-42"}
	receipt, e := c.UpsertCRMEntity(context.Background(), account(), testRuntime{}, req)
	if e != nil || !receipt.Duplicate || !receipt.Reconciled || receipt.RemoteID != "9" {
		t.Fatalf("receipt %#v %v", receipt, e)
	}
}

func TestReplaceRowsReturnsDuplicateWithoutWrite(t *testing.T) {
	c := New(transportFunc(func(_ context.Context, r Request) (Response, error) {
		if r.Path != "/rest/crm.item.productrow.list" {
			t.Fatalf("unexpected %s", r.Path)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"productRows":[{"id":10,"ownerId":9,"ownerType":"D","productId":5,"productName":"SKU 5","price":12.50,"quantity":2,"taxRate":20,"taxIncluded":"Y"}]},"total":1}`)}, nil
	}), testConfig{}, nil)
	req := sdk.CRMProductRowsWriteRequest{OwnerKind: sdk.CRMDeal, OwnerRemoteID: "9", Rows: []sdk.CRMProductRowWrite{{ProductRemoteID: "5", Name: "SKU 5", Price: "12.50", Quantity: "2", TaxRate: "20", TaxIncluded: true}}, IdempotencyKey: "rows-9"}
	receipt, e := c.ReplaceCRMProductRows(context.Background(), account(), testRuntime{}, req)
	if e != nil || !receipt.Duplicate || receipt.RemoteID != "9" {
		t.Fatalf("receipt %#v %v", receipt, e)
	}
}
func TestAPIErrorNormalizesQueryLimit(t *testing.T) {
	e := normalizeAPIError(Response{StatusCode: 200, Body: []byte(`{"error":"QUERY_LIMIT_EXCEEDED","error_description":"secret detail"}`)})
	var remote *sdk.RemoteError
	if !errors.As(e, &remote) || remote.Category != sdk.ErrorRateLimited {
		t.Fatalf("%T %v", e, e)
	}
	if bytes.Contains([]byte(e.Error()), []byte("secret detail")) {
		t.Fatal("description leaked")
	}
}
