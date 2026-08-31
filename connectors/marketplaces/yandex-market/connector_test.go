package yandexmarket

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

//go:embed fixtures/products-page-1.json
var productsPageOne []byte

//go:embed fixtures/products-last.json
var productsLast []byte

//go:embed fixtures/prices.json
var pricesFixture []byte

//go:embed fixtures/warehouses-page-1.json
var warehousesPageOne []byte

//go:embed fixtures/warehouses-last.json
var warehousesLast []byte

//go:embed fixtures/stocks-partner.json
var stocksPartner []byte

//go:embed fixtures/stocks-campaign.json
var stocksCampaign []byte

//go:embed fixtures/orders.json
var ordersFixture []byte

//go:embed fixtures/notification-order.json
var orderNotification []byte

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (transport *scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	request.Body = append([]byte(nil), request.Body...)
	request.APIKey = append([]byte(nil), request.APIKey...)
	request.Query = append([]QueryParam(nil), request.Query...)
	transport.requests = append(transport.requests, request)
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

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{runtime.secret} }

type testSecrets struct{ value []byte }

func (secrets testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secrets.value...)
	defer clear(value)
	return callback(value)
}

type staticConfig struct{ value Configuration }

func (config staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return config.value, nil
}

func partnerConfig() Configuration {
	return Configuration{BusinessID: 10001, CampaignID: 20001, InventoryMode: InventoryPartnerWarehouses, PriceMode: PriceCampaignUnique}
}
func campaignConfig() Configuration {
	return Configuration{BusinessID: 10001, CampaignID: 20001, InventoryMode: InventoryCampaignWarehouses, PriceMode: PriceBusinessWide, Warehouses: []Warehouse{{ID: 80001, Name: "FBY / grouped warehouse"}}}
}
func testAccount() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "ym-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "yandex-market", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}
func apiKey() []byte { return []byte("synthetic-yandex-market-api-key-0123456789") }

func TestManifestMatchesCommittedJSONAndDeclaresOnlyQualifiedWrites(t *testing.T) {
	var got sdk.Manifest
	if json.Unmarshal(manifestJSON, &got) != nil || got.Validate() != nil {
		t.Fatal("invalid manifest")
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatal("manifest drift")
	}
	if !Manifest().Supports("prices.write") {
		t.Fatal("qualified prices.write capability missing")
	}
	if !Manifest().Supports("inventory.write") {
		t.Fatal("qualified inventory.write capability missing")
	}
	if !Manifest().Supports("products.write") {
		t.Fatal("qualified products.write capability missing")
	}
	for _, capability := range []sdk.Capability{"orders.status.write"} {
		if Manifest().Supports(capability) {
			t.Fatalf("unqualified write capability %s", capability)
		}
	}
}
func TestConfigAndAPIKeyAreStrict(t *testing.T) {
	if partnerConfig().Validate() != nil || campaignConfig().Validate() != nil {
		t.Fatal("valid config rejected")
	}
	bad := partnerConfig()
	bad.BusinessID = 0
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("bad business accepted")
	}
	bad = campaignConfig()
	bad.Warehouses = append(bad.Warehouses, bad.Warehouses[0])
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("duplicate warehouse accepted")
	}
	for _, value := range [][]byte{nil, []byte("short"), []byte(" key-with-space "), []byte("abc\ndefghijklmnop")} {
		if validAPIKey(value) {
			t.Fatalf("bad key accepted %q", value)
		}
	}
	if !validAPIKey(apiKey()) {
		t.Fatal("good key rejected")
	}
}
func TestHealthAndProductsUseCurrentOfferMappings(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"status":"OK","result":{"paging":{},"offerMappings":[]}}`)}, {StatusCode: 200, Body: productsPageOne}, {StatusCode: 200, Body: productsLast}}}
	connector := New(transport, staticConfig{partnerConfig()}, func() time.Time { return time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC) })
	health, err := connector.Health(context.Background(), testAccount(), testRuntime{apiKey()})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].RemoteID != "YM-RED-M" || page.Items[0].SellerSKU != "YM-RED-M" || page.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	page2, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(page2.Items) != 1 || page2.NextCursor != "" {
		t.Fatalf("page2=%+v err=%v", page2, err)
	}
	if transport.requests[0].Path != "/v2/businesses/10001/offer-mappings" || transport.requests[1].Host != apiHost || len(transport.requests[1].APIKey) == 0 {
		t.Fatalf("request=%+v", transport.requests[1])
	}
}
func TestPricesPreserveExactNumbers(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: pricesFixture}}}
	page, err := New(transport, staticConfig{partnerConfig()}, nil).ReadPrices(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].Value != "1999.90" || page.Items[0].CompareAt != "2499" || page.Items[0].VATRemoteID != "7" {
		t.Fatalf("prices=%+v err=%v", page, err)
	}
	if transport.requests[0].Path != "/v2/campaigns/20001/offer-prices" {
		t.Fatalf("path=%s", transport.requests[0].Path)
	}
}
func TestWritePriceUsesQualifiedYandexEndpointAndExactDesiredState(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, RequestID: "ym-write-1", Body: []byte(`{"status":"OK"}`)}}}
	connector := New(transport, staticConfig{partnerConfig()}, nil)
	receipt, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PriceWriteRequest{VariantRemoteID: "YM-RED-M", Value: "1999.90", CompareAt: "2499", Currency: "RUB", IdempotencyKey: "price-1"})
	if err != nil || !receipt.Applied || receipt.Reconciled || receipt.RemoteID != "YM-RED-M" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Path != "/v2/campaigns/20001/offer-prices/updates" || transport.requests[0].Method != "POST" {
		t.Fatalf("request=%+v", transport.requests)
	}
	var body map[string]any
	if json.Unmarshal(transport.requests[0].Body, &body) != nil || strings.Contains(string(transport.requests[0].Body), `"currencyId":"RUB"`) || !strings.Contains(string(transport.requests[0].Body), `"currencyId":"RUR"`) {
		t.Fatalf("body=%s", transport.requests[0].Body)
	}
}

func TestWritePriceBusinessModeAndFailuresFailClosed(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"status":"OK"}`)}}}
	connector := New(transport, staticConfig{campaignConfig()}, nil)
	_, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PriceWriteRequest{VariantRemoteID: "YM-1", Value: "100", Currency: "RUB", IdempotencyKey: "price-2"})
	if err != nil || transport.requests[0].Path != "/v2/businesses/10001/offer-prices/updates" {
		t.Fatalf("path=%s err=%v", transport.requests[0].Path, err)
	}
	if _, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PriceWriteRequest{VariantRemoteID: "YM-1", Value: "100", CompareAt: "125.50", Currency: "RUB", IdempotencyKey: "price-3"}); !errors.Is(err, sdk.ErrInvalidCommerceWrite) {
		t.Fatalf("fractional discount base accepted: %v", err)
	}
	bad := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"status":"ERROR"}`)}}}
	if _, err := New(bad, staticConfig{partnerConfig()}, nil).WritePrice(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PriceWriteRequest{VariantRemoteID: "YM-1", Value: "100", Currency: "RUB", IdempotencyKey: "price-4"}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("bad response err=%v", err)
	}
}

func TestWriteInventoryUsesPartnerWarehouseEndpoint(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"status":"OK"}`)}}}
	connector := New(transport, staticConfig{partnerConfig()}, func() time.Time {
		return time.Date(2026, 8, 10, 15, 4, 5, 123000000, time.UTC)
	})
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.InventoryWriteRequest{
		VariantRemoteID:  "YM-RED-M",
		LocationRemoteID: "70001",
		Quantity:         7,
		IdempotencyKey:   "inventory-partner-1",
	})
	if err != nil || !receipt.Applied || receipt.Reconciled || receipt.RemoteID != "YM-RED-M" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "POST" || transport.requests[0].Path != "/v3/businesses/10001/offers/stocks/update" {
		t.Fatalf("request=%+v", transport.requests)
	}
	var body struct {
		SKUItems []struct {
			SKU                string `json:"sku"`
			PartnerWarehouseID int64  `json:"partnerWarehouseId"`
			Count              int64  `json:"count"`
			UpdatedAt          string `json:"updatedAt"`
		} `json:"skuItems"`
	}
	if err := json.Unmarshal(transport.requests[0].Body, &body); err != nil || len(body.SKUItems) != 1 {
		t.Fatalf("body=%s err=%v", transport.requests[0].Body, err)
	}
	item := body.SKUItems[0]
	if item.SKU != "YM-RED-M" || item.PartnerWarehouseID != 70001 || item.Count != 7 || item.UpdatedAt != "2026-08-10T15:04:05.123Z" {
		t.Fatalf("stock item=%+v", item)
	}
}

func TestWriteInventoryUsesConfiguredCampaignWarehouseEndpoint(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"status":"OK"}`)}}}
	connector := New(transport, staticConfig{campaignConfig()}, func() time.Time {
		return time.Date(2026, 8, 10, 15, 4, 5, 0, time.UTC)
	})
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.InventoryWriteRequest{
		VariantRemoteID:  "YM-BLUE-L",
		LocationRemoteID: "80001",
		Quantity:         0,
		IdempotencyKey:   "inventory-campaign-1",
	})
	if err != nil || !receipt.Applied || receipt.Reconciled || receipt.RemoteID != "YM-BLUE-L" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "PUT" || transport.requests[0].Path != "/v2/campaigns/20001/offers/stocks" {
		t.Fatalf("request=%+v", transport.requests)
	}
	var body struct {
		SKUs []struct {
			SKU   string `json:"sku"`
			Items []struct {
				Count     int64  `json:"count"`
				UpdatedAt string `json:"updatedAt"`
			} `json:"items"`
		} `json:"skus"`
	}
	if err := json.Unmarshal(transport.requests[0].Body, &body); err != nil || len(body.SKUs) != 1 || len(body.SKUs[0].Items) != 1 {
		t.Fatalf("body=%s err=%v", transport.requests[0].Body, err)
	}
	if body.SKUs[0].SKU != "YM-BLUE-L" || body.SKUs[0].Items[0].Count != 0 || body.SKUs[0].Items[0].UpdatedAt != "2026-08-10T15:04:05Z" || strings.Contains(string(transport.requests[0].Body), "partnerWarehouseId") {
		t.Fatalf("stock body=%s", transport.requests[0].Body)
	}
}

func TestWriteInventoryRejectsInvalidWarehouseBoundsAndProviderResponse(t *testing.T) {
	connector := New(&scriptedTransport{}, staticConfig{partnerConfig()}, nil)
	base := sdk.InventoryWriteRequest{VariantRemoteID: "YM-RED-M", LocationRemoteID: "70001", Quantity: 1, IdempotencyKey: "inventory-invalid-1"}
	for _, request := range []sdk.InventoryWriteRequest{
		{VariantRemoteID: base.VariantRemoteID, LocationRemoteID: base.LocationRemoteID, Quantity: maxYandexStockQuantity + 1, IdempotencyKey: base.IdempotencyKey},
		{VariantRemoteID: base.VariantRemoteID, LocationRemoteID: "not-a-number", Quantity: base.Quantity, IdempotencyKey: base.IdempotencyKey},
		{VariantRemoteID: base.VariantRemoteID, LocationRemoteID: "", Quantity: base.Quantity, IdempotencyKey: base.IdempotencyKey},
	} {
		if _, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{apiKey()}, request); !errors.Is(err, sdk.ErrInvalidCommerceWrite) {
			t.Fatalf("invalid request=%+v err=%v", request, err)
		}
	}
	wrongWarehouse := New(&scriptedTransport{}, staticConfig{campaignConfig()}, nil)
	if _, err := wrongWarehouse.WriteInventory(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.InventoryWriteRequest{VariantRemoteID: "YM-RED-M", LocationRemoteID: "70001", Quantity: 1, IdempotencyKey: "inventory-invalid-2"}); !errors.Is(err, sdk.ErrInvalidCommerceWrite) {
		t.Fatalf("unconfigured warehouse err=%v", err)
	}
	bad := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"status":"ERROR"}`)}}}
	if _, err := New(bad, staticConfig{partnerConfig()}, nil).WriteInventory(context.Background(), testAccount(), testRuntime{apiKey()}, base); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("bad response err=%v", err)
	}
}

func TestInventorySupportsPartnerAndConfiguredCampaignWarehouses(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: warehousesPageOne}, {StatusCode: 200, Body: warehousesLast}, {StatusCode: 200, Body: stocksPartner}}}
	connector := New(transport, staticConfig{partnerConfig()}, nil)
	locations, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{apiKey()})
	if err != nil || len(locations) != 2 || locations[1].RemoteID != "70002" {
		t.Fatalf("locations=%+v err=%v", locations, err)
	}
	stock, err := connector.ReadInventory(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.InventoryQuery{LocationRemoteID: "70001", VariantRemoteIDs: []string{"YM-RED-M", "YM-BLUE-L"}})
	if err != nil || len(stock) != 2 || stock[0].Quantity != 7 || stock[1].Quantity != 3 {
		t.Fatalf("stock=%+v err=%v", stock, err)
	}
	campaignTransport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: stocksCampaign}}}
	campaignConnector := New(campaignTransport, staticConfig{campaignConfig()}, nil)
	configured, err := campaignConnector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{apiKey()})
	if err != nil || len(configured) != 1 || configured[0].RemoteID != "80001" {
		t.Fatalf("configured=%+v err=%v", configured, err)
	}
	stock, err = campaignConnector.ReadInventory(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.InventoryQuery{LocationRemoteID: "80001", VariantRemoteIDs: []string{"YM-RED-M", "YM-BLUE-L"}})
	if err != nil || stock[0].Quantity != 5 || stock[1].Quantity != 0 {
		t.Fatalf("campaign stock=%+v err=%v", stock, err)
	}
}
func TestOrdersAreBoundedAndExcludeBuyerPII(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: ordersFixture}}}
	page, err := New(transport, staticConfig{partnerConfig()}, nil).ReadOrders(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].RemoteID != "50001" || page.Items[0].CampaignRemoteID != "20001" || len(page.Items[0].Items) != 2 || page.NextCursor == "" {
		t.Fatalf("orders=%+v err=%v", page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "SHOULD_NOT_BE_PARSED") || strings.Contains(string(encoded), "7999") {
		t.Fatal("buyer PII leaked")
	}
}
func TestNotificationDedupAndScope(t *testing.T) {
	connector := New(nil, staticConfig{partnerConfig()}, nil)
	first, err := connector.DecodeMarketplaceNotification(context.Background(), testAccount(), orderNotification)
	if err != nil || first.Type != "ORDER_STATUS_UPDATED" || first.ResourceRemoteID != "50001" || first.DedupKey == "" {
		t.Fatalf("notification=%+v err=%v", first, err)
	}
	second, err := connector.DecodeMarketplaceNotification(context.Background(), testAccount(), append([]byte(nil), orderNotification...))
	if err != nil || first.DedupKey != second.DedupKey {
		t.Fatal("duplicate key not stable")
	}
	wrong := []byte(`{"notificationType":"ORDER_CREATED","businessId":999,"campaignId":20001,"orderId":1,"createdAt":"2026-08-10T10:00:00Z"}`)
	if _, err := connector.DecodeMarketplaceNotification(context.Background(), testAccount(), wrong); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("scope err=%v", err)
	}
	ping, err := connector.DecodeMarketplaceNotification(context.Background(), testAccount(), []byte(`{"notificationType":"PING","time":"2026-08-10T10:00:00Z"}`))
	if err != nil || ping.ResourceKind != "ping" {
		t.Fatalf("ping=%+v err=%v", ping, err)
	}
	ack, err := connector.NotificationAcknowledgement(time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))
	if err != nil || ack.Version != "1.0.0" || ack.Name != "TORGNEXA" {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
}
func TestCursorAndRemoteErrorsFailClosed(t *testing.T) {
	connector := New(&scriptedTransport{}, staticConfig{partnerConfig()}, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 2, Cursor: "%%%"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("cursor err=%v", err)
	}
	transport := &scriptedTransport{responses: []Response{{StatusCode: 420, RequestID: "ym-req", RetryAfterMS: 1500, Body: []byte(`{"errors":[{"message":"api-key=secret"}]}`)}}}
	_, err := New(transport, staticConfig{partnerConfig()}, nil).ReadPrices(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "rate_limited" || remote.RetryAfterMS != 1500 || strings.Contains(remote.Error(), "secret") {
		t.Fatalf("remote=%#v err=%v", remote, err)
	}
	oversized := make([]byte, maxBodyBytes+1)
	transport = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: oversized}}}
	_, err = New(transport, staticConfig{partnerConfig()}, nil).ReadPrices(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorInternal {
		t.Fatalf("oversize=%v", err)
	}
	transport = &scriptedTransport{responses: []Response{{}}, errs: []error{errors.New("dial api-key=must-not-leak")}}
	_, err = New(transport, staticConfig{partnerConfig()}, nil).ReadPrices(context.Background(), testAccount(), testRuntime{apiKey()}, sdk.PageRequest{Limit: 1})
	if !errors.As(err, &remote) || strings.Contains(remote.Error(), "must-not-leak") {
		t.Fatalf("transport=%v", err)
	}
}
func TestInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
	var _ sdk.PriceReader = (*Connector)(nil)
	var _ sdk.PriceWriter = (*Connector)(nil)
	var _ sdk.InventoryWriter = (*Connector)(nil)
	var _ sdk.OrderReader = (*Connector)(nil)
	var _ sdk.MarketplaceNotificationDecoder = (*Connector)(nil)
}
