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
