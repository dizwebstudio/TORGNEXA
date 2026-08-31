package ozon

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed manifest.json
var manifestJSON []byte

//go:embed fixtures/product-list-page-1.json
var productListPageOne []byte

//go:embed fixtures/product-list-last.json
var productListLast []byte

//go:embed fixtures/product-info.json
var productInfoFixture []byte

//go:embed fixtures/product-info-last.json
var productInfoLast []byte

//go:embed fixtures/warehouses.json
var warehousesFixture []byte

//go:embed fixtures/stocks.json
var stocksFixture []byte

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (t *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	r.Body = append([]byte(nil), r.Body...)
	r.ClientID = append([]byte(nil), r.ClientID...)
	r.APIKey = append([]byte(nil), r.APIKey...)
	r.Bearer = append([]byte(nil), r.Bearer...)
	t.requests = append(t.requests, r)
	i := len(t.requests) - 1
	if i < len(t.errs) && t.errs[i] != nil {
		return Response{}, t.errs[i]
	}
	if i >= len(t.responses) {
		return Response{}, errors.New("unexpected call")
	}
	return t.responses[i], nil
}

type testRuntime struct{ secret []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{r.secret} }

type testSecrets struct{ value []byte }

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s.value...)
	defer clear(v)
	return cb(v)
}
func testAccount() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "ozon-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "ozon", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}
func creds() []byte { return []byte("123456\nsynthetic-api-key-0123456789abcdef") }
func advertisingCreds() []byte {
	return []byte("123456\nsynthetic-api-key-0123456789abcdef\nsynthetic-performance-bearer-0123456789")
}

func TestManifestMatchesCommittedJSON(t *testing.T) {
	var got sdk.Manifest
	if err := json.Unmarshal(manifestJSON, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatalf("manifest drift")
	}
	if !Manifest().Supports("products.write") || Manifest().Supports("inventory.write") {
		t.Fatal("marketplace product publication capability is not declared exactly")
	}
}

func TestAdvertisingReadsCampaignsAndPerformanceBySKU(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	stats := []byte(`[{"campaignId":"cmp-1","date":"2026-08-09","expense":12.34,"views":100,"clicks":20,"orders":3,"ordersSum":500,"sku":"offer-1"}]`)
	transport := &scriptedTransport{responses: []Response{
		{StatusCode: 200, Body: []byte(`{"list":[{"id":"cmp-1","title":"Summer","state":"running","budget":100}]}`)},
		{StatusCode: 200, Body: stats},
		{StatusCode: 200, Body: stats},
	}}
	connector := New(transport, func() time.Time { return now })
	campaigns, err := connector.ReadAdvertisingCampaigns(context.Background(), testAccount(), testRuntime{advertisingCreds()}, sdk.PageRequest{Limit: 100})
	if err != nil || len(campaigns.Items) != 1 || campaigns.Items[0].RemoteID != "cmp-1" || campaigns.Items[0].Status != "active" || campaigns.Items[0].DailyBudgetMinor != 10000 {
		t.Fatalf("campaigns=%+v err=%v", campaigns, err)
	}
	query := sdk.AdvertisingQuery{From: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), CampaignIDs: []string{"cmp-1"}, Limit: 100}
	spend, err := connector.ReadAdvertisingSpend(context.Background(), testAccount(), testRuntime{advertisingCreds()}, query)
	if err != nil || len(spend.Items) != 1 || spend.Items[0].SKU != "offer-1" || spend.Items[0].AmountMinor != 1234 {
		t.Fatalf("spend=%+v err=%v", spend, err)
	}
	performance, err := connector.ReadAdvertisingPerformance(context.Background(), testAccount(), testRuntime{advertisingCreds()}, query)
	if err != nil || len(performance.Items) != 1 || performance.Items[0].SKU != "offer-1" || performance.Items[0].Orders != 3 || performance.Items[0].RevenueMinor != 50000 {
		t.Fatalf("performance=%+v err=%v", performance, err)
	}
	if got := transport.requests[1]; got.Host != performanceHost || got.Path != "/api/client/statistics/campaign/media/json" || string(got.Bearer) != "synthetic-performance-bearer-0123456789" || len(got.Query) != 3 {
		t.Fatalf("unexpected stats request: %+v", got)
	}
}

func TestCredentialBundleIsStrict(t *testing.T) {
	good := creds()
	cid, key, err := parseCredentialBundle(good)
	if err != nil || string(cid) != "123456" || !strings.HasPrefix(string(key), "synthetic-") {
		t.Fatalf("good creds rejected: %v", err)
	}
	for _, bad := range [][]byte{nil, []byte("123456"), []byte("abc\n12345678"), []byte("123\nshort"), []byte("123\nabc\ndef"), []byte("123\nabc\x00defgh")} {
		if _, _, err := parseCredentialBundle(bad); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("accepted bad creds %q: %v", bad, err)
		}
	}
}
func TestHealthUsesCurrentProductListAndNormalizesAuth(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"result":{"items":[],"total":0,"last_id":""}}`)}, {StatusCode: 404}}}
	c := New(tr, func() time.Time { return now })
	h, err := c.Health(context.Background(), testAccount(), testRuntime{creds()})
	if err != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", h, err)
	}
	h, err = c.Health(context.Background(), testAccount(), testRuntime{creds()})
	if err != nil || h.Status != sdk.HealthDegraded || h.ReasonCode != "auth_rejected" {
		t.Fatalf("health=%+v err=%v", h, err)
	}
	r := tr.requests[0]
	if r.Host != apiHost || r.Path != "/v3/product/list" || r.Method != "POST" || string(r.ClientID) != "123456" || len(r.APIKey) == 0 {
		t.Fatalf("request=%+v", r)
	}
}
func TestReadProductsMapsAndReplaysOpaqueCursor(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: productListPageOne}, {StatusCode: 200, Body: productInfoFixture}, {StatusCode: 200, Body: productListLast}, {StatusCode: 200, Body: productInfoLast}}}
	c := New(tr, nil)
	p, err := c.ReadProducts(context.Background(), testAccount(), testRuntime{creds()}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].RemoteID != "9001001" || p.Items[0].SellerSKU != "OZ-RED-M" || p.Items[0].Variants[0].RemoteID != "OZ-RED-M" || p.NextCursor == "" {
		t.Fatalf("page=%+v", p)
	}
	var first productListRequest
	if err := json.Unmarshal(tr.requests[0].Body, &first); err != nil {
		t.Fatal(err)
	}
	if first.Filter.Visibility != "ALL" || first.Limit != 2 || first.LastID != "" {
		t.Fatalf("request=%+v", first)
	}
	var info productInfoRequest
	_ = json.Unmarshal(tr.requests[1].Body, &info)
	if !reflect.DeepEqual(info.ProductIDs, []int64{9001001, 9001002}) {
		t.Fatalf("ids=%v", info.ProductIDs)
	}
	p2, err := c.ReadProducts(context.Background(), testAccount(), testRuntime{creds()}, sdk.PageRequest{Limit: 2, Cursor: p.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Items) != 1 || p2.NextCursor != "" {
		t.Fatalf("second=%+v", p2)
	}
	var second productListRequest
	_ = json.Unmarshal(tr.requests[2].Body, &second)
	if second.LastID != "oz-last-2" {
		t.Fatalf("cursor=%q", second.LastID)
	}
}
func TestInventoryReadsWarehouseAndAvailableStock(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: warehousesFixture}, {StatusCode: 200, Body: stocksFixture}}}
	c := New(tr, nil)
	loc, err := c.ListInventoryLocations(context.Background(), testAccount(), testRuntime{creds()})
	if err != nil || len(loc) != 2 || loc[0].RemoteID != "70001" {
		t.Fatalf("loc=%+v err=%v", loc, err)
	}
	items, err := c.ReadInventory(context.Background(), testAccount(), testRuntime{creds()}, sdk.InventoryQuery{LocationRemoteID: "70001", VariantRemoteIDs: []string{"OZ-RED-M", "OZ-BLUE-L"}})
	if err != nil || len(items) != 2 || items[0].Quantity != 7 || items[1].Quantity != 3 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if tr.requests[0].Path != "/v2/warehouse/list" || tr.requests[1].Path != "/v2/product/info/stocks-by-warehouse/fbs" {
		t.Fatalf("paths=%s %s", tr.requests[0].Path, tr.requests[1].Path)
	}
	var req stocksRequest
	_ = json.Unmarshal(tr.requests[1].Body, &req)
	if !reflect.DeepEqual(req.OfferIDs, []string{"OZ-RED-M", "OZ-BLUE-L"}) || req.Limit != 1000 {
		t.Fatalf("stock request=%+v", req)
	}
}
func TestReadProductsRejectsMalformedAndDriftedResponses(t *testing.T) {
	c := New(&scriptedTransport{}, nil)
	if _, err := c.ReadProducts(context.Background(), testAccount(), testRuntime{creds()}, sdk.PageRequest{Limit: 2, Cursor: "%%%"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("cursor err=%v", err)
	}
	cases := [][]byte{[]byte(`{"result":{"items":[{"product_id":1,"offer_id":"A"},{"product_id":1,"offer_id":"B"}],"total":2,"last_id":"x"}}`), []byte(`{"result":{"items":[{"product_id":1,"offer_id":"A"},{"product_id":2,"offer_id":"A"}],"total":2,"last_id":"x"}}`), []byte(`{"result":{"items":[{"product_id":1,"offer_id":"A"}],"total":0,"last_id":"x"}}`)}
	for _, body := range cases {
		tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: body}}}
		c := New(tr, nil)
		if _, err := c.ReadProducts(context.Background(), testAccount(), testRuntime{creds()}, sdk.PageRequest{Limit: 2}); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("accepted %s err=%v", body, err)
		}
	}
	badInfo := []byte(`{"items":[{"id":9001001,"name":"X","offer_id":"WRONG","barcodes":[],"updated_at":"2026-08-10T00:00:00Z"},{"id":9001002,"name":"Y","offer_id":"OZ-BLUE-L","barcodes":[],"updated_at":"2026-08-10T00:00:00Z"}]}`)
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: productListPageOne}, {StatusCode: 200, Body: badInfo}}}
	c = New(tr, nil)
	if _, err := c.ReadProducts(context.Background(), testAccount(), testRuntime{creds()}, sdk.PageRequest{Limit: 2}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("info drift err=%v", err)
	}
}
func TestInventoryRejectsPartialPaginationAndUnsafeRows(t *testing.T) {
	wh := []byte(`{"warehouses":[{"warehouse_id":1,"name":"A"},{"warehouse_id":1,"name":"B"}],"cursor":""}`)
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: wh}}}
	if _, err := New(tr, nil).ListInventoryLocations(context.Background(), testAccount(), testRuntime{creds()}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate warehouse err=%v", err)
	}
	wh = []byte(`{"warehouses":[],"cursor":"more"}`)
	tr = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: wh}}}
	if _, err := New(tr, nil).ListInventoryLocations(context.Background(), testAccount(), testRuntime{creds()}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("partial warehouse err=%v", err)
	}
	bodies := [][]byte{[]byte(`{"items":[{"offer_id":"OTHER","sku":1,"stocks":[]}],"cursor":""}`), []byte(`{"items":[{"offer_id":"OZ-RED-M","sku":1,"stocks":[{"warehouse_id":70001,"present":1,"reserved":2}]},{"offer_id":"OZ-BLUE-L","sku":2,"stocks":[]}],"cursor":""}`), []byte(`{"items":[{"offer_id":"OZ-RED-M","sku":1,"stocks":[]},{"offer_id":"OZ-RED-M","sku":2,"stocks":[]}],"cursor":""}`), []byte(`{"items":[{"offer_id":"OZ-RED-M","sku":1,"stocks":[]},{"offer_id":"OZ-BLUE-L","sku":2,"stocks":[]}],"cursor":"more"}`)}
	for _, body := range bodies {
		tr = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: body}}}
		_, err := New(tr, nil).ReadInventory(context.Background(), testAccount(), testRuntime{creds()}, sdk.InventoryQuery{LocationRemoteID: "70001", VariantRemoteIDs: []string{"OZ-RED-M", "OZ-BLUE-L"}})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("accepted %s err=%v", body, err)
		}
	}
}

func TestProductPublicationUsesImportTaskAndDoesNotLeakCredentials(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, RequestID: "ozon-request-1", Body: []byte(`{"result":{"task_id":12345}}`)}}}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) })
	request := publicationRequest(t, "OZ-SKU-1", "ozon")
	receipt, err := connector.WriteProductPublication(context.Background(), testAccount(), testRuntime{creds()}, request)
	if err != nil || receipt.Status != sdk.PublicationAccepted || receipt.RemoteOperationID != "12345" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	remoteRequest := transport.requests[0]
	if remoteRequest.Path != "/v2/product/import" || remoteRequest.IdempotencyKey != "publication-1" || strings.Contains(string(remoteRequest.Body), "synthetic-api-key") || strings.Contains(string(remoteRequest.Body), "https://") {
		t.Fatalf("unsafe or unexpected request: %+v body=%s", remoteRequest, remoteRequest.Body)
	}
}

func publicationRequest(t *testing.T, sku, connectorID string) sdk.ProductPublicationRequest {
	t.Helper()
	var request sdk.ProductPublicationRequest
	data := []byte(`{"operation":"create_product","snapshot":{"id":"snapshot-1","target":{"organization_id":"01890f4d-1e10-7cc0-9c4a-111111111111","workspace_id":"01890f4d-1e10-7cc0-9c4a-222222222222","product_id":"product-1","connector_account_id":"ozon-account","connector_id":"ozon","locale":"ru-RU","jurisdiction":"RU"},"version":1,"sku":"OZ-SKU-1","title":"Synthetic card","category_code":"123","dimension":{"length_mm":10,"width_mm":10,"height_mm":10,"weight_g":100},"price_minor":199900,"currency":"RUB","product_status":"active","catalog_version":1,"pim_version":1,"price_version":1,"media_version":1,"mapping_version":1,"capability_version":1,"assembled_at":"2026-08-10T11:00:00Z"},"idempotency_key":"publication-1","approval_request_id":"approval-1","quality_receipt_id":"receipt-1"}`)
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	request.Snapshot.SKU = sku
	request.Snapshot.Target.ConnectorID = connectorID
	return request
}
func TestErrorsAreBounded(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 429, RequestID: "oz-req", RetryAfterMS: 1500, Body: []byte(`{"error":"api-key=secret"}`)}}}
	_, err := New(tr, nil).ListInventoryLocations(context.Background(), testAccount(), testRuntime{creds()})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "rate_limited" || remote.RetryAfterMS != 1500 || strings.Contains(remote.Error(), "secret") {
		t.Fatalf("remote=%#v err=%v", remote, err)
	}
	oversized := make([]byte, maxBodyBytes+1)
	tr = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: oversized}}}
	_, err = New(tr, nil).ListInventoryLocations(context.Background(), testAccount(), testRuntime{creds()})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorInternal {
		t.Fatalf("oversize err=%v", err)
	}
	tr = &scriptedTransport{responses: []Response{{}}, errs: []error{errors.New("dial api_key=must-not-leak")}}
	_, err = New(tr, nil).ListInventoryLocations(context.Background(), testAccount(), testRuntime{creds()})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnavailable || strings.Contains(remote.Error(), "must-not-leak") {
		t.Fatalf("transport err=%v", err)
	}
}
func TestInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
}
