package cian

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

//go:embed fixtures/import-state.json
var importStateFixture []byte

//go:embed fixtures/import-report.json
var importReportFixture []byte

//go:embed fixtures/import-report-errors.json
var importReportErrorsFixture []byte

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{FeedURL: "https://feeds.example.test/cian.xml"}, nil
}

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	return cb([]byte("synthetic-cian-access-key-0123456789"))
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
		return Response{}, errors.New("missing transport")
	}
	return t.fn(r), nil
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "cian-a", OrganizationID: "018f47a0-1234-7890-8abc-1234567890ab", WorkspaceID: "018f47a0-1234-7890-8abc-1234567890ac", ConnectorID: "cian", Family: sdk.FamilyClassified, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

func TestManifestStatusOnly(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if len(Manifest().Capabilities) != 1 || Manifest().Capabilities[0] != "classified.publications.status.read" {
		t.Fatalf("caps=%v", Manifest().Capabilities)
	}
	if err := sdk.RequireCapability(Manifest(), "classified.publications.write"); err == nil {
		t.Fatal("CIAN must not advertise API publication write")
	}
}

func TestHealthBindsExactFeed(t *testing.T) {
	tr := &testTransport{fn: func(r Request) Response {
		if r.Method != "GET" || r.Host != apiHost || r.Operation != OperationImportState || string(r.Authorization) != "Bearer synthetic-cian-access-key-0123456789" {
			t.Fatalf("request=%+v auth=%q", r, string(r.Authorization))
		}
		return Response{StatusCode: 200, Body: importStateFixture}
	}}
	c := New(tr, testConfig{}, func() time.Time { return time.Date(2026, 8, 11, 20, 40, 0, 0, time.UTC) })
	h, err := c.Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || h.Status != sdk.HealthHealthy || tr.calls != 1 {
		t.Fatalf("err=%v health=%+v calls=%d", err, h, tr.calls)
	}
}

func TestHealthRejectsForeignFeed(t *testing.T) {
	tr := &testTransport{fn: func(Request) Response {
		return Response{StatusCode: 200, Body: []byte(`{"feed_url":"https://foreign.example.test/cian.xml","order_id":"88001"}`)}
	}}
	c := New(tr, testConfig{}, nil)
	h, err := c.Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || h.Status != sdk.HealthDegraded || h.ReasonCode != "remote_contract_invalid" {
		t.Fatalf("err=%v health=%+v", err, h)
	}
}

func TestReadPublicationStatus(t *testing.T) {
	tr := &testTransport{fn: func(r Request) Response {
		if r.Operation != OperationImportReport {
			t.Fatalf("op=%s", r.Operation)
		}
		return Response{StatusCode: 200, Body: importReportFixture}
	}}
	at := time.Date(2026, 8, 11, 20, 45, 0, 0, time.UTC)
	c := New(tr, testConfig{}, func() time.Time { return at })
	s, err := c.ReadClassifiedPublicationStatus(context.Background(), testAccount(), testRuntime{}, "88001")
	if err != nil || s.State != sdk.ClassifiedPublicationSucceeded || s.Total != 12 || s.Inserted != 3 || s.Updated != 7 || s.Deleted != 1 || s.Skipped != 1 || s.Errors != 0 || s.Notices != 2 || !s.CheckedAt.Equal(at) {
		t.Fatalf("err=%v status=%+v", err, s)
	}
}

func TestReadPublicationStatusRejectsForeignOrder(t *testing.T) {
	tr := &testTransport{fn: func(Request) Response { return Response{StatusCode: 200, Body: importReportFixture} }}
	c := New(tr, testConfig{}, nil)
	if _, err := c.ReadClassifiedPublicationStatus(context.Background(), testAccount(), testRuntime{}, "88002"); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err=%v", err)
	}
}

func TestReadPublicationStatusMapsProblemsToFailed(t *testing.T) {
	tr := &testTransport{fn: func(Request) Response { return Response{StatusCode: 200, Body: importReportErrorsFixture} }}
	c := New(tr, testConfig{}, nil)
	s, err := c.ReadClassifiedPublicationStatus(context.Background(), testAccount(), testRuntime{}, "88002")
	if err != nil || s.State != sdk.ClassifiedPublicationFailed || s.Errors != 2 {
		t.Fatalf("err=%v status=%+v", err, s)
	}
}

func TestRateLimitNormalization(t *testing.T) {
	tr := &testTransport{fn: func(Request) Response { return Response{StatusCode: 429, RetryAfterMS: 2500} }}
	c := New(tr, testConfig{}, nil)
	_, err := c.ReadClassifiedPublicationStatus(context.Background(), testAccount(), testRuntime{}, "88001")
	var re *sdk.RemoteError
	if !errors.As(err, &re) || re.Category != sdk.ErrorRateLimited || re.RetryAfterMS != 2500 {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestBuildPropertyFeed(t *testing.T) {
	feed, err := BuildPropertyFeed([]PropertyFeedItem{validProperty()})
	if err != nil {
		t.Fatal(err)
	}
	text := string(feed)
	for _, want := range []string{`<?xml version="1.0" encoding="utf-8"?>`, `<Feed>`, `<Feed_Version>2</Feed_Version>`, `<Category>flatSale</Category>`, `<ExternalId>flat-001</ExternalId>`, `<Address>Воронеж, улица Ленина, 1</Address>`, `<FlatRoomsCount>2</FlatRoomsCount>`, `<TotalArea>57.5</TotalArea>`, `<FloorsCount>17</FloorsCount>`, `<Price>8500000</Price>`, `<Currency>rur</Currency>`, `<SaleType>free</SaleType>`, `<FullUrl>https://images.example.test/flat-001.jpg</FullUrl>`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	var decoded struct {
		Version int `xml:"Feed_Version"`
		Objects []struct {
			ExternalID string `xml:"ExternalId"`
		} `xml:"Object"`
	}
	if err := xml.Unmarshal(feed, &decoded); err != nil || decoded.Version != 2 || len(decoded.Objects) != 1 || decoded.Objects[0].ExternalID != "flat-001" {
		t.Fatalf("decode err=%v value=%+v", err, decoded)
	}
}

func TestBuildPropertyFeedRentUsesLeaseTerm(t *testing.T) {
	p := validProperty()
	p.Category = "flatRent"
	p.MortgageAllowed = false
	p.SaleType = ""
	p.LeaseTermType = "fewMonths"
	feed, err := BuildPropertyFeed([]PropertyFeedItem{p})
	if err != nil {
		t.Fatal(err)
	}
	text := string(feed)
	if !strings.Contains(text, `<LeaseTermType>fewMonths</LeaseTermType>`) || strings.Contains(text, `<SaleType>`) || strings.Contains(text, `<MortgageAllowed>`) {
		t.Fatalf("unexpected rental bargain terms: %s", text)
	}
}

func TestBuildPropertyFeedRejectsUnsafeAndInvalid(t *testing.T) {
	cases := []PropertyFeedItem{validProperty(), validProperty(), validProperty(), validProperty(), validProperty(), validProperty()}
	cases[0].Description = "коротко"
	cases[1].Photos = []PropertyPhoto{{URL: "http://images.example.test/x.jpg"}}
	cases[2].Description = "Хорошая квартира & рядом с центром города"
	cases[3].LivingArea = 50
	cases[3].KitchenArea = 10
	cases[4].Phones = []PropertyPhone{{CountryCode: "+7", Number: "123"}}
	cases[5].Category = "newBuildingFlatSale"
	for i, v := range cases {
		if _, err := BuildPropertyFeed([]PropertyFeedItem{v}); !errors.Is(err, ErrInvalidProperty) {
			t.Fatalf("case=%d err=%v", i, err)
		}
	}
}

func TestBuildPropertyFeedRejectsDuplicateExternalID(t *testing.T) {
	a, b := validProperty(), validProperty()
	if _, err := BuildPropertyFeed([]PropertyFeedItem{a, b}); !errors.Is(err, ErrInvalidProperty) {
		t.Fatalf("err=%v", err)
	}
}

func validProperty() PropertyFeedItem {
	return PropertyFeedItem{Category: "flatSale", ExternalID: "flat-001", Description: "Просторная квартира рядом с центром города.", Address: "Воронеж, улица Ленина, 1", Latitude: 51.672, Longitude: 39.184, RoomsCount: 2, TotalArea: 57.5, LivingArea: 34.0, KitchenArea: 10.5, FloorNumber: 8, FloorsCount: 17, Price: 8500000, Currency: "rur", MortgageAllowed: true, Phones: []PropertyPhone{{CountryCode: "+7", Number: "9001234567"}}, Photos: []PropertyPhoto{{URL: "https://images.example.test/flat-001.jpg", IsDefault: true}}}
}
