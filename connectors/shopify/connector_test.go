package shopify

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "shopify-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "shopify", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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
	return Configuration{ShopDomain: "acme.myshopify.com", StoreCurrency: "USD"}, nil
}

const testToken = "shopify-test-token"

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

func TestReadProductsInlineVariants(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Path != "/admin/api/"+apiVersion+"/products.json" {
			t.Fatalf("path=%s", request.Path)
		}
		if string(request.Bearer) != testToken {
			t.Fatalf("bearer=%s", request.Bearer)
		}
		return Response{StatusCode: 200, Body: []byte(`{"products":[{"id":7,"title":"Boot","status":"active","vendor":"Acme","updated_at":"2026-08-12T07:00:00-00:00","variants":[{"id":70,"sku":"BOOT-7","price":"10.00","compare_at_price":"","inventory_item_id":700,"updated_at":"2026-08-12T07:00:00-00:00"}]}]}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Variants[0].RemoteID != "variant:70" || page.Items[0].SellerSKU != "BOOT-7" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestReadProductsPagesViaLinkHeaderCursor(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		hasPageInfo := false
		for _, q := range request.Query {
			if q.Name == "page_info" {
				hasPageInfo = true
				if q.Value != "cursor-token" {
					t.Fatalf("page_info=%s", q.Value)
				}
			}
		}
		if calls == 1 {
			if hasPageInfo {
				t.Fatal("first page must not carry page_info")
			}
			return Response{StatusCode: 200, Body: []byte(`{"products":[{"id":1,"title":"A","status":"active","updated_at":"2026-08-12T07:00:00-00:00","variants":[{"id":10,"sku":"A-1","price":"1.00","updated_at":"2026-08-12T07:00:00-00:00"}]}]}`), NextPageInfo: "cursor-token"}, nil
		}
		if !hasPageInfo {
			t.Fatal("second page must carry the returned page_info")
		}
		return Response{StatusCode: 200, Body: []byte(`{"products":[]}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	first, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 10})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page = %#v err=%v", first, err)
	}
	second, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 10, Cursor: first.NextCursor})
	if err != nil || second.NextCursor != "" || len(second.Items) != 0 {
		t.Fatalf("second page = %#v err=%v", second, err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
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

func (dedup *memoryDedup) ClaimCommerceWebhook(_ context.Context, _ sdk.Account, id, _ string, _ time.Time) (bool, error) {
	duplicate := dedup.seen[id]
	dedup.seen[id] = true
	return duplicate, nil
}

func TestUpsertProductCreateIsUnsupported(t *testing.T) {
	connector := New(scriptedTransport{}, testConfig{}, nil)
	_, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{SellerSKU: "NEW-1", Title: "New", StatusRemoteID: "active", IdempotencyKey: "create-1"})
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
				return Response{StatusCode: 200, Body: []byte(`{"product":{"id":99,"title":"Old","status":"draft","variants":[{"id":990,"sku":"BOOT-99"}]}}`)}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`{"product":{"id":99,"title":"Boot","status":"active","variants":[{"id":990,"sku":"BOOT-99"}]}}`)}, nil
		}
		if request.Method != "PUT" || request.Path != "/admin/api/"+apiVersion+"/products/99.json" {
			t.Fatalf("method=%s path=%s", request.Method, request.Path)
		}
		return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{RemoteID: "99", SellerSKU: "BOOT-99", Title: "Boot", StatusRemoteID: "active", IdempotencyKey: "update-99"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "99" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestUpsertProductRejectsSKUChange(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		return Response{StatusCode: 200, Body: []byte(`{"product":{"id":99,"title":"Boot","status":"active","variants":[{"id":990,"sku":"BOOT-99"}]}}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	_, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{RemoteID: "99", SellerSKU: "DIFFERENT-SKU", Title: "Boot", StatusRemoteID: "active", IdempotencyKey: "update-99"})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
}

func TestWritePriceIdempotentAndApplied(t *testing.T) {
	state := `{"variant":{"id":70,"price":"9.00","compare_at_price":""}}`
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if request.Method == "GET" {
			return Response{StatusCode: 200, Body: []byte(state)}, nil
		}
		if request.Method != "PUT" || request.Path != "/admin/api/"+apiVersion+"/variants/70.json" {
			t.Fatalf("method=%s path=%s", request.Method, request.Path)
		}
		state = `{"variant":{"id":70,"price":"10.00","compare_at_price":""}}`
		return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PriceWriteRequest{VariantRemoteID: "variant:70", Value: "10.00", Currency: "USD", IdempotencyKey: "price-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "variant:70" {
		t.Fatalf("unexpected %#v", receipt)
	}
	second, err := connector.WritePrice(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PriceWriteRequest{VariantRemoteID: "variant:70", Value: "10.00", Currency: "USD", IdempotencyKey: "price-2"})
	if err != nil || !second.Duplicate {
		t.Fatalf("expected duplicate, got %#v err=%v", second, err)
	}
	_ = calls
}

func TestWriteInventoryResolvesInventoryItemAndSets(t *testing.T) {
	level := int64(3)
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch {
		case request.Method == "GET" && request.Path == "/admin/api/"+apiVersion+"/shop.json":
			return Response{StatusCode: 200, Body: []byte(`{"shop":{"primary_location_id":1}}`)}, nil
		case request.Method == "GET" && request.Path == "/admin/api/"+apiVersion+"/variants/70.json":
			return Response{StatusCode: 200, Body: []byte(`{"variant":{"id":70,"inventory_item_id":700}}`)}, nil
		case request.Method == "GET" && request.Path == "/admin/api/"+apiVersion+"/inventory_levels.json":
			return Response{StatusCode: 200, Body: []byte(`{"inventory_levels":[{"inventory_item_id":700,"location_id":1,"available":` + strconv.FormatInt(level, 10) + `}]}`)}, nil
		case request.Method == "POST" && request.Path == "/admin/api/"+apiVersion+"/inventory_levels/set.json":
			level = 12
			return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.InventoryWriteRequest{VariantRemoteID: "variant:70", Quantity: 12, IdempotencyKey: "inv-1"})
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
			if cancelled {
				return Response{StatusCode: 200, Body: []byte(`{"order":{"id":5,"cancelled_at":"2026-08-12T08:00:00-00:00"}}`)}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`{"order":{"id":5,"cancelled_at":null}}`)}, nil
		}
		if request.Path != "/admin/api/"+apiVersion+"/orders/5/cancel.json" {
			t.Fatalf("path=%s", request.Path)
		}
		cancelled = true
		return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteOrderStatus(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.OrderStatusWriteRequest{OrderRemoteID: "5", StatusRemoteID: "cancelled", IdempotencyKey: "cancel-5"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "5" {
		t.Fatalf("unexpected %#v", receipt)
	}
}
