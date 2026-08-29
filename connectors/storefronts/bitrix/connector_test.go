package bitrix

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "bitrix-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "bitrix", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	return callback([]byte(`{"user_id":"1","webhook_code":"test-webhook-code"}`))
}

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", CatalogIblockID: 23, StoreCurrency: "RUB"}, nil
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
	for _, capability := range []sdk.Capability{"products.read", "products.write"} {
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
	} {
		if !check.ok {
			t.Fatalf("%s missing", check.name)
		}
	}
}

func TestReadProductsUsesCatalogRESTShape(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method != "POST" || request.APIMethod != "catalog.product.list" || request.Host != "shop.example.com" || string(request.UserID) != "1" || string(request.WebhookCode) != "test-webhook-code" {
			t.Fatalf("unexpected request %#v", request)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"products":[{"id":1267,"iblockId":23,"name":"Товар","active":"Y","xmlId":"SKU-1","detailText":"Описание","timestampX":"2026-08-28T09:00:00+03:00"}]}}`)}, nil
	}}
	page, err := New(transport, testConfig{}, nil).ReadProducts(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "1267" || page.Items[0].SellerSKU != "SKU-1" || page.Items[0].Title != "Товар" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestUpsertProductUpdatesAndReconciles(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod == "catalog.product.get" {
			calls++
			name, description := "Старое название", "старое описание"
			if calls > 1 {
				name, description = "Товар", "Описание"
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"product":{"id":1267,"iblockId":23,"name":"` + name + `","active":"Y","xmlId":"SKU-1","detailText":"` + description + `","timestampX":"2026-08-28T09:00:00+03:00"}}}`)}, nil
		}
		if request.APIMethod == "catalog.product.update" {
			return Response{StatusCode: 200, Body: []byte(`{"result":{"element":{"id":1267}}}`)}, nil
		}
		return Response{StatusCode: 404}, nil
	}}
	receipt, err := New(transport, testConfig{}, nil).UpsertProduct(context.Background(), testAccount(), testRuntime{}, sdk.ProductWriteRequest{RemoteID: "1267", SellerSKU: "SKU-1", Title: "Товар", Description: "Описание", StatusRemoteID: "publish", IdempotencyKey: "update-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1267" || calls != 2 {
		t.Fatalf("receipt=%#v calls=%d", receipt, calls)
	}
}

func TestUpsertProductCreateIsIdempotentByXMLID(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "catalog.product.list":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"products":[]}}`)}, nil
		case "catalog.product.add":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"element":{"id":1268}}}`)}, nil
		case "catalog.product.get":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"product":{"id":1268,"iblockId":23,"name":"Новый","active":"Y","xmlId":"SKU-2","detailText":"","timestampX":"2026-08-28T09:00:00+03:00"}}}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).UpsertProduct(context.Background(), testAccount(), testRuntime{}, sdk.ProductWriteRequest{SellerSKU: "SKU-2", Title: "Новый", StatusRemoteID: "publish", IdempotencyKey: "create-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1268" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestAPIErrorIsNormalized(t *testing.T) {
	transport := scriptedTransport{fn: func(Request) (Response, error) {
		return Response{StatusCode: 200, Body: []byte(`{"error":"ACCESS_DENIED","error_description":"secret text must not escape"}`)}, nil
	}}
	_, err := New(transport, testConfig{}, nil).ReadProducts(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
