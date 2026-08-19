package onec

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

type testConfigSource struct{ value Configuration }

func (s testConfigSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return s.value, nil
}

type testTransport struct {
	catalog   []byte
	inventory []byte
	requests  []Request
	status    int
	err       error
}

func (t *testTransport) Do(_ context.Context, request Request) (Response, error) {
	t.requests = append(t.requests, request)
	if t.err != nil {
		return Response{}, t.err
	}
	status := t.status
	if status == 0 {
		status = 200
	}
	switch {
	case strings.HasSuffix(request.Path, "/$metadata"):
		return Response{StatusCode: status, Body: []byte("<edmx:Edmx>synthetic</edmx:Edmx>"), RequestID: "req-meta"}, nil
	case strings.Contains(request.Path, "/Catalog_Номенклатура"):
		return Response{StatusCode: status, Body: t.catalog, RequestID: "req-cat"}, nil
	case strings.Contains(request.Path, "/AccumulationRegister_ТоварыНаСкладах/Balance()"):
		return Response{StatusCode: status, Body: t.inventory, RequestID: "req-stock"}, nil
	default:
		return Response{StatusCode: 404, Body: []byte(`{"error":"synthetic"}`)}, nil
	}
}

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := []byte("sync_reader\nsynthetic-password-012345")
	defer clear(value)
	return callback(value)
}

func testConfiguration() Configuration {
	return Configuration{
		Host: "erp.example.test", BasePath: "/demo/odata/standard.odata",
		Catalog:   CatalogMapping{Resource: "Catalog_Номенклатура", IDField: "Ref_Key", CodeField: "Code", SKUField: "Артикул", TitleField: "Description", BrandField: "Бренд", RevisionField: "DataVersion", ArchivedField: "DeletionMark"},
		Inventory: InventoryMapping{Resource: "AccumulationRegister_ТоварыНаСкладах", Function: "Balance", ProductField: "Номенклатура_Key", LocationField: "Склад_Key", QuantityField: "КоличествоBalance"},
	}
}

func testAccount() sdk.Account {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "onec-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "onec", Family: sdk.FamilyERP, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}

//go:embed manifest.json
var manifestJSON []byte

//go:embed fixtures/catalog-page.json
var catalogFixture []byte

//go:embed fixtures/inventory-page.json
var inventoryFixture []byte

func TestManifestIsReadOnlyERP(t *testing.T) {
	manifest := Manifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	var committed sdk.Manifest
	if err := json.Unmarshal(manifestJSON, &committed); err != nil || committed.Validate() != nil || !reflect.DeepEqual(committed.Canonical(), manifest.Canonical()) {
		t.Fatal("manifest drift")
	}
	if manifest.ID != "onec" || manifest.Family != sdk.FamilyERP || !manifest.Supports("erp.catalog.read") || !manifest.Supports("erp.inventory.read") || manifest.Supports("erp.catalog.write") || manifest.Supports("erp.orders.write") {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestConfigurationRejectsUnsafeEndpointAndNames(t *testing.T) {
	cases := []Configuration{testConfiguration(), testConfiguration(), testConfiguration()}
	cases[0].Host = "127.0.0.1"
	cases[1].BasePath = "/demo/../odata/standard.odata"
	cases[2].Catalog.Resource = "Catalog_X/../../secret"
	for i, configuration := range cases {
		if err := configuration.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid", i)
		}
	}
}

func TestCatalogReadMapsConfiguredODataAndOpaqueCursor(t *testing.T) {
	transport := &testTransport{catalog: catalogFixture}
	connector := New(transport, testConfigSource{testConfiguration()}, nil)
	page, err := connector.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].RemoteID != "11111111-1111-1111-1111-111111111111" || page.Items[0].SKU != "SKU-001" || page.Items[0].Archived || !page.Items[1].Archived || page.NextCursor == "" {
		t.Fatalf("unexpected page: %#v", page)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "GET" || transport.requests[0].Host != "erp.example.test" || !strings.Contains(transport.requests[0].Path, "Catalog_Номенклатура") {
		t.Fatalf("unexpected request: %#v", transport.requests)
	}
	nextTransport := &testTransport{catalog: []byte(`{"value":[]}`)}
	nextConnector := New(nextTransport, testConfigSource{testConfiguration()}, nil)
	if _, err := nextConnector.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor}); err != nil {
		t.Fatal(err)
	}
	foundSkip := false
	for _, param := range nextTransport.requests[0].Query {
		if param.Name == "$skip" && param.Value == "2" {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Fatalf("cursor did not advance skip: %#v", nextTransport.requests[0].Query)
	}
}

func TestCursorBoundToConfiguration(t *testing.T) {
	transport := &testTransport{catalog: catalogFixture}
	connector := New(transport, testConfigSource{testConfiguration()}, nil)
	page, err := connector.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	changed := testConfiguration()
	changed.Catalog.CodeField = "Артикул"
	connector = New(&testTransport{catalog: []byte(`{"value":[]}`)}, testConfigSource{changed}, nil)
	if _, err := connector.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2, Cursor: page.NextCursor}); !errors.Is(err, sdk.ErrInvalidReadRequest) {
		t.Fatalf("want invalid cursor, got %v", err)
	}
}

func TestInventoryReadPreservesExactDecimalAndRejectsUnsafeValues(t *testing.T) {
	transport := &testTransport{inventory: inventoryFixture}
	connector := New(transport, testConfigSource{testConfiguration()}, nil)
	page, err := connector.ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Quantity != "12.500" || page.Items[1].Quantity != "3.25" || page.NextCursor == "" {
		t.Fatalf("unexpected inventory: %#v", page)
	}
	negative := []byte(`{"value":[{"Номенклатура_Key":"p1","Склад_Key":"w1","КоличествоBalance":-1.25}]}`)
	connector = New(&testTransport{inventory: negative}, testConfigSource{testConfiguration()}, nil)
	negativePage, err := connector.ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2})
	if err != nil || len(negativePage.Items) != 1 || negativePage.Items[0].Quantity != "-1.25" {
		t.Fatalf("signed ERP balance rejected: %#v %v", negativePage, err)
	}
	exponent := []byte(`{"value":[{"Номенклатура_Key":"p1","Склад_Key":"w1","КоличествоBalance":1e3}]}`)
	connector = New(&testTransport{inventory: exponent}, testConfigSource{testConfiguration()}, nil)
	if _, err := connector.ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2}); err == nil {
		t.Fatal("exponent inventory accepted")
	}
}

func TestCatalogRejectsDuplicateRemoteIDs(t *testing.T) {
	body := []byte(`{"value":[{"Ref_Key":"same","Code":"1","Description":"One","DataVersion":"v1","DeletionMark":false},{"Ref_Key":"same","Code":"2","Description":"Two","DataVersion":"v2","DeletionMark":false}]}`)
	connector := New(&testTransport{catalog: body}, testConfigSource{testConfiguration()}, nil)
	if _, err := connector.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2}); err == nil {
		t.Fatal("duplicate ids accepted")
	}
}

func TestHealthAndRemoteErrorsAreNormalized(t *testing.T) {
	connector := New(&testTransport{}, testConfigSource{testConfiguration()}, func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) })
	health, err := connector.Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	connector = New(&testTransport{status: 401}, testConfigSource{testConfiguration()}, nil)
	health, err = connector.Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "auth_rejected" {
		t.Fatalf("health=%#v err=%v", health, err)
	}
}

func TestCredentialBundleRejectsControlCharacters(t *testing.T) {
	for _, value := range [][]byte{[]byte("user\npass"), []byte("user\r\npass"), []byte("\npass"), []byte("user\n"), []byte("user\npa\x00ss")} {
		_, _, err := parseCredentialBundle(value)
		if string(value) == "user\npass" {
			if err != nil {
				t.Fatalf("valid rejected: %v", err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("invalid credential accepted: %q", value)
		}
	}
}

func TestODataEnvelopeAndMetadataFailClosed(t *testing.T) {
	connector := New(&testTransport{catalog: []byte(`{"odata.error":{"code":"x"}}`)}, testConfigSource{testConfiguration()}, nil)
	if _, err := connector.ReadERPCatalog(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2}); err == nil {
		t.Fatal("200 error envelope accepted as empty page")
	}
	if validMetadataBody([]byte("<html><body>login</body></html>")) {
		t.Fatal("HTML login page accepted as metadata")
	}
}

func TestInventoryRejectsDuplicateBalanceRows(t *testing.T) {
	body := []byte(`{"value":[{"Номенклатура_Key":"p1","Склад_Key":"w1","КоличествоBalance":"1"},{"Номенклатура_Key":"p1","Склад_Key":"w1","КоличествоBalance":"2"}]}`)
	connector := New(&testTransport{inventory: body}, testConfigSource{testConfiguration()}, nil)
	if _, err := connector.ReadERPInventory(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 2}); err == nil {
		t.Fatal("duplicate inventory balance accepted")
	}
}
