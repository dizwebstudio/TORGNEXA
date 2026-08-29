package moysklad

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed manifest.json
var manifestJSON []byte

//go:embed fixtures/catalog-page.json
var catalogFixture []byte

//go:embed fixtures/inventory-page.json
var inventoryFixture []byte

//go:embed fixtures/orders-page.json
var ordersFixture []byte

type testTransport struct {
	catalog, inventory, orders []byte
	requests                   []Request
	status                     int
	err                        error
}

func (t *testTransport) Do(_ context.Context, r Request) (Response, error) {
	t.requests = append(t.requests, r)
	if t.err != nil {
		return Response{}, t.err
	}
	status := t.status
	if status == 0 {
		status = 200
	}
	body := []byte(`{"meta":{"size":0,"limit":1,"offset":0},"rows":[]}`)
	switch r.Path {
	case apiBasePath + "/entity/assortment":
		body = t.catalog
		if len(body) == 0 {
			body = []byte(`{"meta":{"size":0,"limit":1,"offset":0},"rows":[]}`)
		}
	case apiBasePath + "/report/stock/bystore":
		body = t.inventory
	case apiBasePath + "/entity/customerorder":
		body = t.orders
	default:
		return Response{StatusCode: 404, Body: []byte(`{"errors":[{"error":"synthetic"}]}`)}, nil
	}
	return Response{StatusCode: status, Body: body, RequestID: "req-test", RetryAfterMS: 500}, nil
}

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := []byte("synthetic-moysklad-token-0123456789")
	defer clear(value)
	return callback(value)
}
func testAccount() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "moysklad-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "moysklad", Family: sdk.FamilyERP, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}

func TestManifestReadOnlyERP(t *testing.T) {
	m := Manifest()
	if m.Validate() != nil {
		t.Fatal("manifest invalid")
	}
	var committed sdk.Manifest
	if json.Unmarshal(manifestJSON, &committed) != nil || committed.Validate() != nil || !reflect.DeepEqual(committed.Canonical(), m.Canonical()) {
		t.Fatal("manifest drift")
	}
	if !m.Supports("erp.catalog.read") || !m.Supports("erp.inventory.read") || !m.Supports("erp.orders.read") || m.Supports("erp.catalog.write") || m.Supports("erp.orders.write") {
		t.Fatalf("unexpected manifest %#v", m)
	}
	if m.RateLimit.MaxConcurrency > 4 || m.RateLimit.MinIntervalMS < 200 {
		t.Fatal("unsafe throttling")
	}
}

func TestCatalogReadPaginationAndGzip(t *testing.T) {
	tr := &testTransport{catalog: catalogFixture}
	c := New(tr, nil)
	page, err := c.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].RemoteID != "11111111-1111-1111-1111-111111111111" || page.Items[0].SKU != "SKU-001" || page.Items[1].Code != "" || !page.Items[1].Archived || page.NextCursor == "" {
		t.Fatalf("page=%#v", page)
	}
	if len(tr.requests) != 1 || !tr.requests[0].AcceptGzip || string(tr.requests[0].Token) == "" {
		t.Fatalf("request=%#v", tr.requests)
	}
	next := &testTransport{catalog: []byte(`{"meta":{"size":2,"limit":2,"offset":2},"rows":[]}`)}
	_, err = New(next, nil).ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if queryValue(next.requests[0].Query, "offset") != "2" {
		t.Fatal("cursor did not advance")
	}
}

func TestInventoryFlattensWarehousesWithBoundedCursor(t *testing.T) {
	tr := &testTransport{inventory: inventoryFixture}
	c := New(tr, nil)
	first, err := c.ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].Quantity != "12.500" || first.Items[1].LocationRemoteID != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" || first.NextCursor == "" {
		t.Fatalf("first=%#v", first)
	}
	tr2 := &testTransport{inventory: inventoryFixture}
	second, err := New(tr2, nil).ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ProductRemoteID != "22222222-2222-2222-2222-222222222222" || second.NextCursor != "" {
		t.Fatalf("second=%#v", second)
	}
}

func TestOrdersReadMapsStateStoreDeletion(t *testing.T) {
	tr := &testTransport{orders: ordersFixture}
	page, err := New(tr, nil).ReadERPOrders(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Number != "00001" || page.Items[0].StatusRemoteID != "dddddddd-dddd-dddd-dddd-dddddddddddd" || page.Items[0].LocationRemoteID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" || !page.Items[0].Applicable || !page.Items[1].Deleted || page.NextCursor == "" {
		t.Fatalf("orders=%#v", page)
	}
}

func TestFailClosedResponseAndCursorBoundaries(t *testing.T) {
	cases := [][]byte{[]byte(`{"meta":{"size":2,"limit":2,"offset":0},"rows":[{"meta":{"href":"https://evil.example/entity/product/x"},"id":"x","name":"X","updated":"v","archived":false}]}`), []byte(`{"meta":{"size":1,"limit":2,"offset":0},"rows":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/product/x"},"id":"x","name":"X","updated":"v","archived":false},{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/product/y"},"id":"y","name":"Y","updated":"v","archived":false}]}`)}
	for _, body := range cases {
		if _, err := New(&testTransport{catalog: body}, nil).ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2}); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	if _, err := New(&testTransport{catalog: catalogFixture}, nil).ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2, Cursor: "not-base64"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("cursor err=%v", err)
	}
}

func TestInventoryPreservesNegativeAndRejectsExponentDuplicate(t *testing.T) {
	negative := []byte(`{"meta":{"size":1,"limit":1,"offset":0},"rows":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/product/p1"},"stockByStore":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/store/w1"},"name":"W","stock":-30,"reserve":0,"inTransit":0}]}]}`)
	page, err := New(&testTransport{inventory: negative}, nil).ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Quantity != "-30" {
		t.Fatalf("signed stock not preserved: %#v %v", page, err)
	}
	exponent := []byte(`{"meta":{"size":1,"limit":1,"offset":0},"rows":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/product/p1"},"stockByStore":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/store/w1"},"name":"W","stock":1e3,"reserve":0,"inTransit":0}]}]}`)
	if _, err := New(&testTransport{inventory: exponent}, nil).ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 1}); err == nil {
		t.Fatal("exponent stock accepted")
	}
	dup := []byte(`{"meta":{"size":1,"limit":1,"offset":0},"rows":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/product/p1"},"stockByStore":[{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/store/w1"},"name":"W","stock":1,"reserve":0,"inTransit":0},{"meta":{"href":"https://api.moysklad.ru/api/remap/1.2/entity/store/w1"},"name":"W","stock":2,"reserve":0,"inTransit":0}]}]}`)
	if _, err := New(&testTransport{inventory: dup}, nil).ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 10}); err == nil {
		t.Fatal("duplicate stock accepted")
	}
}

func TestHealthErrorsAndCredentials(t *testing.T) {
	health, err := New(&testTransport{}, func() time.Time { return time.Date(2026, 8, 10, 17, 0, 0, 0, time.UTC) }).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	health, err = New(&testTransport{status: 401}, nil).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "auth_rejected" {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	for _, v := range [][]byte{[]byte("short"), []byte(" token-0123456789012345"), []byte("token 0123456789012345"), []byte("token\n0123456789012345")} {
		if validToken(v) {
			t.Fatalf("unsafe token accepted %q", v)
		}
	}
}

func TestRawRemoteBodyAndTransportErrorNotLeaked(t *testing.T) {
	c := New(&testTransport{status: 429, catalog: []byte(`{"token":"super-secret"}`)}, nil)
	_, err := c.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 1})
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("unsafe err %v", err)
	}
	c = New(&testTransport{err: errors.New("dial tcp api.moysklad.ru: secret")}, nil)
	_, err = c.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 1})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("transport leak %v", err)
	}
}
func queryValue(q []QueryParam, name string) string {
	for _, p := range q {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}
