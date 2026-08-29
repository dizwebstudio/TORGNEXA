package aliexpressru

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

type testSecrets struct{ value []byte }

func (secrets testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secrets.value...)
	defer clear(value)
	return callback(value)
}

type testRuntime struct{ value []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{runtime.value} }

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (transport *scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	transport.requests = append(transport.requests, request)
	i := len(transport.requests) - 1
	if i < len(transport.errs) && transport.errs[i] != nil {
		return Response{}, transport.errs[i]
	}
	if i >= len(transport.responses) {
		return Response{StatusCode: 500}, nil
	}
	return transport.responses[i], nil
}

func account() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "ali-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "aliexpress-ru", Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}
func jwt() []byte         { return []byte("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzeW50aGV0aWMifQ.c2lnbmF0dXJl") }
func fixedNow() time.Time { return time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC) }

func TestManifestMatchesAndCapabilityAuditBoundary(t *testing.T) {
	var got sdk.Manifest
	if json.Unmarshal(manifestJSON, &got) != nil || got.Validate() != nil {
		t.Fatal("invalid manifest")
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatal("manifest drift")
	}
	if !Manifest().Supports("products.read") {
		t.Fatal("products.read missing")
	}
	for _, c := range []sdk.Capability{"inventory.read", "orders.read", "prices.read", "products.write", "inventory.write", "prices.write", "orders.status.write"} {
		if Manifest().Supports(c) {
			t.Fatalf("capability %s must stay deferred", c)
		}
	}
}

func TestJWTStrict(t *testing.T) {
	for _, value := range [][]byte{nil, []byte("short"), []byte("a.b.c"), []byte(" eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln "), []byte("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.*")} {
		if validJWT(value) {
			t.Fatalf("bad token accepted: %q", value)
		}
	}
	if !validJWT(jwt()) {
		t.Fatal("valid synthetic JWT rejected")
	}
}

func TestHealthAndProductsPagination(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: productsLast}, {StatusCode: 200, Body: productsPageOne}, {StatusCode: 200, Body: productsLast}}}
	connector := New(transport, fixedNow)
	health, err := connector.Health(context.Background(), account(), testRuntime{jwt()})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	page, err := connector.ReadProducts(context.Background(), account(), testRuntime{jwt()}, sdk.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if page.Items[0].RemoteID != "10001" || page.Items[0].SellerSKU != "USB-C-1" || len(page.Items[1].Variants) != 2 {
		t.Fatalf("projection=%+v", page.Items)
	}
	last, err := connector.ReadProducts(context.Background(), account(), testRuntime{jwt()}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	if !strings.Contains(string(transport.requests[2].Body), `"last_product_id":"10002"`) || !strings.Contains(string(transport.requests[2].Body), `"limit":"2"`) {
		t.Fatalf("body=%s", transport.requests[2].Body)
	}
	for _, request := range transport.requests {
		if request.Host != apiHost || request.Path != productsPath || len(request.XAuthToken) == 0 {
			t.Fatalf("request=%+v", request)
		}
	}
}

func TestFailClosedRemoteContractAndCursor(t *testing.T) {
	connector := New(&scriptedTransport{}, fixedNow)
	if _, err := connector.ReadProducts(context.Background(), account(), testRuntime{jwt()}, sdk.PageRequest{Limit: 2, Cursor: "%%%"}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("cursor=%v", err)
	}
	malformed := []byte(`{"data":[{"id":"10001","ali_updated_at":"2026-08-10T18:00:00Z","subject":"A","sku":[{"sku_id":"9001","code":"A"}]},{"id":"10001","ali_updated_at":"2026-08-10T18:01:00Z","subject":"B","sku":[{"sku_id":"9002","code":"B"}]}],"error":null}`)
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: malformed}}}
	_, err := New(transport, fixedNow).ReadProducts(context.Background(), account(), testRuntime{jwt()}, sdk.PageRequest{Limit: 2})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate product accepted: %v", err)
	}
	remoteError := []byte(`{"data":[],"error":{"code":"token_secret_should_not_escape","message":"x-auth-token=secret"}}`)
	transport = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: remoteError}}}
	_, err = New(transport, fixedNow).ReadProducts(context.Background(), account(), testRuntime{jwt()}, sdk.PageRequest{Limit: 1})
	if !errors.Is(err, ErrInvalidResponse) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("remote body leaked: %v", err)
	}
	transport = &scriptedTransport{responses: []Response{{StatusCode: 429, RequestID: "ali-request", RetryAfterMS: 1200, Body: []byte(`{"message":"x-auth-token=secret"}`)}}}
	_, err = New(transport, fixedNow).ReadProducts(context.Background(), account(), testRuntime{jwt()}, sdk.PageRequest{Limit: 1})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || strings.Contains(remote.Error(), "secret") {
		t.Fatalf("remote=%v", err)
	}
}

func TestProductParserRejectsDeprecatedStockAsAuthorityAndDuplicateVariants(t *testing.T) {
	// Deprecated stock fields may exist in remote payloads, but the products.read
	// projection intentionally ignores them; inventory.read is not admitted.
	payload := []byte(`{"data":[{"id":"10001","ali_updated_at":"2026-08-10T18:00:00Z","subject":"A","sku":[{"id":"1","sku_id":"9001","code":"A","ipm_sku_stock":"999"}]}],"error":null}`)
	products, err := parseProducts(payload, 1)
	if err != nil || len(products) != 1 {
		t.Fatalf("products=%+v err=%v", products, err)
	}
	duplicate := []byte(`{"data":[{"id":"10001","ali_updated_at":"2026-08-10T18:00:00Z","subject":"A","sku":[{"id":"1","sku_id":"9001","code":"A"},{"id":"2","sku_id":"9001","code":"B"}]}],"error":null}`)
	if _, err := parseProducts(duplicate, 1); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("duplicate variant accepted: %v", err)
	}
}

func TestInterfaces(t *testing.T) {
	var _ sdk.Connector = (*Connector)(nil)
	var _ sdk.ProductReader = (*Connector)(nil)
}
