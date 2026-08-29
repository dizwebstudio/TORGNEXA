package cbrfx

import (
	"context"
	_ "embed"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed fixtures/daily.xml
var testDailyXML []byte

type fixtureTransport struct{ body []byte }

func (f fixtureTransport) Daily(context.Context, time.Time) ([]byte, error) {
	return append([]byte(nil), f.body...), nil
}
func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "cbr-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "cbr-fx", Family: sdk.FamilyFX, Status: sdk.AccountActive, Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestHistoricalOfficialRate(t *testing.T) {
	c := New(fixtureTransport{testDailyXML}, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	obs, err := c.ReadFXRate(context.Background(), testAccount(), nil, sdk.FXRateRequest{BaseCurrency: "USD", QuoteCurrency: "RUB", AsOf: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), RateType: "official"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rate != "80.5" || obs.Source != "cbr" {
		t.Fatalf("obs=%+v", obs)
	}
}
func TestNominalIsConvertedExactly(t *testing.T) {
	d, err := unitRate("54,1200", "100")
	if err != nil {
		t.Fatal(err)
	}
	if d != "0.5412" {
		t.Fatalf("rate=%s", d)
	}
}
func TestNoImplicitInverse(t *testing.T) {
	c := New(fixtureTransport{testDailyXML}, nil)
	_, err := c.ReadFXRate(context.Background(), testAccount(), nil, sdk.FXRateRequest{BaseCurrency: "RUB", QuoteCurrency: "USD", AsOf: time.Now().UTC(), RateType: "official"})
	if err == nil {
		t.Fatal("implicit inverse accepted")
	}
	var remote *sdk.RemoteError
	if !errorsAs(err, &remote) || remote.Category != sdk.ErrorUnsupported {
		t.Fatalf("err=%v", err)
	}
}
func errorsAs(err error, target **sdk.RemoteError) bool {
	r, ok := err.(*sdk.RemoteError)
	if ok {
		*target = r
	}
	return ok
}
