package saleor

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "saleor-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "saleor", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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
	return Configuration{StoreHost: "shop.example.com", Channel: "default-channel", Warehouse: "main-warehouse"}, nil
}

const testToken = "testapptoken0123456789abcdef0123456789"

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (transport scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	return transport.fn(request)
}

func bodyContains(request Request, needle string) bool {
	return strings.Contains(string(request.Body), needle)
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
		if request.Path != "/graphql/" || request.Method != "POST" {
			t.Fatalf("path=%s method=%s", request.Path, request.Method)
		}
		if string(request.Bearer) != testToken {
			t.Fatalf("bearer=%s", request.Bearer)
		}
		if !bodyContains(request, "productVariants(channel: $channel") {
			t.Fatalf("unexpected query: %s", request.Body)
		}
		return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariants":{"edges":[{"cursor":"c1","node":{"id":"UHJvZHVjdFZhcmlhbnQ6MQ==","sku":"BOOT-1","updatedAt":"2026-08-12T07:00:00Z","product":{"id":"UHJvZHVjdDox","name":"Boot"},"channelListings":[]}}],"pageInfo":{"hasNextPage":false,"endCursor":"c1"}}}}`)}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "UHJvZHVjdFZhcmlhbnQ6MQ==" || page.Items[0].SellerSKU != "BOOT-1" || page.Items[0].Title != "Boot" || page.Items[0].Variants[0].RemoteID != "UHJvZHVjdFZhcmlhbnQ6MQ==" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestReadProductsPagesUsingCursor(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if calls == 1 {
			if bodyContains(request, `"after"`) {
				t.Fatalf("first call should not carry after: %s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariants":{"edges":[{"cursor":"c1","node":{"id":"A","sku":"A","updatedAt":"2026-08-12T07:00:00Z","product":{"id":"PA","name":"A"},"channelListings":[]}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`)}, nil
		}
		if !bodyContains(request, `"after":"c1"`) {
			t.Fatalf("second call missing after=c1: %s", request.Body)
		}
		return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariants":{"edges":[{"cursor":"c2","node":{"id":"B","sku":"B","updatedAt":"2026-08-12T07:00:00Z","product":{"id":"PB","name":"B"},"channelListings":[]}}],"pageInfo":{"hasNextPage":false,"endCursor":"c2"}}}}`)}, nil
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

func saleorDetachedJWS(t *testing.T, body []byte) (string, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate Saleor test key: %v", err)
	}
	protected := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"saleor-test-key","b64":false,"crit":["b64"]}`))
	signingInput := append([]byte(protected+"."), body...)
	digest := sha256.Sum256(signingInput)
	rawSignature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign Saleor webhook: %v", err)
	}
	modulus := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	jwks := []byte(`{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"saleor-test-key","n":"` + modulus + `","e":"AQAB"}]}`)
	return protected + ".." + base64.RawURLEncoding.EncodeToString(rawSignature), jwks
}

func TestWebhookVerifiesDetachedJWSAndDeduplicates(t *testing.T) {
	body := []byte(`{"event":"PRODUCT_UPDATED","data":{"object":{"id":"UHJvZHVjdDox"}}}`)
	signature, jwks := saleorDetachedJWS(t, body)
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if request.Method != "GET" || request.Host != "shop.example.com" || request.Path != "/.well-known/jwks.json" || string(request.Bearer) != testToken {
			t.Fatalf("unexpected JWKS request: %+v", request)
		}
		return Response{StatusCode: 200, Body: jwks}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receivedAt := time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)
	request := sdk.CommerceWebhookRequest{Signature: signature, HeaderTopic: "product.updated", ExpectedTopic: "product.updated", Body: body, ReceivedAt: receivedAt}
	dedup := &memoryDedup{seen: map[string]bool{}}
	first, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, request, dedup)
	if err != nil {
		t.Fatalf("first webhook: %v", err)
	}
	if first.Duplicate || first.EventType != "product.updated" || first.ResourceKind != "product" || first.ResourceRemoteID != "UHJvZHVjdDox" || string(first.CanonicalPayload) != `{"event":"product.updated","resource_id":"UHJvZHVjdDox"}` {
		t.Fatalf("unexpected first result: %+v", first)
	}
	second, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, request, dedup)
	if err != nil {
		t.Fatalf("duplicate webhook: %v", err)
	}
	if !second.Duplicate || second.DeliveryID != first.DeliveryID || calls != 2 {
		t.Fatalf("unexpected duplicate result: %+v calls=%d", second, calls)
	}
}

func TestWebhookRejectsTamperedDetachedJWS(t *testing.T) {
	body := []byte(`{"event":"PRODUCT_UPDATED","data":{"object":{"id":"UHJvZHVjdDox"}}}`)
	signature, jwks := saleorDetachedJWS(t, body)
	connector := New(scriptedTransport{fn: func(request Request) (Response, error) {
		return Response{StatusCode: 200, Body: jwks}, nil
	}}, testConfig{}, nil)
	request := sdk.CommerceWebhookRequest{Signature: signature, HeaderTopic: "product.updated", ExpectedTopic: "product.updated", Body: []byte(`{"event":"PRODUCT_UPDATED","data":{"object":{"id":"tampered"}}}`), ReceivedAt: time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)}
	_, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, request, &memoryDedup{seen: map[string]bool{}})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnauthorized || remote.Code != "webhook_signature_invalid" {
		t.Fatalf("expected unauthorized signature error, got %v", err)
	}
}

func TestWebhookPreservesJWKSRemoteError(t *testing.T) {
	body := []byte(`{"event":"PRODUCT_UPDATED","data":{"object":{"id":"UHJvZHVjdDox"}}}`)
	signature, _ := saleorDetachedJWS(t, body)
	connector := New(scriptedTransport{fn: func(Request) (Response, error) {
		return Response{StatusCode: 503, RetryAfterMS: 250}, nil
	}}, testConfig{}, nil)
	request := sdk.CommerceWebhookRequest{Signature: signature, HeaderTopic: "product.updated", ExpectedTopic: "product.updated", Body: body, ReceivedAt: time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC)}
	_, err := connector.ReceiveCommerceWebhook(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, request, &memoryDedup{seen: map[string]bool{}})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnavailable || remote.Code != "remote_unavailable" || remote.RetryAfterMS != 250 {
		t.Fatalf("expected preserved JWKS availability error, got %v", err)
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
	_, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{SellerSKU: "NEW-1", Title: "New", StatusRemoteID: "published", IdempotencyKey: "create-1"})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
}

func TestUpsertProductUpdatesInPlace(t *testing.T) {
	variantCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch {
		case bodyContains(request, "productVariant(id: $id, channel: $channel)"):
			variantCalls++
			if variantCalls == 1 {
				return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariant":{"id":"V1","sku":"OLD-1","product":{"id":"P1","name":"Old","channelListings":[{"channel":{"slug":"default-channel"},"isPublished":false}]},"channelListings":[],"stocks":[]}}}`)}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariant":{"id":"V1","sku":"BOOT-1","product":{"id":"P1","name":"Boot","channelListings":[{"channel":{"slug":"default-channel"},"isPublished":true}]},"channelListings":[],"stocks":[]}}}`)}, nil
		case bodyContains(request, "channel(slug: $slug)"):
			return Response{StatusCode: 200, Body: []byte(`{"data":{"channel":{"id":"C1","currencyCode":"USD"}}}`)}, nil
		case bodyContains(request, "productVariantUpdate(id: $id, input: {sku: $sku})"):
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariantUpdate":{"productVariant":{"id":"V1","sku":"BOOT-1"},"errors":[]}}}`)}, nil
		case bodyContains(request, "productUpdate(id: $id, input: {name: $name})"):
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productUpdate":{"product":{"id":"P1","name":"Boot"},"errors":[]}}}`)}, nil
		case bodyContains(request, "productChannelListingUpdate(id: $id"):
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productChannelListingUpdate":{"product":{"id":"P1"},"errors":[]}}}`)}, nil
		}
		t.Fatalf("unexpected query: %s", request.Body)
		return Response{StatusCode: 404}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.UpsertProduct(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.ProductWriteRequest{RemoteID: "V1", SellerSKU: "BOOT-1", Title: "Boot", StatusRemoteID: "published", IdempotencyKey: "update-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "V1" {
		t.Fatalf("unexpected %#v", receipt)
	}
	if variantCalls != 2 {
		t.Fatalf("variantCalls=%d", variantCalls)
	}
}

func TestWriteOrderStatusCancel(t *testing.T) {
	cancelled := false
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch {
		case bodyContains(request, "order(id: $id) { id status }"):
			status := "UNFULFILLED"
			if cancelled {
				status = "CANCELED"
			}
			return Response{StatusCode: 200, Body: []byte(`{"data":{"order":{"id":"O1","status":"` + status + `"}}}`)}, nil
		case bodyContains(request, "orderCancel(id: $id)"):
			cancelled = true
			return Response{StatusCode: 200, Body: []byte(`{"data":{"orderCancel":{"order":{"id":"O1","status":"CANCELED"},"errors":[]}}}`)}, nil
		}
		t.Fatalf("unexpected query: %s", request.Body)
		return Response{StatusCode: 404}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteOrderStatus(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.OrderStatusWriteRequest{OrderRemoteID: "O1", StatusRemoteID: "CANCELED", IdempotencyKey: "cancel-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "O1" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestWriteInventorySingleLocation(t *testing.T) {
	quantity := 3
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch {
		case bodyContains(request, "warehouses(filter: {slugs: $slugs}"):
			return Response{StatusCode: 200, Body: []byte(`{"data":{"warehouses":{"edges":[{"node":{"id":"W1","slug":"main-warehouse"}}]}}}`)}, nil
		case bodyContains(request, "productVariant(id: $id, channel: $channel)"):
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariant":{"id":"V1","sku":"BOOT-1","product":{"id":"P1","name":"Boot","channelListings":[]},"channelListings":[],"stocks":[{"warehouse":{"slug":"main-warehouse"},"quantity":` + strconv.Itoa(quantity) + `}]}}}`)}, nil
		case bodyContains(request, "productVariantStocksUpdate(variantId: $variantId"):
			quantity = 12
			return Response{StatusCode: 200, Body: []byte(`{"data":{"productVariantStocksUpdate":{"productVariant":{"id":"V1"},"errors":[]}}}`)}, nil
		}
		t.Fatalf("unexpected query: %s", request.Body)
		return Response{StatusCode: 404}, nil
	}}
	connector := New(transport, testConfig{}, nil)
	receipt, err := connector.WriteInventory(context.Background(), testAccount(), testRuntime{[]byte(testToken)}, sdk.InventoryWriteRequest{VariantRemoteID: "V1", Quantity: 12, IdempotencyKey: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied {
		t.Fatalf("unexpected %#v", receipt)
	}
}
