package opencart

import (
	"context"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "oc-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "opencart", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
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
	return Configuration{StoreHost: "shop.example.com", StoreCurrency: "USD"}, nil
}
func credentialJSON() []byte { return []byte(`{"token":"oc_12345678901234567890123456789012"}`) }

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (s scriptedTransport) Do(_ context.Context, r Request) (Response, error) { return s.fn(r) }
func TestManifestAndInterfaces(t *testing.T) {
	if e := Manifest().Validate(); e != nil {
		t.Fatal(e)
	}
	var c any = New(scriptedTransport{}, testConfig{}, nil)
	checks := []struct {
		name string
		ok   bool
	}{{"ProductReader", func() bool { _, ok := c.(sdk.ProductReader); return ok }()}, {"ProductWriter", func() bool { _, ok := c.(sdk.ProductWriter); return ok }()}, {"PriceReader", func() bool { _, ok := c.(sdk.PriceReader); return ok }()}, {"PriceWriter", func() bool { _, ok := c.(sdk.PriceWriter); return ok }()}, {"InventoryReader", func() bool { _, ok := c.(sdk.InventoryReader); return ok }()}, {"InventoryWriter", func() bool { _, ok := c.(sdk.InventoryWriter); return ok }()}, {"OrderReader", func() bool { _, ok := c.(sdk.OrderReader); return ok }()}, {"OrderStatusWriter", func() bool { _, ok := c.(sdk.OrderStatusWriter); return ok }()}}
	for _, ch := range checks {
		if !ch.ok {
			t.Fatal(ch.name + " missing")
		}
	}
}

func TestProductStatusNormalization(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "publish", want: "publish"},
		{input: "draft", want: "draft"},
		{input: "private", want: "draft"},
		{input: "archived", want: "draft"},
		{input: " PRIVATE ", want: "draft"},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := normalizeProductStatus(test.input); got != test.want {
				t.Fatalf("normalizeProductStatus(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestReadProductsBridgeV1(t *testing.T) {
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		route := ""
		for _, q := range r.Query {
			if q.Name == "route" {
				route = q.Value
			}
		}
		if route != "extension/torgnexa/api/products" {
			return Response{StatusCode: 404}, nil
		}
		return Response{StatusCode: 200, Body: []byte(`{"items":[{"id":7,"sku":"BOOT-7","title":"Boot","brand":"Acme","status":"publish","price":"10.00","compare_at":"","quantity":4,"modified_at":"2026-08-12T08:00:00Z","variants":[]}],"page":1,"total_pages":1}`)}, nil
	}}
	c := New(tr, testConfig{}, nil)
	page, e := c.ReadProducts(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.PageRequest{Limit: 10})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 1 || page.Items[0].Variants[0].RemoteID != "product:7" {
		t.Fatalf("unexpected %#v", page)
	}
}
func TestProductCreateReconcilesBySKU(t *testing.T) {
	calls := 0
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		calls++
		route := ""
		for _, q := range r.Query {
			if q.Name == "route" {
				route = q.Value
			}
		}
		if route == "extension/torgnexa/api/product-by-sku" {
			if calls == 1 {
				return Response{StatusCode: 404}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`{"id":99,"sku":"BOOT-99","title":"Boot","status":"publish","modified_at":"2026-08-12T08:00:00Z"}`)}, nil
		}
		if route == "extension/torgnexa/api/product" && r.Method == "POST" {
			return Response{}, context.DeadlineExceeded
		}
		return Response{StatusCode: 500}, nil
	}}
	c := New(tr, testConfig{}, nil)
	receipt, e := c.UpsertProduct(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.ProductWriteRequest{SellerSKU: "BOOT-99", Title: "Boot", StatusRemoteID: "publish", IdempotencyKey: "pub-99"})
	if e != nil {
		t.Fatal(e)
	}
	if !receipt.Applied || !receipt.Reconciled || receipt.RemoteID != "99" {
		t.Fatalf("unexpected %#v", receipt)
	}
}
