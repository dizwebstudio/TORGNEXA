package megamarket

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

//go:embed fixtures/products-page-1.json
var productsPageOne []byte

//go:embed fixtures/products-last.json
var productsLast []byte

//go:embed fixtures/stock-red.json
var stockRed []byte

//go:embed fixtures/stock-blue.json
var stockBlue []byte

//go:embed fixtures/orders.json
var ordersFixture []byte

type staticConfig struct{ value Configuration }

func (s staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return s.value, nil
}

type testSecrets struct{ value []byte }

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s.value...)
	defer clear(v)
	return cb(v)
}

type testRuntime struct{ value []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{r.value} }

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (s *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	s.requests = append(s.requests, r)
	i := len(s.requests) - 1
	if i < len(s.errs) && s.errs[i] != nil {
		return Response{}, s.errs[i]
	}
	if i >= len(s.responses) {
		return Response{StatusCode: 500}, nil
	}
	return s.responses[i], nil
}
func config() Configuration {
	return Configuration{MerchantID: 10001, Scheme: SchemeDBS, Warehouses: []Warehouse{{ID: "WH-01", Name: "Main"}, {ID: "WH-02", Name: "Reserve"}}}
}
func account() sdk.Account {
	t := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "mm-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "megamarket", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: t, UpdatedAt: t}
}
func token() []byte { return []byte("synthetic-megamarket-token-0123456789abcdef") }

func TestManifestMatchesAndReadOnly(t *testing.T) {
	var got sdk.Manifest
	if json.Unmarshal(manifestJSON, &got) != nil || got.Validate() != nil {
		t.Fatal("invalid manifest")
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatal("manifest drift")
	}
	for _, c := range []sdk.Capability{"products.write", "prices.write", "inventory.write", "orders.status.write"} {
		if Manifest().Supports(c) {
			t.Fatalf("write capability %s", c)
		}
	}
}
func TestConfigurationAndTokenStrict(t *testing.T) {
	if config().Validate() != nil {
		t.Fatal("valid config rejected")
	}
	bad := config()
	bad.MerchantID = 0
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("merchant accepted")
	}
	bad = config()
	bad.Warehouses = append(bad.Warehouses, bad.Warehouses[0])
	if !errors.Is(bad.Validate(), ErrInvalidConfiguration) {
		t.Fatal("duplicate warehouse accepted")
	}
	for _, v := range [][]byte{nil, []byte("short"), []byte(" token with space "), []byte("abc\ndefghijklmnop")} {
		if validToken(v) {
			t.Fatalf("bad token %q", v)
		}
	}
	if !validToken(token()) {
		t.Fatal("good token rejected")
	}
}
func TestHealthAndProducts(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"success":true,"data":{"items":[],"searchAfter":""}}`)}, {StatusCode: 200, Body: productsPageOne}, {StatusCode: 200, Body: productsLast}}}
	c := New(tr, staticConfig{config()}, func() time.Time { return time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC) })
	h, e := c.Health(context.Background(), account(), testRuntime{token()})
	if e != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", h, e)
	}
	p, e := c.ReadProducts(context.Background(), account(), testRuntime{token()}, sdk.PageRequest{Limit: 2})
	if e != nil || len(p.Items) != 2 || p.Items[0].RemoteID != "900001" || p.Items[0].SellerSKU != "MM-RED-M" || p.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", p, e)
	}
	p2, e := c.ReadProducts(context.Background(), account(), testRuntime{token()}, sdk.PageRequest{Limit: 2, Cursor: p.NextCursor})
	if e != nil || len(p2.Items) != 1 || p2.NextCursor != "" {
		t.Fatalf("page2=%+v err=%v", p2, e)
	}
	if tr.requests[0].Path != "/api/merchantIntegration/assortment/v1/card/getAttributes" || tr.requests[1].Host != apiHost || len(tr.requests[1].MerchantToken) == 0 {
		t.Fatalf("request=%+v", tr.requests[1])
	}
}
func TestInventoryUsesConfiguredWarehouseAndOfferID(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: stockRed}, {StatusCode: 200, Body: stockBlue}}}
	c := New(tr, staticConfig{config()}, nil)
	loc, e := c.ListInventoryLocations(context.Background(), account(), testRuntime{token()})
	if e != nil || len(loc) != 2 || loc[0].RemoteID != "WH-01" {
		t.Fatalf("loc=%+v err=%v", loc, e)
	}
	s, e := c.ReadInventory(context.Background(), account(), testRuntime{token()}, sdk.InventoryQuery{LocationRemoteID: "WH-01", VariantRemoteIDs: []string{"MM-RED-M", "MM-BLUE-L"}})
	if e != nil || len(s) != 2 || s[0].Quantity != 7 || s[1].Quantity != 3 {
		t.Fatalf("stock=%+v err=%v", s, e)
	}
	if tr.requests[0].Path != "/api/merchantIntegration/assortment/v1/stock/getByOfferId" {
		t.Fatalf("path=%s", tr.requests[0].Path)
	}
}
func TestOrdersAreBoundedAndExcludePII(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: ordersFixture}}}
	p, e := New(tr, staticConfig{config()}, nil).ReadOrders(context.Background(), account(), testRuntime{token()}, sdk.PageRequest{Limit: 1})
	if e != nil || len(p.Items) != 1 || p.Items[0].RemoteID != "SHP-1001" || len(p.Items[0].Items) != 2 || p.NextCursor == "" {
		t.Fatalf("orders=%+v err=%v", p, e)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "SHOULD_NOT_BE_PARSED") {
		t.Fatal("PII leaked")
	}
	if tr.requests[0].Path != "/api/market/v1/orderService/order/search" {
		t.Fatalf("path=%s", tr.requests[0].Path)
	}
}
func TestFailClosedRemoteContractAndCursor(t *testing.T) {
	c := New(&scriptedTransport{}, staticConfig{config()}, nil)
	if _, e := c.ReadProducts(context.Background(), account(), testRuntime{token()}, sdk.PageRequest{Limit: 2, Cursor: "%%%"}); !errors.Is(e, sdk.ErrInvalidReadRequest) {
		t.Fatalf("cursor=%v", e)
	}
	tr := &scriptedTransport{responses: []Response{{StatusCode: 429, RequestID: "mm-req", RetryAfterMS: 1000, Body: []byte(`{"message":"token=secret"}`)}}}
	_, e := New(tr, staticConfig{config()}, nil).ReadProducts(context.Background(), account(), testRuntime{token()}, sdk.PageRequest{Limit: 1})
	var re *sdk.RemoteError
	if !errors.As(e, &re) || re.Category != sdk.ErrorRateLimited || strings.Contains(re.Error(), "secret") {
		t.Fatalf("remote=%v", e)
	}
	tr = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"success":true,"data":{"items":[{"goodsId":"1","offerId":"A","name":"A","updatedAt":"bad"}],"searchAfter":""}}`)}}}
	_, e = New(tr, staticConfig{config()}, nil).ReadProducts(context.Background(), account(), testRuntime{token()}, sdk.PageRequest{Limit: 1})
	if !errors.Is(e, ErrInvalidResponse) {
		t.Fatalf("malformed=%v", e)
	}
}
func TestInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
	var _ sdk.OrderReader = (*Connector)(nil)
}
