package woocommerce

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "woo-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "woocommerce", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type testRuntime struct{ secret []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets(runtime.secret) }

type testSecrets []byte

func (secret testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	value := append([]byte(nil), secret...)
	defer clear(value)
	return cb(value)
}

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "USD"}, nil
}
func credentialJSON() []byte {
	return []byte(`{"consumer_key":"ck_12345678901234567890123456789012","consumer_secret":"cs_12345678901234567890123456789012","webhook_secret":"whsec_123456789012345678901234567890123456"}`)
}

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
	if _, ok := connector.(sdk.ProductReader); !ok {
		t.Fatal("ProductReader missing")
	}
	if _, ok := connector.(sdk.ProductWriter); !ok {
		t.Fatal("ProductWriter missing")
	}
	if _, ok := connector.(sdk.PriceReader); !ok {
		t.Fatal("PriceReader missing")
	}
	if _, ok := connector.(sdk.PriceWriter); !ok {
		t.Fatal("PriceWriter missing")
	}
	if _, ok := connector.(sdk.InventoryReader); !ok {
		t.Fatal("InventoryReader missing")
	}
	if _, ok := connector.(sdk.InventoryWriter); !ok {
		t.Fatal("InventoryWriter missing")
	}
	if _, ok := connector.(sdk.OrderReader); !ok {
		t.Fatal("OrderReader missing")
	}
	if _, ok := connector.(sdk.OrderStatusWriter); !ok {
		t.Fatal("OrderStatusWriter missing")
	}
	if _, ok := connector.(sdk.ReturnReader); !ok {
		t.Fatal("ReturnReader missing")
	}
	if _, ok := connector.(sdk.CommerceWebhookReceiver); !ok {
		t.Fatal("CommerceWebhookReceiver missing")
	}
}

func TestReadProductsSimple(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Path != "/wp-json/wc/v3/products" {
			t.Fatalf("path=%s", request.Path)
		}
		return Response{StatusCode: 200, TotalPages: 1, Body: []byte(`[{"id":7,"name":"Boot","sku":"BOOT-7","type":"simple","date_modified_gmt":"2026-08-12T07:00:00","variations":[],"brands":[{"name":"Acme"}]}]`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Variants[0].RemoteID != "product:7" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestWebhookSignatureAndReplay(t *testing.T) {
	secret := "whsec_123456789012345678901234567890123456"
	body := []byte(`{"id":41,"date_modified_gmt":"2026-08-12T07:30:00"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	connector := New(scriptedTransport{}, testConfig{}, nil)
	dedup := &memoryDedup{seen: map[string]bool{}}
	request := sdk.CommerceWebhookRequest{Signature: signature, HeaderTopic: "order.updated", ExpectedTopic: "order.updated", Body: body, ReceivedAt: time.Date(2026, 8, 12, 7, 31, 0, 0, time.UTC)}
	first, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{credentialJSON()}, request, dedup)
	if err != nil {
		t.Fatal(err)
	}
	second, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{credentialJSON()}, request, dedup)
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate || !second.Duplicate || first.DeliveryID != second.DeliveryID {
		t.Fatalf("replay failed: %#v %#v", first, second)
	}
	if string(first.CanonicalPayload) != `{"id":41,"date_modified_gmt":"2026-08-12T07:30:00"}` {
		t.Fatalf("webhook payload was not minimized: %s", first.CanonicalPayload)
	}
	request.HeaderTopic = "product.updated"
	if _, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{credentialJSON()}, request, dedup); err == nil {
		t.Fatal("header topic mismatch accepted")
	}
}

type memoryDedup struct{ seen map[string]bool }

func (d *memoryDedup) ClaimCommerceWebhook(_ context.Context, _ sdk.Account, claim sdk.CommerceWebhookClaim) (bool, error) {
	duplicate := d.seen[claim.DeliveryID]
	d.seen[claim.DeliveryID] = true
	return duplicate, nil
}

func TestCreateProductReconcilesBySKU(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if request.Method == "GET" {
			if calls == 1 {
				return Response{StatusCode: 200, Body: []byte(`[]`)}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`[{"id":99,"name":"Boot","sku":"BOOT-99","description":"","status":"publish"}]`)}, nil
		}
		if request.Method == "POST" {
			return Response{}, context.DeadlineExceeded
		}
		return Response{StatusCode: 500}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.ProductWriteRequest{SellerSKU: "BOOT-99", Title: "Boot", StatusRemoteID: "publish", IdempotencyKey: "pub-99"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || !receipt.Reconciled || receipt.RemoteID != "99" {
		t.Fatalf("unexpected %#v", receipt)
	}
}
