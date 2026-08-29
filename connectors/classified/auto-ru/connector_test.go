package autoru

import (
	"context"
	_ "embed"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed fixtures/account.json
var accountFixture []byte

//go:embed fixtures/offers.json
var offersFixture []byte

//go:embed fixtures/feed-task.json
var feedTaskFixture []byte

//go:embed fixtures/feed-history.json
var feedHistoryFixture []byte

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{AccountID: 42, DealerID: "77"}, nil
}

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	return cb([]byte(`{"authorization":"synthetic-auto-ru-authorization-token-0123456789","session_id":"synthetic-auto-ru-session-0123456789"}`))
}

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testTransport struct {
	fn    func(Request) Response
	calls int
}

func (t *testTransport) Do(_ context.Context, r Request) (Response, error) {
	t.calls++
	if t.fn == nil {
		return Response{}, errors.New("missing test transport")
	}
	return t.fn(r), nil
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 11, 21, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "auto-ru-a", OrganizationID: "018f47a0-1234-7890-8abc-1234567890ab",
		WorkspaceID: "018f47a0-1234-7890-8abc-1234567890ac", ConnectorID: "auto-ru",
		Family: sdk.FamilyClassified, Status: sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1,
		Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	switch name {
	case "account.json":
		return accountFixture
	case "offers.json":
		return offersFixture
	case "feed-task.json":
		return feedTaskFixture
	case "feed-history.json":
		return feedHistoryFixture
	default:
		t.Fatalf("unknown fixture %q", name)
		return nil
	}
}

func TestManifestAndRisk(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if risk, err := sdk.ClassifiedCapabilityRisk("classified.publications.write"); err != nil || risk != sdk.ClassifiedRiskWriteSensitive {
		t.Fatalf("risk=%s err=%v", risk, err)
	}
	if risk, err := sdk.ClassifiedCapabilityRisk("classified.publications.status.read"); err != nil || risk != sdk.ClassifiedRiskRead {
		t.Fatalf("risk=%s err=%v", risk, err)
	}
}

func TestHealthBindsExactAccount(t *testing.T) {
	tr := &testTransport{fn: func(r Request) Response {
		if r.Method != "GET" || r.Host != apiHost || r.Path != "/1.0/dealer/account" || r.DealerID != "77" {
			t.Fatalf("request=%+v", r)
		}
		if string(r.Authorization) == "" || string(r.SessionID) == "" {
			t.Fatal("credentials not forwarded inside transport boundary")
		}
		return Response{StatusCode: 200, Body: fixture(t, "account.json")}
	}}
	c := New(tr, testConfig{}, func() time.Time { return time.Date(2026, 8, 11, 21, 30, 0, 0, time.UTC) })
	h, err := c.Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || h.Status != sdk.HealthHealthy || tr.calls != 1 {
		t.Fatalf("err=%v health=%+v calls=%d", err, h, tr.calls)
	}
}

func TestHealthRejectsForeignAccount(t *testing.T) {
	tr := &testTransport{fn: func(Request) Response {
		return Response{StatusCode: 200, Body: []byte(`{"account_id":43,"dealer_status":"ACTIVE"}`)}
	}}
	c := New(tr, testConfig{}, nil)
	h, err := c.Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || h.Status != sdk.HealthDegraded || h.ReasonCode != "remote_contract_invalid" {
		t.Fatalf("err=%v health=%+v", err, h)
	}
}

func TestReadVehicleListings(t *testing.T) {
	tr := &testTransport{fn: func(r Request) Response {
		if r.Method != "GET" || r.Path != "/1.0/user/offers/cars" || r.Query != "page=1&page_size=1" {
			t.Fatalf("request=%+v", r)
		}
		return Response{StatusCode: 200, Body: fixture(t, "offers.json")}
	}}
	c := New(tr, testConfig{}, nil)
	page, err := c.ReadClassifiedListings(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("err=%v page=%+v", err, page)
	}
	item := page.Items[0]
	if item.RemoteID != "901" || item.ExternalID != "car-001" || item.Title != "Toyota Camry 2022" || item.Price != "2450000" || item.Currency != "RUB" || item.Status != "ACTIVE" || page.NextCursor == "" {
		t.Fatalf("item=%+v next=%q", item, page.NextCursor)
	}
}

func TestPublishVehicleFeed(t *testing.T) {
	tr := &testTransport{fn: func(r Request) Response {
		if r.Method != "POST" || r.Path != "/1.0/feeds/tasks/cars/USED" || r.ContentType != "application/json" {
			t.Fatalf("request=%+v", r)
		}
		if string(r.Body) != `{"source":"https://feeds.example.test/auto-ru-used.xml"}` {
			t.Fatalf("body=%s", r.Body)
		}
		return Response{StatusCode: 200, Body: fixture(t, "feed-task.json")}
	}}
	c := New(tr, testConfig{}, nil)
	receipt, err := c.PublishClassified(context.Background(), testAccount(), testRuntime{}, sdk.ClassifiedPublicationRequest{
		Kind: sdk.ClassifiedPublicationVehicle, Section: sdk.ClassifiedPublicationUsed,
		SourceURL: "https://feeds.example.test/auto-ru-used.xml",
	})
	if err != nil || receipt.RemoteTaskID != "7001" || receipt.State != sdk.ClassifiedPublicationSubmitted {
		t.Fatalf("err=%v receipt=%+v", err, receipt)
	}
}

func TestPublishAmbiguousFailureIsNotRetryable(t *testing.T) {
	tr := &testTransport{fn: func(Request) Response { return Response{StatusCode: 503, RetryAfterMS: 5000} }}
	c := New(tr, testConfig{}, nil)
	_, err := c.PublishClassified(context.Background(), testAccount(), testRuntime{}, sdk.ClassifiedPublicationRequest{
		Kind: sdk.ClassifiedPublicationVehicle, Section: sdk.ClassifiedPublicationNew,
		SourceURL: "https://feeds.example.test/new.xml",
	})
	var re *sdk.RemoteError
	if !errors.As(err, &re) || re.Code != "write_outcome_unknown" || re.RetryAfterMS != 0 || tr.calls != 1 {
		t.Fatalf("err=%T %v calls=%d", err, err, tr.calls)
	}
}

func TestReadPublicationStatus(t *testing.T) {
	tr := &testTransport{fn: func(r Request) Response {
		if r.Method != "GET" || r.Path != "/1.0/feeds/history/7001" || r.Query != "page=1&page_size=1" {
			t.Fatalf("request=%+v", r)
		}
		return Response{StatusCode: 200, Body: fixture(t, "feed-history.json")}
	}}
	at := time.Date(2026, 8, 11, 21, 31, 0, 0, time.UTC)
	c := New(tr, testConfig{}, func() time.Time { return at })
	status, err := c.ReadClassifiedPublicationStatus(context.Background(), testAccount(), testRuntime{}, "7001")
	if err != nil || status.State != sdk.ClassifiedPublicationSucceeded || status.Total != 10 || status.Inserted != 3 || status.Updated != 4 || status.Deleted != 1 || status.Skipped != 2 || status.Errors != 1 || status.Notices != 2 || !status.CheckedAt.Equal(at) {
		t.Fatalf("err=%v status=%+v", err, status)
	}
}

func TestVehicleFeedMappingUsed(t *testing.T) {
	item := validUsedVehicle()
	feed, err := BuildVehicleFeed("USED", []VehicleFeedItem{item})
	if err != nil {
		t.Fatal(err)
	}
	text := string(feed)
	for _, want := range []string{
		`<?xml version="1.0" encoding="utf-8"?>`, `<data>`, `<cars>`, `<car>`,
		`<mark_id>Toyota</mark_id>`, `<folder_id>Camry</folder_id>`, `<modification_id>2.5_AT</modification_id>`,
		`<run>85000</run>`, `<registry_year>2020</registry_year>`, `<currency>RUR</currency>`,
		`<vin>XW7BF4FK90S123456</vin>`, `<unique_id>car-001</unique_id>`, `<action>show</action>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	var decoded struct {
		Cars []struct {
			VIN string `xml:"vin"`
		} `xml:"cars>car"`
	}
	if err := xml.Unmarshal(feed, &decoded); err != nil || len(decoded.Cars) != 1 || decoded.Cars[0].VIN != item.VIN {
		t.Fatalf("decode err=%v value=%+v", err, decoded)
	}
}

func TestVehicleFeedMappingRejectsUnsafeInput(t *testing.T) {
	cases := []VehicleFeedItem{validUsedVehicle(), validUsedVehicle(), validUsedVehicle()}
	cases[0].VIN = "INVALIDVIN"
	cases[1].Images = []string{"http://images.example.test/1.jpg"}
	cases[2].Price = 1500
	for i, item := range cases {
		if _, err := BuildVehicleFeed("USED", []VehicleFeedItem{item}); !errors.Is(err, ErrInvalidVehicle) {
			t.Fatalf("case %d err=%v", i, err)
		}
	}
}

func TestVehicleFeedMappingNewRequiresComplectation(t *testing.T) {
	item := validUsedVehicle()
	item.State, item.Run = "", 0
	item.OwnersNumber = "Не было владельцев"
	item.RegistryYear = 2020
	item.ComplectationName = ""
	if _, err := BuildVehicleFeed("NEW", []VehicleFeedItem{item}); !errors.Is(err, ErrInvalidVehicle) {
		t.Fatalf("err=%v", err)
	}
	item.ComplectationName = "Prestige"
	if _, err := BuildVehicleFeed("NEW", []VehicleFeedItem{item}); err != nil {
		t.Fatalf("valid new vehicle: %v", err)
	}
}

func validUsedVehicle() VehicleFeedItem {
	return VehicleFeedItem{
		UniqueID: "car-001", VIN: "XW7BF4FK90S123456", MarkID: "Toyota", FolderID: "Camry",
		ModificationID: "2.5_AT", BodyType: "Седан", Wheel: "Левый", Color: "Белый",
		Availability: "В наличии", Custom: "Растаможен", State: "Отличное", OwnersNumber: "Один владелец",
		Run: 85000, Year: 2020, RegistryYear: 2020, DoorsCount: 4, Currency: "RUR", Price: 2450000,
		Description: "Проверенный автомобиль.", Images: []string{"https://images.example.test/car-001-1.jpg"}, Action: "show",
	}
}
