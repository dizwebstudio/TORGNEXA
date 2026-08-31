package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidLogisticsRequest = errors.New("connectors: invalid logistics request")
var logisticsRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var logisticsReturnCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var logisticsProviderCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var logisticsBatchDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

type LogisticsMoney struct {
	MinorUnits int64  `json:"minor_units"`
	Currency   string `json:"currency"`
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

// ShipmentItem is one declared product line in a shipment. It is optional in
// the neutral request because most carriers accept package-only data; a
// carrier that requires customs/payment product lines must explicitly require
// and validate it in its adapter.
type ShipmentItem struct {
	Name       string         `json:"name"`
	Quantity   int64          `json:"quantity"`
	UnitPrice  LogisticsMoney `json:"unit_price"`
	VATPercent int            `json:"vat_percent"`
	SKU        string         `json:"sku,omitempty"`
	Barcode    string         `json:"barcode,omitempty"`
}

// Validate checks the provider-neutral product-line bounds. Provider adapters
// may impose stricter catalog, tax or identifier rules.
func (item ShipmentItem) Validate() error {
	if !validLogisticsText(item.Name, 512) || item.Quantity <= 0 || item.UnitPrice.MinorUnits <= 0 || item.UnitPrice.Validate() != nil {
		return ErrInvalidLogisticsRequest
	}
	switch item.VATPercent {
	case -1, 0, 5, 7, 10, 22:
	default:
		return ErrInvalidLogisticsRequest
	}
	if !validLogisticsOptionalText(item.SKU, 128) || !validLogisticsOptionalText(item.Barcode, 128) {
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
	From          Address        `json:"from"`
	To            Address        `json:"to"`
	Parcels       []Parcel       `json:"parcels"`
	FromPointRef  string         `json:"from_point_ref,omitempty"`
	ToPointRef    string         `json:"to_point_ref,omitempty"`
	CalculateDate string         `json:"calculate_date,omitempty"`
	DeclaredValue LogisticsMoney `json:"declared_value,omitempty"`
	PaymentValue  LogisticsMoney `json:"payment_value,omitempty"`
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
	if !validLogisticsOptionalText(r.FromPointRef, 192) || !validLogisticsOptionalText(r.ToPointRef, 192) || !validLogisticsOptionalText(r.CalculateDate, 10) {
		return ErrInvalidLogisticsRequest
	}
	if r.CalculateDate != "" && !logisticsBatchDatePattern.MatchString(r.CalculateDate) {
		return ErrInvalidLogisticsRequest
	}
	if r.CalculateDate != "" {
		if _, err := time.Parse("2006-01-02", r.CalculateDate); err != nil {
			return ErrInvalidLogisticsRequest
		}
	}
	if (r.DeclaredValue.Currency == "" && r.DeclaredValue.MinorUnits != 0) || (r.DeclaredValue.Currency != "" && r.DeclaredValue.Validate() != nil) {
		return ErrInvalidLogisticsRequest
	}
	if (r.PaymentValue.Currency == "" && r.PaymentValue.MinorUnits != 0) || (r.PaymentValue.Currency != "" && r.PaymentValue.Validate() != nil) {
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
	Items          []ShipmentItem   `json:"items,omitempty"`
	DeclaredValue  LogisticsMoney   `json:"declared_value,omitempty"`
	DeliveryCost   LogisticsMoney   `json:"delivery_cost,omitempty"`
	PaymentValue   LogisticsMoney   `json:"payment_value,omitempty"`
}
type ShipmentResult struct {
	RemoteID, Status string
	Cost             LogisticsMoney
	TrackingNumber   string
	ObservedAt       time.Time
}
type ShipmentStatusRequest struct{ RemoteID string }

// ShipmentCancelRequest selects the bounded neutral cancellation variant.
// Empty Variant means delivery-to-address for backward compatibility.
type ShipmentCancelRequest struct {
	RemoteID, IdempotencyKey string
	Variant                  string
}
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

// LogisticsSeparateReturnRequest describes one return shipment that is not
// linked to an existing carrier RPO. Addresses and names are request-scoped
// PII and must not be persisted in operation receipts or logs.
type LogisticsSeparateReturnRequest struct {
	From              Address  `json:"from"`
	To                *Address `json:"to,omitempty"`
	InsuredValueMinor int64    `json:"insured_value_minor"`
	MailType          string   `json:"mail_type"`
	OrderNumber       string   `json:"order_number,omitempty"`
	PostOfficeCode    string   `json:"postoffice_code,omitempty"`
	RecipientName     string   `json:"recipient_name"`
	SenderName        string   `json:"sender_name"`
	IdempotencyKey    string   `json:"idempotency_key"`
}

// LogisticsSeparateReturnDeleteRequest requests deletion of one standalone
// return shipment. The barcode remains a provider reference and the operation
// is protected by the host-side idempotency boundary.
type LogisticsSeparateReturnDeleteRequest struct {
	ReturnBarcode  string `json:"return_barcode"`
	IdempotencyKey string `json:"idempotency_key"`
}

// LogisticsSeparateReturnUpdateRequest describes the editable fields of one
// standalone return shipment. The barcode is the provider-owned identity and
// is never replaced by the update operation.
type LogisticsSeparateReturnUpdateRequest struct {
	ReturnBarcode     string   `json:"return_barcode"`
	From              Address  `json:"from"`
	To                *Address `json:"to,omitempty"`
	InsuredValueMinor int64    `json:"insured_value_minor"`
	MailType          string   `json:"mail_type"`
	OrderNumber       string   `json:"order_number,omitempty"`
	PostOfficeCode    string   `json:"postoffice_code,omitempty"`
	RecipientName     string   `json:"recipient_name"`
	SenderName        string   `json:"sender_name"`
	IdempotencyKey    string   `json:"idempotency_key"`
}

// LogisticsSeparateReturnDeletion is the normalized acknowledgement of a
// standalone return deletion.
type LogisticsSeparateReturnDeletion struct {
	RemoteID   string
	Status     string
	Deleted    bool
	ObservedAt time.Time
}

// LogisticsSeparateReturnUpdate is the normalized acknowledgement of a
// standalone return edit.
type LogisticsSeparateReturnUpdate struct {
	RemoteID   string
	Status     string
	Updated    bool
	ObservedAt time.Time
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

// LogisticsBatchLookupQuery bounds a provider batch lookup by its remote
// name. The name remains a provider reference and is not a Core identifier.
type LogisticsBatchLookupQuery struct {
	BatchID string
}

// Validate checks the provider batch reference.
func (query LogisticsBatchLookupQuery) Validate() error {
	if !logisticsRefPattern.MatchString(query.BatchID) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsBatchOrdersQuery bounds a provider batch-order read. The batch
// identifier remains a provider reference and the host projects only safe
// order identity/status fields.
type LogisticsBatchOrdersQuery struct {
	BatchID string
	Limit   int
	Page    int
}

// LogisticsOrderQuery bounds a read of one provider order by its remote ID.
// The provider reference remains opaque to Core and is validated again by the
// host adapter when a connector has a stricter identifier contract.
type LogisticsOrderQuery struct {
	RemoteID string
}

// Validate checks the provider order reference.
func (query LogisticsOrderQuery) Validate() error {
	if !logisticsRefPattern.MatchString(query.RemoteID) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsOrderSearchQuery bounds a provider order search by the merchant's
// assigned order number.
type LogisticsOrderSearchQuery struct {
	ExternalID string
	Limit      int
}

// Validate checks the bounded merchant-order search.
func (query LogisticsOrderSearchQuery) Validate(maxLimit int) error {
	if maxLimit < 1 || query.Limit < 1 || query.Limit > maxLimit || !logisticsRefPattern.MatchString(query.ExternalID) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsOrderSummary is the safe projection of one provider order search
// result. Batch and tracking references are optional because backlog orders
// may not have entered a provider batch yet.
type LogisticsOrderSummary struct {
	RemoteID       string
	ExternalID     string
	BatchID        string
	TrackingNumber string
	Status         string
	ObservedAt     time.Time
}

// Validate checks the normalized order search projection.
func (order LogisticsOrderSummary) Validate() error {
	if !logisticsRefPattern.MatchString(order.RemoteID) || !logisticsRefPattern.MatchString(order.ExternalID) || !safeCodePattern.MatchString(order.Status) || order.ObservedAt.IsZero() || order.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	if order.BatchID != "" && !logisticsRefPattern.MatchString(order.BatchID) {
		return ErrInvalidLogisticsRequest
	}
	if order.TrackingNumber != "" && !logisticsRefPattern.MatchString(order.TrackingNumber) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsArchiveBatchQuery bounds a read of the provider archive. The
// archive endpoint has no client-side page parameter, so the host bounds the
// returned collection after the fixed HTTPS request.
type LogisticsArchiveBatchQuery struct {
	Limit int
}

// Validate checks the bounded archive query.
func (query LogisticsArchiveBatchQuery) Validate(maxLimit int) error {
	if maxLimit < 1 || query.Limit < 1 || query.Limit > maxLimit {
		return ErrInvalidLogisticsRequest
	}
	return nil
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

// Validate checks the bounded order-list query for one provider batch.
func (query LogisticsBatchOrdersQuery) Validate(maxLimit int) error {
	if maxLimit < 1 || query.Limit < 1 || query.Limit > maxLimit || query.Page < 0 || query.Page > 100000 || !logisticsRefPattern.MatchString(query.BatchID) {
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

// LogisticsBatchOrder is the safe neutral projection of one provider order
// row inside a batch. Recipient and address fields never cross the connector
// boundary.
type LogisticsBatchOrder struct {
	RemoteID       string
	BatchID        string
	TrackingNumber string
	Status         string
	ObservedAt     time.Time
}

// Validate checks the normalized provider order projection.
func (order LogisticsBatchOrder) Validate() error {
	if !logisticsRefPattern.MatchString(order.RemoteID) || !logisticsRefPattern.MatchString(order.BatchID) || !safeCodePattern.MatchString(order.Status) || order.ObservedAt.IsZero() || order.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	if order.TrackingNumber != "" && !logisticsRefPattern.MatchString(order.TrackingNumber) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsBatchSubmission is the normalized acknowledgement of a provider
// batch hand-off. It intentionally does not claim that every parcel was
// physically accepted by the carrier.
type LogisticsBatchSubmission struct {
	RemoteID   string
	Status     string
	Accepted   bool
	ObservedAt time.Time
}

// Validate checks the normalized batch hand-off acknowledgement.
func (submission LogisticsBatchSubmission) Validate() error {
	if !logisticsRefPattern.MatchString(submission.RemoteID) || !logisticsRefPattern.MatchString(submission.Status) || !submission.Accepted || submission.ObservedAt.IsZero() || submission.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsBatchCreateRequest requests formation of one remote carrier
// batch from already-created provider orders. Order identifiers remain
// provider references and never become canonical shipment identities.
type LogisticsBatchCreateRequest struct {
	OrderIDs         []string `json:"order_ids"`
	SendingDate      string   `json:"sending_date,omitempty"`
	UseOnlineBalance bool     `json:"use_online_balance"`
	IdempotencyKey   string   `json:"idempotency_key"`
}

// LogisticsBatchSubmitRequest requests hand-off of one formed provider batch
// to postal processing. The batch identifier remains a provider reference.
type LogisticsBatchSubmitRequest struct {
	BatchID          string `json:"batch_id"`
	UseOnlineBalance bool   `json:"use_online_balance"`
	IdempotencyKey   string `json:"idempotency_key"`
}

// LogisticsBatchArchiveRequest requests moving one formed provider batch to
// the provider archive. The batch identifier remains a provider reference.
type LogisticsBatchArchiveRequest struct {
	BatchID        string `json:"batch_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// LogisticsBatchArchive is the normalized acknowledgement of a batch archive
// operation.
type LogisticsBatchArchive struct {
	RemoteID   string
	Status     string
	Archived   bool
	ObservedAt time.Time
}

// LogisticsBatchUnarchiveRequest requests restoring one provider batch from
// the archive. The batch identifier remains a provider reference.
type LogisticsBatchUnarchiveRequest struct {
	BatchID        string `json:"batch_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// LogisticsBatchUnarchive is the normalized acknowledgement of a batch
// restore operation.
type LogisticsBatchUnarchive struct {
	RemoteID   string
	Status     string
	Archived   bool
	ObservedAt time.Time
}

// LogisticsBatchCancelRequest requests cancellation of one provider-side
// pre-alert batch. The batch identifier remains a provider reference.
type LogisticsBatchCancelRequest struct {
	BatchID        string `json:"batch_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// LogisticsBatchCancellation is the normalized acknowledgement of a
// provider-side pre-alert batch cancellation.
type LogisticsBatchCancellation struct {
	RemoteID   string
	Status     string
	Cancelled  bool
	ObservedAt time.Time
}

// LogisticsBatchSendingDateRequest requests changing the provider hand-off
// date for one formed batch. The provider batch reference remains opaque to
// Core and the operation is approval- and idempotency-gated by the host.
type LogisticsBatchSendingDateRequest struct {
	BatchID        string `json:"batch_id"`
	SendingDate    string `json:"sending_date"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Validate checks the provider batch reference, ISO date and idempotency key.
func (request LogisticsBatchSendingDateRequest) Validate() error {
	if !logisticsRefPattern.MatchString(request.BatchID) || !logisticsRefPattern.MatchString(request.IdempotencyKey) || !logisticsBatchDatePattern.MatchString(request.SendingDate) {
		return ErrInvalidLogisticsRequest
	}
	if _, err := time.Parse("2006-01-02", request.SendingDate); err != nil {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsBatchSendingDateUpdate is the normalized acknowledgement of a
// provider batch sending-date update.
type LogisticsBatchSendingDateUpdate struct {
	RemoteID    string
	SendingDate string
	Status      string
	Updated     bool
	ObservedAt  time.Time
}

// Validate checks the normalized batch sending-date acknowledgement.
func (update LogisticsBatchSendingDateUpdate) Validate() error {
	if !logisticsRefPattern.MatchString(update.RemoteID) || update.Status != "UPDATED" || !update.Updated || !logisticsBatchDatePattern.MatchString(update.SendingDate) || update.ObservedAt.IsZero() || update.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	if _, err := time.Parse("2006-01-02", update.SendingDate); err != nil {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// LogisticsOrderRestoreRequest requests moving provider orders from a formed
// batch back to the provider backlog. Order identifiers remain provider
// references and the operation is independently approval- and
// idempotency-gated.
type LogisticsOrderRestoreRequest struct {
	OrderIDs       []string `json:"order_ids"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// LogisticsOrderRestore is the normalized acknowledgement that every
// requested provider order was returned to the backlog.
type LogisticsOrderRestore struct {
	OrderIDs   []string
	Status     string
	ObservedAt time.Time
}

// Validate checks the bounded batch formation request.
func (request LogisticsBatchCreateRequest) Validate() error {
	if len(request.OrderIDs) < 1 || len(request.OrderIDs) > 100 || !logisticsRefPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	seen := make(map[string]struct{}, len(request.OrderIDs))
	for _, orderID := range request.OrderIDs {
		if !logisticsRefPattern.MatchString(orderID) {
			return ErrInvalidLogisticsRequest
		}
		if _, duplicate := seen[orderID]; duplicate {
			return ErrInvalidLogisticsRequest
		}
		seen[orderID] = struct{}{}
	}
	if request.SendingDate != "" {
		if !logisticsBatchDatePattern.MatchString(request.SendingDate) {
			return ErrInvalidLogisticsRequest
		}
		if _, err := time.Parse("2006-01-02", request.SendingDate); err != nil {
			return ErrInvalidLogisticsRequest
		}
	}
	return nil
}

// Validate checks the bounded batch hand-off request.
func (request LogisticsBatchSubmitRequest) Validate() error {
	if !logisticsRefPattern.MatchString(request.BatchID) || !logisticsRefPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded batch archive request.
func (request LogisticsBatchArchiveRequest) Validate() error {
	if !logisticsRefPattern.MatchString(request.BatchID) || !logisticsRefPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the normalized batch archive acknowledgement.
func (archive LogisticsBatchArchive) Validate() error {
	if !logisticsRefPattern.MatchString(archive.RemoteID) || archive.Status != "ARCHIVED" || !archive.Archived || archive.ObservedAt.IsZero() || archive.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded batch restore request.
func (request LogisticsBatchUnarchiveRequest) Validate() error {
	if !logisticsRefPattern.MatchString(request.BatchID) || !logisticsRefPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the normalized batch restore acknowledgement.
func (restore LogisticsBatchUnarchive) Validate() error {
	if !logisticsRefPattern.MatchString(restore.RemoteID) || restore.Status != "RESTORED" || restore.Archived || restore.ObservedAt.IsZero() || restore.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded provider batch cancellation request.
func (request LogisticsBatchCancelRequest) Validate() error {
	if !logisticsRefPattern.MatchString(request.BatchID) || !logisticsRefPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the normalized provider batch cancellation acknowledgement.
func (cancellation LogisticsBatchCancellation) Validate() error {
	if !logisticsRefPattern.MatchString(cancellation.RemoteID) || cancellation.Status != "CANCELLED" || !cancellation.Cancelled || cancellation.ObservedAt.IsZero() || cancellation.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded provider order references and acknowledgement.
func (request LogisticsOrderRestoreRequest) Validate() error {
	if len(request.OrderIDs) < 1 || len(request.OrderIDs) > 100 || !logisticsRefPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	seen := make(map[string]struct{}, len(request.OrderIDs))
	for _, orderID := range request.OrderIDs {
		if !logisticsRefPattern.MatchString(orderID) {
			return ErrInvalidLogisticsRequest
		}
		if _, duplicate := seen[orderID]; duplicate {
			return ErrInvalidLogisticsRequest
		}
		seen[orderID] = struct{}{}
	}
	return nil
}

// Validate checks that a restore acknowledgement contains a bounded set of
// provider order references and no provider-specific payload.
func (restore LogisticsOrderRestore) Validate() error {
	if len(restore.OrderIDs) < 1 || len(restore.OrderIDs) > 100 || restore.Status != "restored" || restore.ObservedAt.IsZero() || restore.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	seen := make(map[string]struct{}, len(restore.OrderIDs))
	for _, orderID := range restore.OrderIDs {
		if !logisticsRefPattern.MatchString(orderID) {
			return ErrInvalidLogisticsRequest
		}
		if _, duplicate := seen[orderID]; duplicate {
			return ErrInvalidLogisticsRequest
		}
		seen[orderID] = struct{}{}
	}
	return nil
}

// Validate checks the normalized batch projection.
func (batch LogisticsBatch) Validate() error {
	if !logisticsRefPattern.MatchString(batch.RemoteID) || !logisticsRefPattern.MatchString(batch.Status) || batch.ShipmentCount < 0 || batch.ShipmentCount > 1000000 || batch.ObservedAt.IsZero() || batch.ObservedAt.Location() != time.UTC {
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
	if len(r.Items) > 100 {
		return ErrInvalidLogisticsRequest
	}
	for _, item := range r.Items {
		if item.Validate() != nil {
			return ErrInvalidLogisticsRequest
		}
	}
	if r.DeclaredValue.Currency != "" && r.DeclaredValue.Validate() != nil {
		return ErrInvalidLogisticsRequest
	}
	if r.DeliveryCost.Currency != "" && r.DeliveryCost.Validate() != nil {
		return ErrInvalidLogisticsRequest
	}
	if r.PaymentValue.Currency != "" && r.PaymentValue.Validate() != nil {
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

// Validate checks the bounded, provider-neutral separate-return request.
func (r LogisticsSeparateReturnRequest) Validate() error {
	return validateLogisticsSeparateReturnFields(r.From, r.To, r.InsuredValueMinor, r.MailType, r.OrderNumber, r.PostOfficeCode, r.RecipientName, r.SenderName, r.IdempotencyKey)
}

// Validate checks the bounded standalone-return deletion request.
func (r LogisticsSeparateReturnDeleteRequest) Validate() error {
	if !logisticsRefPattern.MatchString(r.ReturnBarcode) || !logisticsRefPattern.MatchString(r.IdempotencyKey) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the bounded standalone-return edit request.
func (r LogisticsSeparateReturnUpdateRequest) Validate() error {
	if !logisticsRefPattern.MatchString(r.ReturnBarcode) {
		return ErrInvalidLogisticsRequest
	}
	return validateLogisticsSeparateReturnFields(r.From, r.To, r.InsuredValueMinor, r.MailType, r.OrderNumber, r.PostOfficeCode, r.RecipientName, r.SenderName, r.IdempotencyKey)
}

// Validate checks the normalized standalone-return deletion acknowledgement.
func (r LogisticsSeparateReturnDeletion) Validate() error {
	if !logisticsRefPattern.MatchString(r.RemoteID) || r.Status != "DELETED" || !r.Deleted || r.ObservedAt.IsZero() || r.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

// Validate checks the normalized standalone-return edit acknowledgement.
func (r LogisticsSeparateReturnUpdate) Validate() error {
	if !logisticsRefPattern.MatchString(r.RemoteID) || r.Status != "UPDATED" || !r.Updated || r.ObservedAt.IsZero() || r.ObservedAt.Location() != time.UTC {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

func validateLogisticsSeparateReturnFields(from Address, to *Address, insuredValueMinor int64, mailType, orderNumber, postOfficeCode, recipientName, senderName, idempotencyKey string) error {
	if from.Validate() != nil || (to != nil && to.Validate() != nil) || insuredValueMinor < 0 || insuredValueMinor%100 != 0 || !logisticsReturnCodePattern.MatchString(strings.ToUpper(strings.TrimSpace(mailType))) || !logisticsRefPattern.MatchString(idempotencyKey) || !validLogisticsText(recipientName, 300) || !validLogisticsText(senderName, 300) {
		return ErrInvalidLogisticsRequest
	}
	if orderNumber != "" && !logisticsRefPattern.MatchString(orderNumber) {
		return ErrInvalidLogisticsRequest
	}
	if postOfficeCode != "" && !logisticsRefPattern.MatchString(postOfficeCode) {
		return ErrInvalidLogisticsRequest
	}
	return nil
}

func validLogisticsText(value string, maxRunes int) bool {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, symbol := range value {
		if unicode.IsControl(symbol) {
			return false
		}
	}
	return true
}

func validLogisticsOptionalText(value string, maxRunes int) bool {
	return value == "" || validLogisticsText(value, maxRunes)
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
type LogisticsSeparateReturnCreator interface {
	CreateLogisticsSeparateReturn(context.Context, Account, Runtime, LogisticsSeparateReturnRequest) (ShipmentResult, error)
}
type LogisticsSeparateReturnDeleter interface {
	DeleteLogisticsSeparateReturn(context.Context, Account, Runtime, LogisticsSeparateReturnDeleteRequest) (LogisticsSeparateReturnDeletion, error)
}
type LogisticsSeparateReturnEditor interface {
	EditLogisticsSeparateReturn(context.Context, Account, Runtime, LogisticsSeparateReturnUpdateRequest) (LogisticsSeparateReturnUpdate, error)
}
type LogisticsLabelReader interface {
	ReadLogisticsLabel(context.Context, Account, Runtime, LabelRequest) (LabelResult, error)
}
type LogisticsBatchReader interface {
	ReadLogisticsBatches(context.Context, Account, Runtime, LogisticsBatchQuery) ([]LogisticsBatch, error)
}
type LogisticsBatchLookupReader interface {
	ReadLogisticsBatchByName(context.Context, Account, Runtime, LogisticsBatchLookupQuery) (LogisticsBatch, error)
}
type LogisticsBatchOrderReader interface {
	ReadLogisticsBatchOrders(context.Context, Account, Runtime, LogisticsBatchOrdersQuery) ([]LogisticsBatchOrder, error)
}
type LogisticsOrderReader interface {
	ReadLogisticsOrder(context.Context, Account, Runtime, LogisticsOrderQuery) (LogisticsBatchOrder, error)
}
type LogisticsOrderSearcher interface {
	SearchLogisticsOrders(context.Context, Account, Runtime, LogisticsOrderSearchQuery) ([]LogisticsOrderSummary, error)
}
type LogisticsArchivedBatchReader interface {
	ReadArchivedLogisticsBatches(context.Context, Account, Runtime, LogisticsArchiveBatchQuery) ([]LogisticsBatch, error)
}
type LogisticsBatchCreator interface {
	CreateLogisticsBatch(context.Context, Account, Runtime, LogisticsBatchCreateRequest) (LogisticsBatch, error)
}
type LogisticsBatchSubmitter interface {
	SubmitLogisticsBatch(context.Context, Account, Runtime, LogisticsBatchSubmitRequest) (LogisticsBatchSubmission, error)
}
type LogisticsBatchArchiver interface {
	ArchiveLogisticsBatch(context.Context, Account, Runtime, LogisticsBatchArchiveRequest) (LogisticsBatchArchive, error)
}
type LogisticsBatchUnarchiver interface {
	UnarchiveLogisticsBatch(context.Context, Account, Runtime, LogisticsBatchUnarchiveRequest) (LogisticsBatchUnarchive, error)
}
type LogisticsBatchCanceler interface {
	CancelLogisticsBatch(context.Context, Account, Runtime, LogisticsBatchCancelRequest) (LogisticsBatchCancellation, error)
}
type LogisticsBatchSendingDateUpdater interface {
	UpdateLogisticsBatchSendingDate(context.Context, Account, Runtime, LogisticsBatchSendingDateRequest) (LogisticsBatchSendingDateUpdate, error)
}
type LogisticsOrderRestorer interface {
	RestoreLogisticsOrders(context.Context, Account, Runtime, LogisticsOrderRestoreRequest) (LogisticsOrderRestore, error)
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
