package connectors

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidLogisticsRequest = errors.New("connectors: invalid logistics request")
var logisticsRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

type LogisticsMoney struct {
	MinorUnits int64
	Currency   string
}

func (m LogisticsMoney) Validate() error {
	if m.MinorUnits < 0 || len(m.Currency) != 3 {
		return ErrInvalidLogisticsRequest
	}
	for _, c := range []byte(m.Currency) {
		if c < 'A' || c > 'Z' {
			return ErrInvalidLogisticsRequest
		}
	}
	return nil
}

type Address struct{ Country, PostalCode, City, Line1 string }

func (a Address) Validate() error { return validateAddress(a) }
func validateAddress(a Address) error {
	if len(a.Country) != 2 || a.City == "" || a.Line1 == "" || len(a.PostalCode) > 32 {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

type Parcel struct{ WeightGrams, LengthMM, WidthMM, HeightMM int64 }

func (p Parcel) Validate() error {
	if p.WeightGrams <= 0 || p.LengthMM <= 0 || p.WidthMM <= 0 || p.HeightMM <= 0 {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsContact carries the minimum recipient/sender contact details
// needed by a carrier shipment request. It is request-scoped and is never
// part of the canonical shipment projection.
type LogisticsContact struct {
	Name  string
	Phone string
	Email string
}

type RateRequest struct {
	From, To Address
	Parcels  []Parcel
}
type RateQuote struct {
	ServiceCode                              string
	Cost                                     LogisticsMoney
	MinDeliveryAt, MaxDeliveryAt, ObservedAt time.Time
}

func (r RateRequest) Validate() error {
	if r.From.Validate() != nil || r.To.Validate() != nil || len(r.Parcels) < 1 || len(r.Parcels) > 50 {
		return ErrInvalidLogisticsRequest
	}
	for _, p := range r.Parcels {
		if p.Validate() != nil {
			return ErrInvalidLogisticsRequest
		}
	}
	return nil
}
func (q RateQuote) Validate() error {
	if !safeCodePattern.MatchString(q.ServiceCode) || q.Cost.Validate() != nil || q.MinDeliveryAt.IsZero() || q.MaxDeliveryAt.Before(q.MinDeliveryAt) || q.ObservedAt.IsZero() {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

type ShipmentCreateRequest struct {
	ExternalID, ServiceCode, IdempotencyKey string
	From, To                                Address
	Parcels                                 []Parcel
	PickupPointRef                          string
	Sender, Recipient                       LogisticsContact
}
type ShipmentResult struct {
	RemoteID, Status string
	Cost             LogisticsMoney
	TrackingNumber   string
	ObservedAt       time.Time
}
type ShipmentStatusRequest struct{ RemoteID string }
type ShipmentCancelRequest struct{ RemoteID, IdempotencyKey string }
type ReturnCreateRequest struct{ OriginalRemoteID, ExternalID, IdempotencyKey string }
type LabelRequest struct{ RemoteID, Format string }
type LabelResult struct {
	ArtifactRef, MediaType string
	ObservedAt             time.Time
}

func (r ShipmentCreateRequest) Validate() error {
	if !logisticsRefPattern.MatchString(r.ExternalID) || !safeCodePattern.MatchString(r.ServiceCode) || !logisticsRefPattern.MatchString(r.IdempotencyKey) || r.From.Validate() != nil || r.To.Validate() != nil || len(r.Parcels) < 1 {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

type LogisticsRateReader interface {
	ReadLogisticsRates(context.Context, Account, Runtime, RateRequest) ([]RateQuote, error)
}
type LogisticsShipmentCreator interface {
	CreateLogisticsShipment(context.Context, Account, Runtime, ShipmentCreateRequest) (ShipmentResult, error)
}
type LogisticsTracker interface {
	ReadLogisticsTracking(context.Context, Account, Runtime, ShipmentStatusRequest) (ShipmentResult, error)
}
type LogisticsShipmentCanceler interface {
	CancelLogisticsShipment(context.Context, Account, Runtime, ShipmentCancelRequest) (ShipmentResult, error)
}
type LogisticsReturnCreator interface {
	CreateLogisticsReturn(context.Context, Account, Runtime, ReturnCreateRequest) (ShipmentResult, error)
}
type LogisticsLabelReader interface {
	ReadLogisticsLabel(context.Context, Account, Runtime, LabelRequest) (LabelResult, error)
}

type LogisticsWebhook struct {
	DeliveryID string
	RemoteID   string
	Status     string
	OccurredAt time.Time
}

func (w LogisticsWebhook) Validate() error {
	if !logisticsRefPattern.MatchString(w.DeliveryID) || !logisticsRefPattern.MatchString(w.RemoteID) || !safeCodePattern.MatchString(w.Status) || w.OccurredAt.IsZero() || w.OccurredAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

type LogisticsWebhookVerifier interface {
	VerifyLogisticsWebhook(context.Context, Account, Runtime, []byte, []byte) (LogisticsWebhook, error)
}
