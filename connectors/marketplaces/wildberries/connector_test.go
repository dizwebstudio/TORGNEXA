package wildberries

import (
	"context"
	_ "embed"
	"encoding/base64"
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

//go:embed fixtures/cards-page-1.json
var cardsPageOne []byte

//go:embed fixtures/cards-page-full.json
var cardsPageFull []byte

//go:embed fixtures/warehouses.json
var warehousesFixture []byte

//go:embed fixtures/stocks.json
var stocksFixture []byte

//go:embed fixtures/orders-page.json
var ordersPageFixture []byte

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
	tokens    []string
}

func (transport *scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	transport.requests = append(transport.requests, request)
	transport.tokens = append(transport.tokens, string(request.Token))
	index := len(transport.requests) - 1
	if index < len(transport.errs) && transport.errs[index] != nil {
		return Response{}, transport.errs[index]
	}
	if index >= len(transport.responses) {
		return Response{}, errors.New("unexpected call")
	}
	return transport.responses[index], nil
}

type testRuntime struct{ secret []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{value: runtime.secret} }

type testSecrets struct{ value []byte }

func (secrets testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secrets.value...)
	defer clear(value)
	return callback(value)
}

func testAccount() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "wb-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "wildberries", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}

func TestManifestMatchesCommittedJSON(t *testing.T) {
	var fromFile sdk.Manifest
	if err := json.Unmarshal(manifestJSON, &fromFile); err != nil {
		t.Fatal(err)
	}
	if err := fromFile.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromFile.Canonical(), Manifest().Canonical()) {
		t.Fatalf("manifest drift: %#v != %#v", fromFile, Manifest())
	}
	if !Manifest().Supports("products.write") || !Manifest().Supports("prices.write") || !Manifest().Supports("inventory.write") || !Manifest().Supports("orders.read") || !Manifest().Supports("orders.status.write") {
		t.Fatal("marketplace product publication capability is not declared exactly")
	}
}

func TestApplyMarketplaceOrderActionCancelsAssemblyOrder(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 204}}}
	receipt, err := New(transport, nil).ApplyMarketplaceOrderAction(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.MarketplaceOrderActionRequest{OrderRemoteID: "13833711", Action: sdk.MarketplaceOrderCancel, IdempotencyKey: "wb-cancel-1"})
	if err != nil || receipt.Status != sdk.MarketplaceOperationApplied || receipt.RemoteID != "13833711" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if request := transport.requests[0]; request.Method != "PATCH" || request.Host != marketplaceHost || request.Path != "/api/v3/orders/13833711/cancel" || request.IdempotencyKey != "wb-cancel-1" {
		t.Fatalf("request=%+v", request)
	}
}

func TestApplyMarketplaceOrderActionRejectsUnsupportedConfirmation(t *testing.T) {
	_, err := New(&scriptedTransport{}, nil).ApplyMarketplaceOrderAction(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.MarketplaceOrderActionRequest{OrderRemoteID: "13833711", Action: sdk.MarketplaceOrderConfirm, IdempotencyKey: "wb-confirm-1"})
	if !errors.Is(err, sdk.ErrInvalidMarketplaceOperation) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyAdvertisingOperationUsesWBManagementEndpoints(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200}, {StatusCode: 200, Body: []byte(`{"total":5000}`)}, {StatusCode: 200, Body: []byte(`{"bids":[]}`)}}}
	connector := New(transport, nil)
	operations := []sdk.AdvertisingOperation{
		{Name: sdk.AdvertisingLaunch, CampaignID: "123", IdempotencyKey: "wb-ads-launch-1"},
		{Name: sdk.AdvertisingSetBudget, CampaignID: "123", AmountMinor: 5000, Currency: "RUB", IdempotencyKey: "wb-ads-budget-1"},
		{Name: sdk.AdvertisingSetBid, CampaignID: "123", AmountMinor: 250, Currency: "RUB", ProductIDs: []string{"456"}, IdempotencyKey: "wb-ads-bid-1"},
	}
	for _, operation := range operations {
		result, err := connector.ApplyAdvertisingOperation(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, operation)
		if err != nil || result.State != sdk.AdvertisingAccepted || result.ReadAfterWrite {
			t.Fatalf("operation=%+v result=%+v err=%v", operation, result, err)
		}
	}
	if got := transport.requests[0]; got.Method != "GET" || got.Path != "/adv/v0/start" || len(got.Query) != 1 || got.Query[0].Value != "123" || got.IdempotencyKey != "wb-ads-launch-1" {
		t.Fatalf("launch request=%+v", got)
	}
	if got := transport.requests[1]; got.Method != "POST" || got.Path != "/adv/v1/budget/deposit" {
		t.Fatalf("budget request=%+v", got)
	}
	var budget wbBudgetDeposit
	if json.Unmarshal(transport.requests[1].Body, &budget) != nil || budget.Sum != 5000 || budget.Type != 1 || !budget.Return {
		t.Fatalf("budget body=%s", transport.requests[1].Body)
	}
	if got := transport.requests[2]; got.Method != "PATCH" || got.Path != "/api/advert/v1/bids" {
		t.Fatalf("bid request=%+v", got)
	}
	var bids wbBidsRequest
	if json.Unmarshal(transport.requests[2].Body, &bids) != nil || len(bids.Bids) != 1 || bids.Bids[0].AdvertID != 123 || len(bids.Bids[0].NMBids) != 1 || bids.Bids[0].NMBids[0].NMID != 456 || bids.Bids[0].NMBids[0].BidKopecks != 250 {
		t.Fatalf("bid body=%s", transport.requests[2].Body)
	}
}

func TestApplyAdvertisingOperationTransportFailureIsUnknown(t *testing.T) {
	transport := &scriptedTransport{errs: []error{errors.New("dial token=must-not-leak")}}
	_, err := New(transport, nil).ApplyAdvertisingOperation(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.AdvertisingOperation{Name: sdk.AdvertisingPause, CampaignID: "123", IdempotencyKey: "wb-ads-pause-1"})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Code != "write_outcome_unknown" || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteInventoryUsesFBSStockEndpointAndReturnsUnreconciledReceipt(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 204}}}
	connector := New(transport, nil)
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.InventoryWriteRequest{
		VariantRemoteID: "701710869", LocationRemoteID: "12345", Quantity: 7, IdempotencyKey: "inventory-write-1",
	})
	if err != nil || receipt.RemoteID != "701710869" || !receipt.Applied || receipt.Reconciled || receipt.Duplicate {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if got := transport.requests[0]; got.Method != "PUT" || got.Host != marketplaceHost || got.Path != "/api/v3/stocks/12345" || got.IdempotencyKey != "inventory-write-1" {
		t.Fatalf("unexpected request: %+v", got)
	}
	var body inventoryWriteRequest
	if err := json.Unmarshal(transport.requests[0].Body, &body); err != nil || len(body.Stocks) != 1 || body.Stocks[0].ChrtID != 701710869 || body.Stocks[0].Amount != 7 {
		t.Fatalf("body=%s err=%v", transport.requests[0].Body, err)
	}
}

func TestWriteInventoryTransportFailureIsUnknown(t *testing.T) {
	transport := &scriptedTransport{errs: []error{errors.New("dial token=must-not-leak")}}
	connector := New(transport, nil)
	_, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.InventoryWriteRequest{
		VariantRemoteID: "701710869", LocationRemoteID: "12345", Quantity: 7, IdempotencyKey: "inventory-write-1",
	})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Code != "write_outcome_unknown" || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("err=%v", err)
	}
}

func TestWritePriceRequiresParentProductAndUsesSizePriceEndpoint(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"data":{"id":123}}`)}}}
	connector := New(transport, nil)
	receipt, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PriceWriteRequest{
		ProductRemoteID: "505548518", VariantRemoteID: "701710869", Value: "1999.00", CompareAt: "2499.00", Currency: "RUB", IdempotencyKey: "price-write-1",
	})
	if err != nil || receipt.RemoteID != "701710869" || !receipt.Applied || receipt.Reconciled || receipt.Duplicate {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if got := transport.requests[0]; got.Method != "POST" || got.Host != pricesHost || got.Path != "/api/v2/upload/task" || got.IdempotencyKey != "price-write-1" {
		t.Fatalf("unexpected request: %+v", got)
	}
	var body priceUploadRequest
	if err := json.Unmarshal(transport.requests[0].Body, &body); err != nil || len(body.Data) != 1 || body.Data[0].NmID != 505548518 || body.Data[0].SizeID != 701710869 || body.Data[0].Price != 1999 || body.Data[0].Discount != 20 {
		t.Fatalf("body=%s err=%v", transport.requests[0].Body, err)
	}
}

func TestWritePriceRejectsMissingParentProduct(t *testing.T) {
	_, err := New(&scriptedTransport{}, nil).WritePrice(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PriceWriteRequest{
		VariantRemoteID: "701710869", Value: "1999.00", Currency: "RUB", IdempotencyKey: "price-write-1",
	})
	if !errors.Is(err, sdk.ErrInvalidCommerceWrite) {
		t.Fatalf("err=%v", err)
	}
}

func TestReadOrdersUsesBoundedNextCursorAndNormalizedProjection(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: ordersPageFixture}}}
	connector := New(transport, func() time.Time { return now })
	page, err := connector.ReadOrders(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].RemoteID != "13833711" || page.Items[0].Items[0].VariantRemoteID != "987654321" || page.Items[0].StatusRemoteID != "assembly" || page.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if len(transport.requests[0].Query) != 4 || transport.requests[0].Query[0].Name != "limit" || transport.requests[0].Query[0].Value != "1" || transport.requests[0].Query[1].Name != "next" || transport.requests[0].Query[1].Value != "0" {
		t.Fatalf("query=%+v", transport.requests[0].Query)
	}
	data, decodeErr := base64.RawURLEncoding.DecodeString(page.NextCursor)
	var cursor orderCursor
	if decodeErr != nil || json.Unmarshal(data, &cursor) != nil || cursor.Next != 13833712 {
		t.Fatalf("cursor=%q", page.NextCursor)
	}
}

func TestAdvertisingReadsCampaignsAndProductStatsWithoutDoubleCounting(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	stats := []byte(`[{"advertId":123,"days":[{"date":"2026-08-09","views":100,"clicks":20,"orders":3,"sum":999.99,"sum_price":5000.00,"apps":[{"nm":[{"nmId":456,"views":10,"clicks":2,"orders":1,"sum":12.34,"sum_price":100.00}]}]}]}]`)
	transport := &scriptedTransport{responses: []Response{
		{StatusCode: 200, Body: []byte(`{"adverts":[{"status":9,"advert_list":[{"advertId":123,"changeTime":"2026-08-10T11:00:00Z"}]}]}`)},
		{StatusCode: 200, Body: stats},
		{StatusCode: 200, Body: stats},
	}}
	connector := New(transport, func() time.Time { return now })
	campaigns, err := connector.ReadAdvertisingCampaigns(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100})
	if err != nil || len(campaigns.Items) != 1 || campaigns.Items[0].RemoteID != "123" || campaigns.Items[0].Status != "active" {
		t.Fatalf("campaigns=%+v err=%v", campaigns, err)
	}
	query := sdk.AdvertisingQuery{From: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), CampaignIDs: []string{"123"}, Limit: 100}
	spend, err := connector.ReadAdvertisingSpend(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, query)
	if err != nil || len(spend.Items) != 1 || spend.Items[0].SKU != "456" || spend.Items[0].AmountMinor != 1234 {
		t.Fatalf("spend=%+v err=%v", spend, err)
	}
	performance, err := connector.ReadAdvertisingPerformance(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, query)
	if err != nil || len(performance.Items) != 1 || performance.Items[0].SKU != "456" || performance.Items[0].RevenueMinor != 10000 {
		t.Fatalf("performance=%+v err=%v", performance, err)
	}
	if got := transport.requests[1]; got.Host != advertisingHost || got.Path != "/adv/v3/fullstats" || len(got.Query) != 3 || got.Query[0].Name != "ids" || got.Query[0].Value != "123" {
		t.Fatalf("unexpected stats request: %+v", got)
	}
}

func TestHealthUsesPingAndNormalizesAuthFailure(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200}, {StatusCode: 200}, {StatusCode: 401, RequestID: "req-1"}}}
	connector := New(transport, func() time.Time { return now })
	health, err := connector.Health(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	health, err = connector.Health(context.Background(), testAccount(), testRuntime{secret: []byte("bad")})
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "auth_rejected" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	if got := transport.requests[0]; got.Host != contentHost || got.Path != "/ping" || got.Method != "GET" {
		t.Fatalf("unexpected ping request: %+v", got)
	}
}

func TestReadProductsMapsRemoteIDsAndCursor(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: cardsPageFull}}}
	connector := New(transport, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "505548518" || page.Items[0].Variants[0].RemoteID != "701710869" || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	var body map[string]any
	if err := json.Unmarshal(transport.requests[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	settings := body["settings"].(map[string]any)
	cursor := settings["cursor"].(map[string]any)
	if cursor["limit"] != float64(1) {
		t.Fatalf("missing bounded limit: %s", transport.requests[0].Body)
	}
	transport.responses = append(transport.responses, Response{StatusCode: 200, Body: cardsPageOne})
	_, err = connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	var second map[string]any
	_ = json.Unmarshal(transport.requests[1].Body, &second)
	secondCursor := second["settings"].(map[string]any)["cursor"].(map[string]any)
	if secondCursor["nmID"] != float64(505548518) || secondCursor["updatedAt"] != "2026-08-10T12:00:00Z" {
		t.Fatalf("cursor not replayed: %v", secondCursor)
	}
}

func TestReadMarketplaceListingTaxonomyUsesOfficialContentDictionaries(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{
		{StatusCode: 200, Body: []byte(`{"data":[{"id":479,"name":"Электроника","isVisible":true}]}`)},
		{StatusCode: 200, Body: []byte(`{"data":[{"subjectID":105,"subjectName":"Кроссовки","parentID":479,"parentName":"Электроника","isVisible":true}]}`)},
		{StatusCode: 200, Body: []byte(`{"data":[{"charcID":54337,"subjectID":105,"name":"Размер","required":true,"unitName":"см","charcType":4}]}`)},
	}}
	taxonomy, err := New(transport, nil).ReadMarketplaceListingTaxonomy(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.MarketplaceListingTaxonomyRequest{Locale: "ru-RU", Jurisdiction: "RU", CategoryCode: "105"})
	if err != nil {
		t.Fatal(err)
	}
	if taxonomy.ChannelID != "wildberries" || taxonomy.Fingerprint == "" || len(taxonomy.Categories) != 2 || len(taxonomy.Attributes) != 1 || taxonomy.Attributes[0].Code != "размер" || taxonomy.Attributes[0].Unit != "cm" {
		t.Fatalf("taxonomy=%+v", taxonomy)
	}
	for _, category := range taxonomy.Categories {
		if category.Code == "105" && len(category.AttributeCodes) != 1 || category.Code == "105" && category.AttributeCodes[0] != "размер" {
			t.Fatalf("subject attributes=%+v", category)
		}
	}
	if got := transport.requests[0]; got.Method != "GET" || got.Host != contentHost || got.Path != "/content/v2/object/parent/all" || len(got.Query) != 1 || got.Query[0].Value != "ru" {
		t.Fatalf("parent request=%+v", got)
	}
	if got := transport.requests[2]; got.Path != "/content/v2/object/charcs/105" {
		t.Fatalf("characteristics request=%+v", got)
	}
}

func TestInventoryReadsWarehousesAndChrtIDs(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: warehousesFixture}, {StatusCode: 200, Body: stocksFixture}}}
	connector := New(transport, nil)
	locations, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	if err != nil || len(locations) != 2 || locations[0].RemoteID != "12345" {
		t.Fatalf("locations=%+v err=%v", locations, err)
	}
	items, err := connector.ReadInventory(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.InventoryQuery{LocationRemoteID: "12345", VariantRemoteIDs: []string{"701710869", "701710870"}})
	if err != nil || len(items) != 2 || items[0].Quantity != 17 || items[1].Quantity != 0 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if transport.requests[1].Path != "/api/v3/stocks/12345" {
		t.Fatalf("path=%s", transport.requests[1].Path)
	}
	var request stocksRequest
	if err := json.Unmarshal(transport.requests[1].Body, &request); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.ChrtIDs, []int64{701710869, 701710870}) {
		t.Fatalf("chrtIds=%v", request.ChrtIDs)
	}
}

func TestRemoteErrorsAreBounded(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 429, RequestID: "req-rate", RetryAfterMS: 1500, Body: []byte(`{"error":"raw secret must not escape"}`)}}}
	connector := New(transport, nil)
	_, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "rate_limited" || remote.RetryAfterMS != 1500 || remote.RemoteRequestID != "req-rate" {
		t.Fatalf("err=%#v", err)
	}
	if remote.Error() == string(transport.responses[0].Body) {
		t.Fatal("raw response leaked")
	}
}

func TestProductAndInventoryInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
	var _ sdk.MarketplaceListingTaxonomyReader = (*Connector)(nil)
	var _ sdk.OrderReader = (*Connector)(nil)
}

func TestReadProductsRejectsMalformedCursorAndResponse(t *testing.T) {
	connector := New(&scriptedTransport{}, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100, Cursor: "%%%"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("malformed cursor error=%v", err)
	}
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"cards":[],"cursor":{"updatedAt":"","nmID":0,"total":-1}}`)}}}
	connector = New(transport, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("invalid response error=%v", err)
	}
	transport = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"cards":[],"cursor":{"updatedAt":"","nmID":0,"total":1}}`)}}}
	connector = New(transport, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("cursor total/card count mismatch error=%v", err)
	}
}

func TestInventoryRejectsDuplicateWarehouseAndUnsafeStocks(t *testing.T) {
	duplicateWarehouses := []byte(`[{"id":12345,"name":"A"},{"id":12345,"name":"B"}]`)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: duplicateWarehouses}}}
	connector := New(transport, nil)
	if _, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate warehouse error=%v", err)
	}

	cases := [][]byte{
		[]byte(`{"stocks":[{"chrtId":701710869,"amount":-1}]}`),
		[]byte(`{"stocks":[{"chrtId":999999999,"amount":1}]}`),
		[]byte(`{"stocks":[{"chrtId":701710869,"amount":1},{"chrtId":701710869,"amount":1}]}`),
	}
	for _, body := range cases {
		transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: body}}}
		connector := New(transport, nil)
		_, err := connector.ReadInventory(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.InventoryQuery{LocationRemoteID: "12345", VariantRemoteIDs: []string{"701710869", "701710870"}})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("unsafe stock body accepted: %s err=%v", body, err)
		}
	}
}

func TestOversizedAndTransportFailuresNormalize(t *testing.T) {
	oversized := make([]byte, maxBodyBytes+1)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: oversized}}}
	connector := New(transport, nil)
	_, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorInternal || remote.Code != "response_invalid" {
		t.Fatalf("oversized response err=%#v", err)
	}

	transport = &scriptedTransport{errs: []error{errors.New("dial tcp token=must-not-leak")}, responses: []Response{{}}}
	connector = New(transport, nil)
	_, err = connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnavailable || remote.Code != "transport_unavailable" || strings.Contains(remote.Error(), "must-not-leak") {
		t.Fatalf("transport error not normalized: %#v", err)
	}
}

func TestProductPublicationUsesSnapshotAndBoundedIdempotency(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, RequestID: "wb-request-1"}}}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) })
	request := publicationRequest(t, "WB-SKU-1", "wildberries")
	receipt, err := connector.WriteProductPublication(context.Background(), testAccount(), testRuntime{secret: []byte("synthetic-token")}, request)
	if err != nil || receipt.Status != sdk.PublicationAccepted || len(transport.requests) != 1 {
		t.Fatalf("receipt=%+v err=%v requests=%d", receipt, err, len(transport.requests))
	}
	remoteRequest := transport.requests[0]
	if remoteRequest.Path != "/content/v2/cards/upload" || remoteRequest.IdempotencyKey != "publication-1" || strings.Contains(string(remoteRequest.Body), "https://") || strings.Contains(string(remoteRequest.Body), "synthetic-token") {
		t.Fatalf("unsafe or unexpected request: %+v body=%s", remoteRequest, remoteRequest.Body)
	}
}

func publicationRequest(t *testing.T, sku, connectorID string) sdk.ProductPublicationRequest {
	t.Helper()
	var request sdk.ProductPublicationRequest
	data := []byte(`{"operation":"create_product","snapshot":{"id":"snapshot-1","target":{"organization_id":"01890f4d-1e10-7cc0-9c4a-111111111111","workspace_id":"01890f4d-1e10-7cc0-9c4a-222222222222","product_id":"product-1","connector_account_id":"wb-account","connector_id":"wildberries","locale":"ru-RU","jurisdiction":"RU"},"version":1,"sku":"WB-SKU-1","title":"Synthetic card","category_code":"123","dimension":{"length_mm":10,"width_mm":10,"height_mm":10,"weight_g":100},"price_minor":199900,"currency":"RUB","product_status":"active","catalog_version":1,"pim_version":1,"price_version":1,"media_version":1,"mapping_version":1,"capability_version":1,"assembled_at":"2026-08-10T11:00:00Z"},"idempotency_key":"publication-1","approval_request_id":"approval-1","quality_receipt_id":"receipt-1"}`)
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	request.Snapshot.SKU = sku
	request.Snapshot.Target.ConnectorID = connectorID
	return request
}

func TestInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
	var _ sdk.InventoryWriter = (*Connector)(nil)
	var _ sdk.PriceWriter = (*Connector)(nil)
	var _ sdk.OrderReader = (*Connector)(nil)
}
