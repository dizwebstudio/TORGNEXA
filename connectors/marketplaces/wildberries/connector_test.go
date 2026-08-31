package wildberries

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

//go:embed fixtures/cards-page-1.json
var cardsPageOne []byte

//go:embed fixtures/cards-page-full.json
var cardsPageFull []byte

//go:embed fixtures/warehouses.json
var warehousesFixture []byte

//go:embed fixtures/stocks.json
var stocksFixture []byte

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
	tokens    []string
}

func (transport *scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	transport.requests = append(transport.requests, request)
	transport.tokens = append(transport.tokens, string(request.Token))
	index := len(transport.requests) - 1
	if index < len(transport.errs) && transport.errs[index] != nil {
		return Response{}, transport.errs[index]
	}
	if index >= len(transport.responses) {
		return Response{}, errors.New("unexpected call")
	}
	return transport.responses[index], nil
}

type testRuntime struct{ secret []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{value: runtime.secret} }

type testSecrets struct{ value []byte }

func (secrets testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secrets.value...)
	defer clear(value)
	return callback(value)
}

func testAccount() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "wb-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "wildberries", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}

func TestManifestMatchesCommittedJSON(t *testing.T) {
	var fromFile sdk.Manifest
	if err := json.Unmarshal(manifestJSON, &fromFile); err != nil {
		t.Fatal(err)
	}
	if err := fromFile.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromFile.Canonical(), Manifest().Canonical()) {
		t.Fatalf("manifest drift: %#v != %#v", fromFile, Manifest())
	}
	if !Manifest().Supports("products.write") || Manifest().Supports("inventory.write") {
		t.Fatal("marketplace product publication capability is not declared exactly")
	}
}

func TestHealthUsesPingAndNormalizesAuthFailure(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200}, {StatusCode: 200}, {StatusCode: 401, RequestID: "req-1"}}}
	connector := New(transport, func() time.Time { return now })
	health, err := connector.Health(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	health, err = connector.Health(context.Background(), testAccount(), testRuntime{secret: []byte("bad")})
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "auth_rejected" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	if got := transport.requests[0]; got.Host != contentHost || got.Path != "/ping" || got.Method != "GET" {
		t.Fatalf("unexpected ping request: %+v", got)
	}
}

func TestReadProductsMapsRemoteIDsAndCursor(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: cardsPageFull}}}
	connector := New(transport, nil)
	page, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "505548518" || page.Items[0].Variants[0].RemoteID != "701710869" || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	var body map[string]any
	if err := json.Unmarshal(transport.requests[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	settings := body["settings"].(map[string]any)
	cursor := settings["cursor"].(map[string]any)
	if cursor["limit"] != float64(1) {
		t.Fatalf("missing bounded limit: %s", transport.requests[0].Body)
	}
	transport.responses = append(transport.responses, Response{StatusCode: 200, Body: cardsPageOne})
	_, err = connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	var second map[string]any
	_ = json.Unmarshal(transport.requests[1].Body, &second)
	secondCursor := second["settings"].(map[string]any)["cursor"].(map[string]any)
	if secondCursor["nmID"] != float64(505548518) || secondCursor["updatedAt"] != "2026-08-10T12:00:00Z" {
		t.Fatalf("cursor not replayed: %v", secondCursor)
	}
}

func TestInventoryReadsWarehousesAndChrtIDs(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: warehousesFixture}, {StatusCode: 200, Body: stocksFixture}}}
	connector := New(transport, nil)
	locations, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	if err != nil || len(locations) != 2 || locations[0].RemoteID != "12345" {
		t.Fatalf("locations=%+v err=%v", locations, err)
	}
	items, err := connector.ReadInventory(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.InventoryQuery{LocationRemoteID: "12345", VariantRemoteIDs: []string{"701710869", "701710870"}})
	if err != nil || len(items) != 2 || items[0].Quantity != 17 || items[1].Quantity != 0 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if transport.requests[1].Path != "/api/v3/stocks/12345" {
		t.Fatalf("path=%s", transport.requests[1].Path)
	}
	var request stocksRequest
	if err := json.Unmarshal(transport.requests[1].Body, &request); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.ChrtIDs, []int64{701710869, 701710870}) {
		t.Fatalf("chrtIds=%v", request.ChrtIDs)
	}
}

func TestRemoteErrorsAreBounded(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 429, RequestID: "req-rate", RetryAfterMS: 1500, Body: []byte(`{"error":"raw secret must not escape"}`)}}}
	connector := New(transport, nil)
	_, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "rate_limited" || remote.RetryAfterMS != 1500 || remote.RemoteRequestID != "req-rate" {
		t.Fatalf("err=%#v", err)
	}
	if remote.Error() == string(transport.responses[0].Body) {
		t.Fatal("raw response leaked")
	}
}

func TestProductAndInventoryInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
	var _ sdk.InventoryReader = (*Connector)(nil)
}

func TestReadProductsRejectsMalformedCursorAndResponse(t *testing.T) {
	connector := New(&scriptedTransport{}, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100, Cursor: "%%%"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("malformed cursor error=%v", err)
	}
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"cards":[],"cursor":{"updatedAt":"","nmID":0,"total":-1}}`)}}}
	connector = New(transport, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("invalid response error=%v", err)
	}
	transport = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"cards":[],"cursor":{"updatedAt":"","nmID":0,"total":1}}`)}}}
	connector = New(transport, nil)
	if _, err := connector.ReadProducts(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.PageRequest{Limit: 100}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("cursor total/card count mismatch error=%v", err)
	}
}

func TestInventoryRejectsDuplicateWarehouseAndUnsafeStocks(t *testing.T) {
	duplicateWarehouses := []byte(`[{"id":12345,"name":"A"},{"id":12345,"name":"B"}]`)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: duplicateWarehouses}}}
	connector := New(transport, nil)
	if _, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate warehouse error=%v", err)
	}

	cases := [][]byte{
		[]byte(`{"stocks":[{"chrtId":701710869,"amount":-1}]}`),
		[]byte(`{"stocks":[{"chrtId":999999999,"amount":1}]}`),
		[]byte(`{"stocks":[{"chrtId":701710869,"amount":1},{"chrtId":701710869,"amount":1}]}`),
	}
	for _, body := range cases {
		transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: body}}}
		connector := New(transport, nil)
		_, err := connector.ReadInventory(context.Background(), testAccount(), testRuntime{secret: []byte("token")}, sdk.InventoryQuery{LocationRemoteID: "12345", VariantRemoteIDs: []string{"701710869", "701710870"}})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("unsafe stock body accepted: %s err=%v", body, err)
		}
	}
}

func TestOversizedAndTransportFailuresNormalize(t *testing.T) {
	oversized := make([]byte, maxBodyBytes+1)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: oversized}}}
	connector := New(transport, nil)
	_, err := connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorInternal || remote.Code != "response_invalid" {
		t.Fatalf("oversized response err=%#v", err)
	}

	transport = &scriptedTransport{errs: []error{errors.New("dial tcp token=must-not-leak")}, responses: []Response{{}}}
	connector = New(transport, nil)
	_, err = connector.ListInventoryLocations(context.Background(), testAccount(), testRuntime{secret: []byte("token")})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorUnavailable || remote.Code != "transport_unavailable" || strings.Contains(remote.Error(), "must-not-leak") {
		t.Fatalf("transport error not normalized: %#v", err)
	}
}
