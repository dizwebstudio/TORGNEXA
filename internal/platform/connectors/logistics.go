package connectors

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidLogisticsRequest = errors.New("connectors: invalid logistics request")
var logisticsRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var logisticsReturnCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var logisticsProviderCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

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

type Address struct {
	Country    string `json:"country"`
	PostalCode string `json:"postal_code,omitempty"`
	City       string `json:"city"`
	Line1      string `json:"line1"`
}

func (a Address) Validate() error { return validateAddress(a) }
func validateAddress(a Address) error {
	if len(a.Country) != 2 || a.City == "" || a.Line1 == "" || len(a.PostalCode) > 32 {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

type Parcel struct {
	WeightGrams int64 `json:"weight_grams"`
	LengthMM    int64 `json:"length_mm"`
	WidthMM     int64 `json:"width_mm"`
	HeightMM    int64 `json:"height_mm"`
}

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
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email,omitempty"`
}

type RateRequest struct {
	From    Address  `json:"from"`
	To      Address  `json:"to"`
	Parcels []Parcel `json:"parcels"`
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
	ExternalID     string           `json:"external_id"`
	ServiceCode    string           `json:"service_code"`
	IdempotencyKey string           `json:"idempotency_key"`
	From           Address          `json:"from"`
	To             Address          `json:"to"`
	Parcels        []Parcel         `json:"parcels"`
	PickupPointRef string           `json:"pickup_point_ref,omitempty"`
	Sender         LogisticsContact `json:"sender"`
	Recipient      LogisticsContact `json:"recipient"`
}
type ShipmentResult struct {
	RemoteID, Status string
	Cost             LogisticsMoney
	TrackingNumber   string
	ObservedAt       time.Time
}
type ShipmentStatusRequest struct{ RemoteID string }
type ShipmentCancelRequest struct{ RemoteID, IdempotencyKey string }
type ReturnCreateRequest struct {
	OriginalRemoteID string `json:"original_remote_id"`
	ExternalID       string `json:"external_id"`
	MailType         string `json:"mail_type"`
	// TariffCode is an optional provider-native tariff for return services
	// that require one (for example, CDEK client returns). Zero means that
	// the selected return service does not use a tariff code.
	TariffCode     int    `json:"tariff_code,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}
type LabelRequest struct{ RemoteID, Format string }
type LabelResult struct {
	ArtifactRef, MediaType string
	ObservedAt             time.Time
}

// LogisticsBatchQuery bounds a provider batch-directory read. Provider-native
// filters remain optional strings and never become canonical batch state.
type LogisticsBatchQuery struct {
	MailType     string
	MailCategory string
	Limit        int
	Page         int
}

// Validate checks the bounded batch-directory query.
func (query LogisticsBatchQuery) Validate(maxLimit int) error {
	if maxLimit < 1 || query.Limit < 1 || query.Limit > maxLimit || query.Page < 0 || query.Page > 100000 {
		return ErrInvalidLogisticsRequest
	}
	if (query.MailType != "" && !logisticsProviderCodePattern.MatchString(query.MailType)) || (query.MailCategory != "" && !logisticsProviderCodePattern.MatchString(query.MailCategory)) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsBatch is the bounded neutral projection of one provider batch.
// RemoteID and Status remain provider references/codes; no raw provider
// payload or order list crosses the connector boundary.
type LogisticsBatch struct {
	RemoteID      string
	Status        string
	ShipmentCount int
	ObservedAt    time.Time
}

// Validate checks the normalized batch projection.
func (batch LogisticsBatch) Validate() error {
	if !logisticsRefPattern.MatchString(batch.RemoteID) || !logisticsProviderCodePattern.MatchString(batch.Status) || batch.ShipmentCount < 0 || batch.ShipmentCount > 1000000 || batch.ObservedAt.IsZero() || batch.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded remote reference and provider-neutral format
// requested by a label read. Provider adapters may narrow the format further
// when their official API exposes a smaller allow-list.
func (r LabelRequest) Validate() error {
	if !logisticsRefPattern.MatchString(r.RemoteID) || !logisticsRefPattern.MatchString(r.Format) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

func (r ShipmentCreateRequest) Validate() error {
	if !logisticsRefPattern.MatchString(r.ExternalID) || !safeCodePattern.MatchString(r.ServiceCode) || !logisticsRefPattern.MatchString(r.IdempotencyKey) || r.From.Validate() != nil || r.To.Validate() != nil || len(r.Parcels) < 1 {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded inputs for a return shipment created from an
// existing remote shipment. The connector may further narrow MailType to its
// own official service-code allow-list.
func (r ReturnCreateRequest) Validate() error {
	if r.TariffCode < 0 || !logisticsRefPattern.MatchString(r.OriginalRemoteID) || !logisticsRefPattern.MatchString(r.ExternalID) || !logisticsReturnCodePattern.MatchString(r.MailType) || !logisticsRefPattern.MatchString(r.IdempotencyKey) {
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
type LogisticsBatchReader interface {
	ReadLogisticsBatches(context.Context, Account, Runtime, LogisticsBatchQuery) ([]LogisticsBatch, error)
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
