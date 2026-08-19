// Package logistics normalizes carrier costs and SLA for provider-neutral routing.
package logistics

import (
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"sort"
	"time"
)

var ErrInvalid = errors.New("logistics: invalid value")

// ServiceMapping maps one account-local remote tariff/service code into a
// canonical routing service without leaking provider IDs into fulfillment.
type ServiceMapping struct {
	AccountID, RemoteServiceCode, CanonicalServiceCode string
}

func (m ServiceMapping) Validate() error {
	if m.AccountID == "" || m.RemoteServiceCode == "" || m.CanonicalServiceCode == "" {
		return ErrInvalid
	}
	return nil
}

func MapQuote(m ServiceMapping, q sdk.RateQuote) (RouteOption, error) {
	if m.Validate() != nil || q.Validate() != nil || q.ServiceCode != m.RemoteServiceCode {
		return RouteOption{}, ErrInvalid
	}
	q.ServiceCode = m.CanonicalServiceCode
	return RouteOption{CarrierAccountID: m.AccountID, Quote: q}, nil
}

type RouteOption struct {
	CarrierAccountID string
	Quote            sdk.RateQuote
}

func Rank(options []RouteOption, now time.Time) ([]RouteOption, error) {
	out := append([]RouteOption(nil), options...)
	for _, o := range out {
		if o.CarrierAccountID == "" || o.Quote.Validate() != nil || o.Quote.MaxDeliveryAt.Before(now) {
			return nil, ErrInvalid
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Quote, out[j].Quote
		if a.Cost.Currency == b.Cost.Currency && a.Cost.MinorUnits != b.Cost.MinorUnits {
			return a.Cost.MinorUnits < b.Cost.MinorUnits
		}
		if !a.MaxDeliveryAt.Equal(b.MaxDeliveryAt) {
			return a.MaxDeliveryAt.Before(b.MaxDeliveryAt)
		}
		return out[i].CarrierAccountID < out[j].CarrierAccountID
	})
	return out, nil
}
