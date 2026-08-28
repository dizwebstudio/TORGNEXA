package shopware

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "shopware-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "shopware", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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

var testCredentialJSON = []byte(`{"client_id":"SWIATestClientId000000","client_secret":"test-client-secret-value"}`)

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (transport scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	return transport.fn(request)
}

// tokenIssuingTransport wraps fn so every test does not need to reimplement
// the OAuth token exchange step.
func tokenIssuingTransport(fn func(Request) (Response, error)) scriptedTransport {
	return scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method == "POST" && request.Path == "/api/oauth/token" {
			return Response{StatusCode: 200, Body: []byte(`{"access_token":"test-token","expires_in":600}`)}, nil
		}
		if request.Headers["Authorization"] != "Bearer test-token" {
			return Response{StatusCode: 401}, nil
		}
		return fn(request)
	}}
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

func TestAccessTokenIsCachedAcrossCalls(t *testing.T) {
	tokenCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method == "POST" && request.Path == "/api/oauth/token" {
			tokenCalls++
			return Response{StatusCode: 200, Body: []byte(`{"access_token":"test-token","expires_in":600}`)}, nil
		}
		if request.Headers["Authorization"] != "Bearer test-token" {
			return Response{StatusCode: 401}, nil
		}
		return Response{StatusCode: 200, Body: []byte(`{"data":[],"total":0}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	for i := 0; i < 3; i++ {
		if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{testCredentialJSON}, sdk.PageRequest{Limit: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if tokenCalls != 1 {
		t.Fatalf("token exchanged %d times, want 1", tokenCalls)
	}
}

func TestReadProductsWithVariants(t *testing.T) {
	transport := tokenIssuingTransport(func(request Request) (Response, error) {
		if request.Path == "/api/search/product" {
			var body map[string]any
			_ = json.Unmarshal(request.Body, &body)
			if filters, ok := body["filter"].([]any); ok && len(filters) == 1 {
				if entry, ok := filters[0].(map[string]any); ok {
					if entry["field"] == "parentId" && entry["value"] == nil {
						return Response{StatusCode: 200, Body: []byte(`{"data":[{"id":"p1","parentId":null,"childCount":1,"productNumber":"BOOT","name":"Boot","active":true,"updatedAt":"2026-08-12T07:00:00+00:00"}],"total":1}`)}, nil
					}
					if entry["field"] == "parentId" && entry["value"] == "p1" {
						return Response{StatusCode: 200, Body: []byte(`{"data":[{"id":"v1","parentId":"p1","productNumber":"BOOT-42","name":"Boot 42","active":true,"updatedAt":"2026-08-12T07:00:00+00:00"}],"total":1}`)}, nil
					}
				}
			}
		}
		return Response{StatusCode: 404}, nil
	})
	connector := New(transport, testConfig{}, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{testCredentialJSON}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Variants[0].RemoteID != "v1" || page.Items[0].SellerSKU != "BOOT" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestWebhookIsUnsupported(t *testing.T) {
	connector := New(scriptedTransport{}, testConfig{}, nil)
	request := sdk.CommerceWebhookRequest{Signature: "0123456789abcdef", HeaderTopic: "order.updated", ExpectedTopic: "order.updated", Body: []byte(`{"id":1}`), ReceivedAt: time.Now().UTC()}
	_, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{testCredentialJSON}, request, &memoryDedup{seen: map[string]bool{}})
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
	_, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{testCredentialJSON}, sdk.ProductWriteRequest{SellerSKU: "NEW-1", Title: "New", StatusRemoteID: "active", IdempotencyKey: "create-1"})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
}

func TestUpsertProductUpdatesInPlace(t *testing.T) {
	calls := 0
	transport := tokenIssuingTransport(func(request Request) (Response, error) {
		if request.Path == "/api/product/p1" {
			calls++
			if request.Method == "GET" {
				if calls == 1 {
					return Response{StatusCode: 200, Body: []byte(`{"data":{"id":"p1","productNumber":"BOOT","name":"Old","active":false,"description":"d"}}`)}, nil
				}
				return Response{StatusCode: 200, Body: []byte(`{"data":{"id":"p1","productNumber":"BOOT","name":"Boot","active":true,"description":"d"}}`)}, nil
			}
			if request.Method == "PATCH" {
				return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
			}
		}
		return Response{StatusCode: 404}, nil
	})
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{testCredentialJSON}, sdk.ProductWriteRequest{RemoteID: "p1", SellerSKU: "BOOT", Title: "Boot", Description: "d", StatusRemoteID: "active", IdempotencyKey: "update-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "p1" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestWriteOrderStatusCancel(t *testing.T) {
	cancelled := false
	transport := tokenIssuingTransport(func(request Request) (Response, error) {
		if request.Path == "/api/search/order" {
			state := "open"
			if cancelled {
				state = "cancelled"
			}
			return Response{StatusCode: 200, Body: []byte(`{"data":[{"id":"o1","orderNumber":"1001","stateMachineState":{"technicalName":"` + state + `"}}],"total":1}`)}, nil
		}
		if request.Path == "/api/_action/order/o1/state/cancel" {
			cancelled = true
			return Response{StatusCode: 200, Body: []byte(`{}`)}, nil
		}
		return Response{StatusCode: 404}, nil
	})
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteOrderStatus(context.Background(), testAccount(), testRuntime{testCredentialJSON}, sdk.OrderStatusWriteRequest{OrderRemoteID: "o1", StatusRemoteID: "cancelled", IdempotencyKey: "cancel-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "o1" {
		t.Fatalf("unexpected %#v", receipt)
	}
}
