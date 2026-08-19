package magnitmarket

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

//go:embed fixtures/shops.json
var shopsFixture []byte

//go:embed fixtures/products-page-1.json
var productsPageOne []byte

//go:embed fixtures/products-last.json
var productsLast []byte

//go:embed fixtures/prices-page-1.json
var pricesPageOne []byte

//go:embed fixtures/prices-last.json
var pricesLast []byte

//go:embed fixtures/short-page-1.json
var shortPageOne []byte

//go:embed fixtures/short-last.json
var shortLast []byte

//go:embed fixtures/stocks.json
var stocksFixture []byte

//go:embed fixtures/orders-page-1.json
var ordersPageOne []byte

//go:embed fixtures/orders-last.json
var ordersLast []byte

type staticConfig struct{ value Configuration }

func (source staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return source.value, nil
}

type testSecrets struct{ value []byte }

func (secrets testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secrets.value...)
	defer clear(value)
	return callback(value)
}

type testRuntime struct{ value []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{runtime.value} }

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (transport *scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	transport.requests = append(transport.requests, request)
	index := len(transport.requests) - 1
	if index < len(transport.errs) && transport.errs[index] != nil {
		return Response{}, transport.errs[index]
	}
	if index >= len(transport.responses) {
		return Response{StatusCode: 500}, nil
	}
	return transport.responses[index], nil
}

func config() Configuration {
	return Configuration{ShopID: 42, StockType: StockTypeFBS, OrderWindowDays: 30}
}
func account() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "magnit-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "magnit-market", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}
func apiKey() []byte      { return []byte("synthetic-magnit-market-key-0123456789abcdef") }
func fixedNow() time.Time { return time.Date(2026, 8, 10, 19, 0, 0, 0, time.UTC) }

func TestManifestMatchesAndReadOnly(t *testing.T) {
	var got sdk.Manifest
	if json.Unmarshal(manifestJSON, &got) != nil || got.Validate() != nil {
		t.Fatal("invalid manifest")
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatal("manifest drift")
	}
	for _, capability := range []sdk.Capability{"products.write", "prices.write", "inventory.write", "orders.status.write"} {
		if Manifest().Supports(capability) {
			t.Fatalf("write capability %s admitted", capability)
		}
	}
	for _, capability := range []sdk.Capability{"products.read", "prices.read", "inventory.read", "orders.read"} {
		if !Manifest().Supports(capability) {
			t.Fatalf("read capability %s missing", capability)
		}
	}
}

func TestConfigurationAndAPIKeyStrict(t *testing.T) {
	if config().Validate() != nil {
		t.Fatal("valid configuration rejected")
	}
	bad := config()
	bad.ShopID = 0
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("bad shop accepted")
	}
	bad = config()
	bad.StockType = "UNKNOWN"
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("bad stock type accepted")
	}
	bad = config()
	bad.OrderWindowDays = 91
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("bad window accepted")
	}
	for _, value := range [][]byte{nil, []byte("short"), []byte(" key with spaces "), []byte("abc\ndefghijklmnop")} {
		if validAPIKey(value) {
			t.Fatalf("bad key accepted: %q", value)
		}
	}
	if !validAPIKey(apiKey()) {
		t.Fatal("good key rejected")
	}
}

func TestHealthValidatesConfiguredShop(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: shopsFixture}}}
	health, err := New(transport, staticConfig{config()}, fixedNow).Health(context.Background(), account(), testRuntime{apiKey()})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Path != "/api/seller/v1/shops" || transport.requests[0].Host != apiHost || len(transport.requests[0].APIKey) == 0 {
		t.Fatalf("request=%+v", transport.requests)
	}

	missing := config()
	missing.ShopID = 999
	health, err = New(&scriptedTransport{responses: []Response{{StatusCode: 200, Body: shopsFixture}}}, staticConfig{missing}, fixedNow).Health(context.Background(), account(), testRuntime{apiKey()})
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "remote_contract_invalid" {
		t.Fatalf("missing shop health=%+v err=%v", health, err)
	}
}

func TestProductsUseShopFilterAndPriceTimestamp(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{
		{StatusCode: 200, Body: productsPageOne}, {StatusCode: 200, Body: pricesPageOne},
		{StatusCode: 200, Body: productsLast}, {StatusCode: 200, Body: pricesLast},
	}}
	connector := New(transport, staticConfig{config()}, fixedNow)
	page, err := connector.ReadProducts(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].RemoteID != "1001:2001" || page.Items[0].Variants[0].RemoteID != "2001" || page.Items[0].SellerSKU != "MAG-RED-M" || page.Items[0].UpdatedAt.Format(time.RFC3339) != "2026-08-10T18:00:00Z" || page.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if !strings.Contains(string(transport.requests[0].Body), `"shop_id":42`) || transport.requests[0].Path != "/api/seller/v1/products/sku/list" || transport.requests[1].Path != "/api/seller/v1/products/sku/price/info" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	last, err := connector.ReadProducts(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.Items[0].Variants[0].RemoteID != "2003" || last.NextCursor != "" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
}

func TestPricesUseShopScopedKeyset(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{
		{StatusCode: 200, Body: shortPageOne}, {StatusCode: 200, Body: pricesPageOne},
		{StatusCode: 200, Body: shortLast}, {StatusCode: 200, Body: pricesLast},
	}}
	connector := New(transport, staticConfig{config()}, fixedNow)
	page, err := connector.ReadPrices(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].VariantRemoteID != "2001" || page.Items[0].Value != "999.90" || page.Items[0].CompareAt != "1299.00" || page.Items[0].Currency != "RUB" || page.NextCursor == "" {
		t.Fatalf("prices=%+v err=%v", page, err)
	}
	if transport.requests[0].Path != "/api/seller/v1/products/sku/shops/42/short/list" || transport.requests[1].Path != "/api/seller/v1/products/sku/price/info" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	last, err := connector.ReadPrices(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.Items[0].VariantRemoteID != "2003" || last.NextCursor != "" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
}

func TestInventoryUsesExplicitAggregateStockType(t *testing.T) {
	connector := New(&scriptedTransport{}, staticConfig{config()}, fixedNow)
	locations, err := connector.ListInventoryLocations(context.Background(), account(), testRuntime{apiKey()})
	if err != nil || len(locations) != 1 || locations[0].RemoteID != "shop:42:stock-type:FBS" {
		t.Fatalf("locations=%+v err=%v", locations, err)
	}

	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: stocksFixture}}}
	values, err := New(transport, staticConfig{config()}, fixedNow).ReadInventory(context.Background(), account(), testRuntime{apiKey()}, sdk.InventoryQuery{LocationRemoteID: "shop:42:stock-type:FBS", VariantRemoteIDs: []string{"2001", "2002"}})
	if err != nil || len(values) != 2 || values[0].Quantity != 8 || values[1].Quantity != 3 {
		t.Fatalf("inventory=%+v err=%v", values, err)
	}
	if transport.requests[0].Path != "/api/seller/v1/products/sku/stocks/info" {
		t.Fatalf("path=%s", transport.requests[0].Path)
	}

	if _, err := New(transport, staticConfig{config()}, fixedNow).ReadInventory(context.Background(), account(), testRuntime{apiKey()}, sdk.InventoryQuery{LocationRemoteID: "warehouse:invented", VariantRemoteIDs: []string{"2001"}}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("invented location accepted: %v", err)
	}
}

func TestOrdersUseStableWindowAndExcludeBuyerData(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: ordersPageOne}, {StatusCode: 200, Body: ordersLast}}}
	connector := New(transport, staticConfig{config()}, fixedNow)
	page, err := connector.ReadOrders(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].RemoteID != "123e4567-e89b-12d3-a456-426614174111" || page.Items[0].ProgramRemoteID != "FBS" || len(page.Items[0].Items) != 2 || page.NextCursor == "" {
		t.Fatalf("orders=%+v err=%v", page, err)
	}
	encoded, _ := json.Marshal(page)
	text := string(encoded)
	if strings.Contains(text, "SHOULD_NOT_BE_EXPOSED") || strings.Contains(text, "customer_id") || strings.Contains(text, "delivery_region") {
		t.Fatal("buyer data leaked")
	}
	firstBody := string(transport.requests[0].Body)
	if transport.requests[0].Path != "/api/seller/v1/orders/list" || !strings.Contains(firstBody, `"from":"2026-07-11T19:00:00Z"`) || !strings.Contains(firstBody, `"to":"2026-08-10T19:00:00Z"`) {
		t.Fatalf("body=%s", firstBody)
	}
	last, err := connector.ReadOrders(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	secondBody := string(transport.requests[1].Body)
	if !strings.Contains(secondBody, `"page_token":"magnit-orders-next-1"`) || !strings.Contains(secondBody, `"from":"2026-07-11T19:00:00Z"`) || !strings.Contains(secondBody, `"to":"2026-08-10T19:00:00Z"`) {
		t.Fatalf("second body=%s", secondBody)
	}
}

func TestFailClosedMalformedRemoteDataAndCursor(t *testing.T) {
	connector := New(&scriptedTransport{}, staticConfig{config()}, fixedNow)
	if _, err := connector.ReadProducts(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2, Cursor: "%%%"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("cursor=%v", err)
	}

	transport := &scriptedTransport{responses: []Response{{StatusCode: 429, RequestID: "magnit-req", RetryAfterMS: 1000, Body: []byte(`{"errors":[{"message":"X-Api-Key=secret"}]}`)}}}
	_, err := New(transport, staticConfig{config()}, fixedNow).ReadPrices(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || strings.Contains(remote.Error(), "secret") {
		t.Fatalf("remote=%v", err)
	}

	malformedProducts := []byte(`{"result":[{"barcode":1,"product_id":1,"seller_sku_id":"A","sku_id":2,"title":"A","is_active":true},{"barcode":2,"product_id":2,"seller_sku_id":"B","sku_id":2,"title":"B","is_active":true}]}`)
	transport = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: malformedProducts}}}
	_, err = New(transport, staticConfig{config()}, fixedNow).ReadProducts(context.Background(), account(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate sku=%v", err)
	}

	badStock := []byte(`{"result":[{"seller_sku_id":"A","sku_id":2001,"stock_info_details":[{"reserved":11,"stock":10,"type":"FBS"}]}]}`)
	transport = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: badStock}}}
	_, err = New(transport, staticConfig{config()}, fixedNow).ReadInventory(context.Background(), account(), testRuntime{apiKey()}, sdk.InventoryQuery{LocationRemoteID: "shop:42:stock-type:FBS", VariantRemoteIDs: []string{"2001"}})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("reserved > stock accepted: %v", err)
	}
}

func TestPriceAndProductResponseCollisionsFailClosed(t *testing.T) {
	duplicatePrice := []byte(`{"result":[{"currency_code":"RUB","price":100,"seller_sku_id":"A","sku_id":2001,"timestamp":"2026-08-10T18:00:00Z"},{"currency_code":"RUB","price":100,"seller_sku_id":"A","sku_id":2001,"timestamp":"2026-08-10T18:00:00Z"}]}`)
	if _, err := parsePrices(duplicatePrice, []int64{2001, 2002}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate price accepted: %v", err)
	}
	exponentPrice := []byte(`{"result":[{"currency_code":"RUB","price":1e3,"seller_sku_id":"A","sku_id":2001,"timestamp":"2026-08-10T18:00:00Z"}]}`)
	if _, err := parsePrices(exponentPrice, []int64{2001}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("exponent price accepted: %v", err)
	}
}

func TestInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.PriceReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
	var _ sdk.OrderReader = (*Connector)(nil)
}
