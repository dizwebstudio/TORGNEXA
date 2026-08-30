package prestashop

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "ps-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "prestashop", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type testRuntime struct{ secret []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets(r.secret) }

type testSecrets []byte

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s...)
	defer clear(v)
	return cb(v)
}

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "EUR", LanguageID: 1}, nil
}
func credentialJSON() []byte { return []byte(`{"api_key":"PS123456789012345678901234567890"}`) }

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (s scriptedTransport) Do(_ context.Context, r Request) (Response, error) { return s.fn(r) }
func TestManifestAndInterfaces(t *testing.T) {
	if e := Manifest().Validate(); e != nil {
		t.Fatal(e)
	}
	var c any = New(scriptedTransport{}, testConfig{}, nil)
	for name, ok := range map[string]bool{"ProductReader": implementsProductReader(c), "PriceReader": implementsPriceReader(c), "PriceWriter": implementsPriceWriter(c), "InventoryReader": implementsInventoryReader(c), "InventoryWriter": implementsInventoryWriter(c), "OrderReader": implementsOrderReader(c), "OrderStatusWriter": implementsOrderStatusWriter(c)} {
		if !ok {
			t.Fatal(name + " missing")
		}
	}
}

func TestOrderStatusConfigurationRequiresCompleteUniqueIDs(t *testing.T) {
	valid := Configuration{OrderStatusMapping: map[string]string{
		"pending": "1", "confirmed": "2", "processing": "3", "fulfilled": "4", "cancelled": "5",
	}}
	if _, err := valid.OrderStatuses(); err != nil {
		t.Fatalf("valid status configuration rejected: %v", err)
	}
	for name, mapping := range map[string]map[string]string{
		"missing state": {"pending": "1", "confirmed": "2", "processing": "3", "fulfilled": "4"},
		"duplicate id":  {"pending": "1", "confirmed": "1", "processing": "3", "fulfilled": "4", "cancelled": "5"},
		"zero id":       {"pending": "0", "confirmed": "2", "processing": "3", "fulfilled": "4", "cancelled": "5"},
		"non numeric":   {"pending": "1", "confirmed": "two", "processing": "3", "fulfilled": "4", "cancelled": "5"},
	} {
		if _, err := (Configuration{OrderStatusMapping: mapping}).OrderStatuses(); err == nil {
			t.Fatalf("%s configuration was accepted", name)
		}
	}
}
func implementsProductReader(v any) bool     { _, ok := v.(sdk.ProductReader); return ok }
func implementsPriceReader(v any) bool       { _, ok := v.(sdk.PriceReader); return ok }
func implementsPriceWriter(v any) bool       { _, ok := v.(sdk.PriceWriter); return ok }
func implementsInventoryReader(v any) bool   { _, ok := v.(sdk.InventoryReader); return ok }
func implementsInventoryWriter(v any) bool   { _, ok := v.(sdk.InventoryWriter); return ok }
func implementsOrderReader(v any) bool       { _, ok := v.(sdk.OrderReader); return ok }
func implementsOrderStatusWriter(v any) bool { _, ok := v.(sdk.OrderStatusWriter); return ok }
func TestReadProductsUsesCombinationReferences(t *testing.T) {
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		if r.Path == "/api/products" {
			return Response{StatusCode: 200, Body: []byte(`{"products":[{"id":"7","reference":"","name":"Boot","price":"10.00","active":"1","date_upd":"2026-08-12 08:00:00"}]}`)}, nil
		}
		if r.Path == "/api/combinations" {
			return Response{StatusCode: 200, Body: []byte(`{"combinations":[{"id":"11","id_product":"7","reference":"BOOT-RED","price":"2.00","date_upd":"2026-08-12 08:01:00"}]}`)}, nil
		}
		return Response{StatusCode: 404}, nil
	}}
	c := New(tr, testConfig{}, nil)
	page, e := c.ReadProducts(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.PageRequest{Limit: 10})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 1 || page.Items[0].SellerSKU != "BOOT-RED" || page.Items[0].Variants[0].RemoteID != "combination:7:11" {
		t.Fatalf("unexpected %#v", page)
	}
}
func TestWriteInventoryUsesStockAvailablePatch(t *testing.T) {
	calls := 0
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		calls++
		if r.Method == "GET" && r.Path == "/api/stock_availables" {
			qty := "3"
			if calls > 1 {
				qty = "9"
			}
			return Response{StatusCode: 200, Body: []byte(`{"stock_availables":[{"id":"44","id_product":"7","id_product_attribute":"0","quantity":"` + qty + `"}]}`)}, nil
		}
		if r.Method == "PATCH" && r.Path == "/api/stock_availables/44" {
			if !strings.Contains(string(r.Body), "<quantity>9</quantity>") {
				t.Fatalf("body=%s", r.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
		}
		return Response{StatusCode: 404}, nil
	}}
	c := New(tr, testConfig{}, nil)
	receipt, e := c.WriteInventory(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.InventoryWriteRequest{VariantRemoteID: "product:7", Quantity: 9, IdempotencyKey: "stock-7"})
	if e != nil {
		t.Fatal(e)
	}
	if !receipt.Applied {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestOfficialWebservicePluralEnvelopeForSingleResources(t *testing.T) {
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		switch {
		case r.Method == "GET" && r.Path == "/api/products/7":
			return Response{StatusCode: 200, Body: []byte(`{"products":[{"id":7,"reference":"BOOT-RED","name":"Boot","price":"10.00","active":"1","date_upd":"2026-08-12 08:00:00"}]}`)}, nil
		case r.Method == "GET" && r.Path == "/api/combinations/11":
			return Response{StatusCode: 200, Body: []byte(`{"combinations":[{"id":11,"id_product":7,"reference":"BOOT-RED","price":"2.00","date_upd":"2026-08-12 08:01:00"}]}`)}, nil
		case r.Method == "GET" && r.Path == "/api/orders/9001":
			return Response{StatusCode: 200, Body: []byte(`{"orders":[{"id":9001,"current_state":2}]}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	c := New(tr, testConfig{}, nil)
	cfg := Configuration{StoreHost: "shop.example.com", StoreCurrency: "EUR", LanguageID: 1}
	cred := credentials{APIKey: "PS123456789012345678901234567890"}
	product, err := c.fetchProduct(context.Background(), cfg, cred, 7)
	if err != nil || product.Reference != "BOOT-RED" {
		t.Fatalf("product envelope: %#v, %v", product, err)
	}
	combination, err := c.fetchCombination(context.Background(), cfg, cred, 7, 11)
	if err != nil || combination.Reference != "BOOT-RED" {
		t.Fatalf("combination envelope: %#v, %v", combination, err)
	}
	state, err := c.fetchOrderState(context.Background(), cfg, cred, 9001)
	if err != nil || state != "2" {
		t.Fatalf("order envelope: %q, %v", state, err)
	}
}

func TestOrdersReadAndStateWriteUseOrderHistory(t *testing.T) {
	state := "2"
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		switch {
		case r.Method == "GET" && r.Path == "/api/orders":
			return Response{StatusCode: 200, Body: []byte(`{"orders":[{"id":9001,"reference":"REF-1","current_state":2,"date_add":"2026-08-12 08:00:00","date_upd":"2026-08-12 08:05:00"}]}`)}, nil
		case r.Method == "GET" && r.Path == "/api/order_details":
			return Response{StatusCode: 200, Body: []byte(`{"order_details":[{"id":1,"id_order":9001,"product_id":7,"product_attribute_id":0,"product_quantity":2}]}`)}, nil
		case r.Method == "GET" && r.Path == "/api/orders/9001":
			return Response{StatusCode: 200, Body: []byte(`{"orders":[{"id":9001,"current_state":` + state + `}]}`)}, nil
		case r.Method == "POST" && r.Path == "/api/order_histories":
			if !strings.Contains(string(r.Body), "<id_order_state>3</id_order_state>") || !strings.Contains(string(r.Body), "<id_order>9001</id_order>") {
				t.Fatalf("unexpected order history body: %s", r.Body)
			}
			state = "3"
			return Response{StatusCode: 201, Body: []byte(`{}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	c := New(tr, testConfig{}, nil)
	page, err := c.ReadOrders(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.PageRequest{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].StatusRemoteID != "2" || len(page.Items[0].Items) != 1 {
		t.Fatalf("unexpected order page: %#v, %v", page, err)
	}
	receipt, err := c.WriteOrderStatus(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.OrderStatusWriteRequest{OrderRemoteID: "9001", StatusRemoteID: "3", IdempotencyKey: "order-status-9001"})
	if err != nil || !receipt.Applied || !receipt.Reconciled {
		t.Fatalf("unexpected order status receipt: %#v, %v", receipt, err)
	}
}
