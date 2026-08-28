package magento

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "magento-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "magento", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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

const testToken = "testintegrationaccesstoken0123456789"

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (transport scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	return transport.fn(request)
}

func queryValue(request Request, name string) (string, bool) {
	for _, q := range request.Query {
		if q.Name == name {
			return q.Value, true
		}
	}
	return "", false
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

func TestReadProductsFlattensToOneVariantEach(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Path != "/rest/V1/products" {
			t.Fatalf("path=%s", request.Path)
		}
		if string(request.Bearer) != testToken {
			t.Fatalf("bearer=%s", request.Bearer)
		}
		return Response{StatusCode: 200, Body: []byte(`{"items":[{"sku":"BOOT-1","name":"Boot","price":19.99,"status":1,"updated_at":"2026-08-12 07:00:00"}],"total_count":1}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "BOOT-1" || page.Items[0].Variants[0].RemoteID != "BOOT-1" || page.Items[0].SellerSKU != "BOOT-1" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestReadProductsPagesUsingSearchCriteria(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		page, _ := queryValue(request, "searchCriteria[currentPage]")
		if calls == 1 {
			if page != "1" {
				t.Fatalf("page=%s", page)
			}
			return Response{StatusCode: 200, Body: []byte(`{"items":[{"sku":"A","name":"A","price":1,"status":1,"updated_at":"2026-08-12 07:00:00"}],"total_count":2}`)}, nil
		}
		if page != "2" {
			t.Fatalf("page=%s", page)
		}
		return Response{StatusCode: 200, Body: []byte(`{"items":[{"sku":"B","name":"B","price":2,"status":1,"updated_at":"2026-08-12 07:00:00"}],"total_count":2}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	first, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || second.NextCursor != "" || len(second.Items) != 1 {
		t.Fatalf("second=%#v err=%v", second, err)
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
	_, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{SellerSKU: "NEW-1", Title: "New", StatusRemoteID: "enabled", IdempotencyKey: "create-1"})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
}

func TestUpsertProductUpdatesInPlace(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Path == "/rest/V1/products/BOOT-1" {
			calls++
			if request.Method == "GET" {
				if calls == 1 {
					return Response{StatusCode: 200, Body: []byte(`{"sku":"BOOT-1","name":"Old","status":2,"custom_attributes":[{"attribute_code":"description","value":"d"}]}`)}, nil
				}
				return Response{StatusCode: 200, Body: []byte(`{"sku":"BOOT-1","name":"Boot","status":1,"custom_attributes":[{"attribute_code":"description","value":"d"}]}`)}, nil
			}
			if request.Method == "PUT" {
				return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
			}
		}
		return Response{StatusCode: 404}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{RemoteID: "BOOT-1", SellerSKU: "BOOT-1", Title: "Boot", Description: "d", StatusRemoteID: "enabled", IdempotencyKey: "update-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "BOOT-1" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestWriteOrderStatusCancel(t *testing.T) {
	cancelled := false
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Path == "/rest/V1/orders/5" && request.Method == "GET" {
			status := "processing"
			if cancelled {
				status = "canceled"
			}
			return Response{StatusCode: 200, Body: []byte(`{"entity_id":5,"status":"` + status + `"}`)}, nil
		}
		if request.Path == "/rest/V1/orders/5/cancel" {
			cancelled = true
			return Response{StatusCode: 200, Body: []byte(`true`)}, nil
		}
		return Response{StatusCode: 404}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteOrderStatus(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.OrderStatusWriteRequest{OrderRemoteID: "5", StatusRemoteID: "canceled", IdempotencyKey: "cancel-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "5" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestWriteInventorySingleLocation(t *testing.T) {
	qty := `3`
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch {
		case request.Method == "GET" && request.Path == "/rest/V1/stockItems/BOOT-1":
			return Response{StatusCode: 200, Body: []byte(`{"item_id":10,"product_id":1,"qty":` + qty + `,"is_in_stock":true}`)}, nil
		case request.Method == "PUT" && request.Path == "/rest/V1/products/BOOT-1/stockItems/10":
			qty = "12"
			return Response{StatusCode: 200, Body: []byte(`12`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.InventoryWriteRequest{VariantRemoteID: "BOOT-1", Quantity: 12, IdempotencyKey: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied {
		t.Fatalf("unexpected %#v", receipt)
	}
}
