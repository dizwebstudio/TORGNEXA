package opencart

import (
	"context"
	"strings"
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

func TestOrdersReadAndStateWriteUseBridgeV1(t *testing.T) {
	state := "2"
	tr := scriptedTransport{fn: func(r Request) (Response, error) {
		route := ""
		for _, q := range r.Query {
			if q.Name == "route" {
				route = q.Value
			}
		}
		switch {
		case r.Method == "GET" && route == "extension/torgnexa/api/orders":
			return Response{StatusCode: 200, Body: []byte(`{"items":[{"id":9001,"external_id":"OC-9001","status_remote_id":"` + state + `","created_at":"2026-08-12T08:00:00Z","updated_at":"2026-08-12T08:05:00Z","items":[{"id":1,"variant_remote_id":"product:7","quantity":2}]}],"page":1,"total_pages":1}`)}, nil
		case r.Method == "GET" && route == "extension/torgnexa/api/order":
			return Response{StatusCode: 200, Body: []byte(`{"id":9001,"external_id":"OC-9001","status_remote_id":"` + state + `"}`)}, nil
		case r.Method == "PUT" && route == "extension/torgnexa/api/order-status":
			if !strings.Contains(string(r.Body), `"id":9001`) || !strings.Contains(string(r.Body), `"status_remote_id":"3"`) {
				t.Fatalf("unexpected order status body: %s", r.Body)
			}
			state = "3"
			return Response{StatusCode: 200, Body: []byte(`{"ok":true}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	c := New(tr, testConfig{}, nil)
	page, err := c.ReadOrders(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.PageRequest{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].StatusRemoteID != "2" || len(page.Items[0].Items) != 1 {
		t.Fatalf("unexpected order page: %#v, %v", page, err)
	}
	receipt, err := c.WriteOrderStatus(context.Background(), testAccount(), testRuntime{credentialJSON()}, sdk.OrderStatusWriteRequest{OrderRemoteID: "9001", StatusRemoteID: "3", IdempotencyKey: "order-status-9001"})
	if err != nil || !receipt.Applied || !receipt.Reconciled {
		t.Fatalf("unexpected order status receipt: %#v, %v", receipt, err)
	}
}

func TestOrderStatusConfigurationRequiresCompleteUniqueIDs(t *testing.T) {
	valid := Configuration{OrderStatusMapping: map[string]string{
		"pending": "1", "confirmed": "2", "processing": "3", "fulfilled": "4", "cancelled": "5",
	}}
	if _, err := valid.OrderStatuses(); err != nil {
		t.Fatalf("valid status configuration rejected: %v", err)
	}
	for name, mapping := range map[string]map[string]string{
		"missing state": {"pending": "1", "confirmed": "2", "processing": "3", "fulfilled": "4"},
		"duplicate id":  {"pending": "1", "confirmed": "1", "processing": "3", "fulfilled": "4", "cancelled": "5"},
		"zero id":       {"pending": "0", "confirmed": "2", "processing": "3", "fulfilled": "4", "cancelled": "5"},
		"non numeric":   {"pending": "1", "confirmed": "two", "processing": "3", "fulfilled": "4", "cancelled": "5"},
	} {
		if _, err := (Configuration{OrderStatusMapping: mapping}).OrderStatuses(); err == nil {
			t.Fatalf("%s configuration was accepted", name)
		}
	}
}
