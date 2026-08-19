package logistics

import (
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

func TestRoutingNormalizesSLAAndCosts(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	q := func(cost int64, h int) sdk.RateQuote {
		return sdk.RateQuote{ServiceCode: "door", Cost: sdk.LogisticsMoney{MinorUnits: cost, Currency: "RUB"}, MinDeliveryAt: now.Add(time.Hour), MaxDeliveryAt: now.Add(time.Duration(h) * time.Hour), ObservedAt: now}
	}
	x, e := Rank([]RouteOption{{"b", q(200, 5)}, {"a", q(100, 10)}}, now)
	if e != nil || x[0].CarrierAccountID != "a" {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestServiceMappingNormalizesRemoteCode(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	q := sdk.RateQuote{ServiceCode: "remote-door", Cost: sdk.LogisticsMoney{MinorUnits: 100, Currency: "RUB"}, MinDeliveryAt: now.Add(time.Hour), MaxDeliveryAt: now.Add(2 * time.Hour), ObservedAt: now}
	x, err := MapQuote(ServiceMapping{AccountID: "acct-1", RemoteServiceCode: "remote-door", CanonicalServiceCode: "door"}, q)
	if err != nil || x.Quote.ServiceCode != "door" || x.CarrierAccountID != "acct-1" {
		t.Fatalf("%+v %v", x, err)
	}
}
