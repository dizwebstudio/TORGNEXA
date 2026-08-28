package cscart

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "runtime-account", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: "cs-cart", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1,
		Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

type testRuntime struct{ secret []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets(r.secret) }

type testSecrets []byte

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), s...)
	defer clear(value)
	return callback(value)
}

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "RUB"}, nil
}

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (s scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	return s.fn(request)
}

func credentialJSON() []byte {
	return []byte(`{"email":"admin@example.com","api_key":"CS123456789012345678901234567890"}`)
}

func TestManifestAndReadProducts(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Path != "/api/2.0/products" || string(request.Username) != "admin@example.com" || string(request.Password) != "CS123456789012345678901234567890" {
			t.Fatalf("unexpected request: %+v", request)
		}
		return Response{StatusCode: 200, Body: []byte(`{"products":[{"product_id":"12","product":"100g Pants","product_code":"SKU-12","status":"A","updated_timestamp":"1720000000","full_description":"Pants"}],"params":{"total_items":"1"}}`)}, nil
	}}
	page, err := New(transport, testConfig{}, nil).ReadProducts(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "12" || page.Items[0].SellerSKU != "SKU-12" {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestUpsertProductReconcilesExisting(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		calls++
		if request.Method == "GET" && request.Path == "/api/2.0/products" {
			return Response{StatusCode: 200, Body: []byte(`{"products":[{"product_id":"12","product":"Old","product_code":"SKU-12","status":"A","updated_timestamp":"1720000000","full_description":"Old"}]}`)}, nil
		}
		if request.Method == "PUT" && request.Path == "/api/2.0/products/12" {
			if !strings.Contains(string(request.Body), `"product":"New"`) {
				t.Fatalf("body=%s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"product_id":"12"}`)}, nil
		}
		if request.Method == "GET" && request.Path == "/api/2.0/products/12" {
			return Response{StatusCode: 200, Body: []byte(`{"product_id":"12","product":"New","product_code":"SKU-12","status":"A","updated_timestamp":"1720000001","full_description":"New"}`)}, nil
		}
		return Response{StatusCode: 404}, nil
	}}
	receipt, err := New(transport, testConfig{}, nil).UpsertProduct(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.ProductWriteRequest{SellerSKU: "SKU-12", Title: "New", Description: "New", StatusRemoteID: "A", IdempotencyKey: "cs-cart-upsert-12"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "12" || calls != 3 {
		t.Fatalf("unexpected receipt=%#v calls=%d", receipt, calls)
	}
}

func TestInvalidCredentialsRejected(t *testing.T) {
	_, err := New(scriptedTransport{fn: func(Request) (Response, error) { t.Fatal("transport called"); return Response{}, nil }}, testConfig{}, nil).ReadProducts(context.Background(), testAccount(), testRuntime{[]byte(`{"email":"admin@example.com","api_key":"short"}`)}, sdk.PageRequest{Limit: 1})
	if err != ErrInvalidCredentials {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestUpsertRejectsInconsistentSKUFilter(t *testing.T) {
	puts := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method == "PUT" {
			puts++
		}
		return Response{StatusCode: 200, Body: []byte(`{"products":[{"product_id":"12","product":"Wrong","product_code":"OTHER","status":"A","updated_timestamp":"1720000000"}]}`)}, nil
	}}
	_, err := New(transport, testConfig{}, nil).UpsertProduct(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.ProductWriteRequest{SellerSKU: "SKU-12", Title: "New", StatusRemoteID: "A", IdempotencyKey: "cs-cart-filter-mismatch"})
	if err != ErrInvalidResponse {
		t.Fatalf("expected inconsistent remote response rejection, got %v", err)
	}
	if puts != 0 {
		t.Fatalf("unexpected write calls: %d", puts)
	}
}
