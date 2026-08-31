package medusa

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "medusa-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "medusa", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type testRuntime struct{ secret []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets(runtime.secret) }

type testSecrets []byte

func (secret testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secret...)
	defer clear(value)
	return callback(value)
}

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "USD"}, nil
}

const testToken = "sk_0123456789abcdef0123456789abcdef"

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (transport scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	return transport.fn(request)
}

func TestManifestAndInterfaces(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []sdk.Capability{"products.read", "products.write", "prices.read", "prices.write", "inventory.read", "inventory.write", "orders.read", "orders.status.write", "returns.read", "notifications.receive"} {
		if !Manifest().Supports(capability) {
			t.Fatalf("missing %s", capability)
		}
	}
	var connector any = New(scriptedTransport{}, testConfig{}, nil)
	for _, check := range []struct {
		name string
		ok   bool
	}{
		{"ProductReader", func() bool { _, ok := connector.(sdk.ProductReader); return ok }()},
		{"ProductWriter", func() bool { _, ok := connector.(sdk.ProductWriter); return ok }()},
		{"PriceReader", func() bool { _, ok := connector.(sdk.PriceReader); return ok }()},
		{"PriceWriter", func() bool { _, ok := connector.(sdk.PriceWriter); return ok }()},
		{"InventoryReader", func() bool { _, ok := connector.(sdk.InventoryReader); return ok }()},
		{"InventoryWriter", func() bool { _, ok := connector.(sdk.InventoryWriter); return ok }()},
		{"OrderReader", func() bool { _, ok := connector.(sdk.OrderReader); return ok }()},
		{"OrderStatusWriter", func() bool { _, ok := connector.(sdk.OrderStatusWriter); return ok }()},
		{"ReturnReader", func() bool { _, ok := connector.(sdk.ReturnReader); return ok }()},
		{"CommerceWebhookReceiver", func() bool { _, ok := connector.(sdk.CommerceWebhookReceiver); return ok }()},
	} {
		if !check.ok {
			t.Fatalf("%s missing", check.name)
		}
	}
}

func TestReadProductsInlineVariantsAndOffsetCursor(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if request.Path != "/admin/products" {
			t.Fatalf("path=%s", request.Path)
		}
		if string(request.Bearer) != testToken {
			t.Fatalf("bearer=%s", request.Bearer)
		}
		if calls == 1 {
			return Response{StatusCode: 200, Body: []byte(`{"products":[{"id":"prod_1","title":"Boot","status":"published","updated_at":"2026-08-12T07:00:00Z","variants":[{"id":"variant_1","sku":"BOOT-1","prices":[{"currency_code":"usd","amount":10.5}]}]}],"count":2}`)}, nil
		}
		for _, q := range request.Query {
			if q.Name == "offset" && q.Value != "1" {
				t.Fatalf("offset=%s", q.Value)
			}
		}
		return Response{StatusCode: 200, Body: []byte(`{"products":[],"count":2}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	first, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].Variants[0].RemoteID != "variant:prod_1:variant_1" || first.Items[0].SellerSKU != "BOOT-1" || first.NextCursor == "" {
		t.Fatalf("unexpected %#v", first)
	}
	second, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 0 || second.NextCursor != "" {
		t.Fatalf("unexpected second page %#v err=%v", second, err)
	}
}

func TestReadPricesFiltersByStoreCurrency(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		return Response{StatusCode: 200, Body: []byte(`{"products":[{"id":"prod_1","title":"Boot","status":"published","updated_at":"2026-08-12T07:00:00Z","variants":[{"id":"variant_1","sku":"BOOT-1","prices":[{"currency_code":"eur","amount":9},{"currency_code":"usd","amount":10.5}]}]}],"count":1}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	page, err := connector.ReadPrices(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Value != "10.5" || page.Items[0].Currency != "USD" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestWebhookIsUnsupported(t *testing.T) {
	connector := New(scriptedTransport{}, testConfig{}, nil)
	request := sdk.CommerceWebhookRequest{Signature: "0123456789abcdef", HeaderTopic: "order.updated", ExpectedTopic: "order.updated", Body: []byte(`{"id":1}`), ReceivedAt: time.Now().UTC()}
	_, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, request, &memoryDedup{seen: map[string]bool{}})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
}

type memoryDedup struct{ seen map[string]bool }

func (dedup *memoryDedup) ClaimCommerceWebhook(_ context.Context, _ sdk.Account, claim sdk.CommerceWebhookClaim) (bool, error) {
	duplicate := dedup.seen[claim.DeliveryID]
	dedup.seen[claim.DeliveryID] = true
	return duplicate, nil
}

func TestUpsertProductCreateIsUnsupported(t *testing.T) {
	connector := New(scriptedTransport{}, testConfig{}, nil)
	_, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{SellerSKU: "NEW-1", Title: "New", StatusRemoteID: "draft", IdempotencyKey: "create-1"})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
}

func TestUpsertProductUpdatesTitleAndStatus(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if request.Method == "GET" {
			if calls == 1 {
				return Response{StatusCode: 200, Body: []byte(`{"product":{"id":"prod_99","title":"Old","status":"draft","description":"d","variants":[{"id":"variant_990","sku":"BOOT-99"}]}}`)}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`{"product":{"id":"prod_99","title":"Boot","status":"published","description":"d","variants":[{"id":"variant_990","sku":"BOOT-99"}]}}`)}, nil
		}
		if request.Method != "POST" || request.Path != "/admin/products/prod_99" {
			t.Fatalf("method=%s path=%s", request.Method, request.Path)
		}
		return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{RemoteID: "prod_99", SellerSKU: "BOOT-99", Title: "Boot", Description: "d", StatusRemoteID: "published", IdempotencyKey: "update-99"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "prod_99" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestWritePriceIdempotentAndApplied(t *testing.T) {
	state := `{"variant":{"id":"variant_1","prices":[{"currency_code":"usd","amount":9}]}}`
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method == "GET" {
			return Response{StatusCode: 200, Body: []byte(state)}, nil
		}
		if request.Method != "POST" || request.Path != "/admin/products/prod_1/variants/variant_1" {
			t.Fatalf("method=%s path=%s", request.Method, request.Path)
		}
		state = `{"variant":{"id":"variant_1","prices":[{"currency_code":"usd","amount":10}]}}`
		return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PriceWriteRequest{VariantRemoteID: "variant:prod_1:variant_1", Value: "10", Currency: "USD", IdempotencyKey: "price-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied {
		t.Fatalf("unexpected %#v", receipt)
	}
	second, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PriceWriteRequest{VariantRemoteID: "variant:prod_1:variant_1", Value: "10", Currency: "USD", IdempotencyKey: "price-2"})
	if err != nil || !second.Duplicate {
		t.Fatalf("expected duplicate, got %#v err=%v", second, err)
	}
}

func TestWriteInventoryResolvesBySKUAndPrimaryLocation(t *testing.T) {
	level := `9`
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch {
		case request.Method == "GET" && request.Path == "/admin/stock-locations":
			return Response{StatusCode: 200, Body: []byte(`{"stock_locations":[{"id":"loc_1","name":"Main"}]}`)}, nil
		case request.Method == "GET" && request.Path == "/admin/products/prod_1/variants/variant_1":
			return Response{StatusCode: 200, Body: []byte(`{"variant":{"id":"variant_1","sku":"BOOT-1"}}`)}, nil
		case request.Method == "GET" && request.Path == "/admin/inventory-items":
			return Response{StatusCode: 200, Body: []byte(`{"inventory_items":[{"id":"iitem_1","sku":"BOOT-1","location_levels":[{"location_id":"loc_1","stocked_quantity":` + level + `,"available_quantity":` + level + `}]}]}`)}, nil
		case request.Method == "POST" && request.Path == "/admin/inventory-items/iitem_1/location-levels/loc_1":
			level = "12"
			return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.InventoryWriteRequest{VariantRemoteID: "variant:prod_1:variant_1", Quantity: 12, IdempotencyKey: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestWriteOrderStatusCancel(t *testing.T) {
	cancelled := false
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method == "GET" {
			status := "pending"
			if cancelled {
				status = "canceled"
			}
			return Response{StatusCode: 200, Body: []byte(`{"order":{"id":"order_5","status":"` + status + `"}}`)}, nil
		}
		if request.Path != "/admin/orders/order_5/cancel" {
			t.Fatalf("path=%s", request.Path)
		}
		cancelled = true
		return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteOrderStatus(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.OrderStatusWriteRequest{OrderRemoteID: "order_5", StatusRemoteID: "canceled", IdempotencyKey: "cancel-5"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "order_5" {
		t.Fatalf("unexpected %#v", receipt)
	}
}
