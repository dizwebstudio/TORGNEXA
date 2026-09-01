package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// ErrInvalidMarketplaceOperation identifies a malformed provider-neutral
// marketplace operation. The connector must reject it before any remote IO.
var ErrInvalidMarketplaceOperation = errors.New("connectors: invalid marketplace operation")

var marketplaceQuantityPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,9})?$`)

// MarketplaceOperationStatus is the normalized outcome of a marketplace
// write. Unknown is used when a timeout leaves remote acceptance ambiguous.
type MarketplaceOperationStatus string

const (
	MarketplaceOperationApplied  MarketplaceOperationStatus = "applied"
	MarketplaceOperationRejected MarketplaceOperationStatus = "rejected"
	MarketplaceOperationUnknown  MarketplaceOperationStatus = "unknown"
	MarketplaceOperationDryRun   MarketplaceOperationStatus = "dry_run"
)

func (status MarketplaceOperationStatus) Valid() bool {
	return status == MarketplaceOperationApplied || status == MarketplaceOperationRejected || status == MarketplaceOperationUnknown || status == MarketplaceOperationDryRun
}

// MarketplaceOperationReceipt is a bounded, secret-free acknowledgement. A
// caller must reconcile an applied or unknown outcome before issuing another
// non-idempotent remote request.
type MarketplaceOperationReceipt struct {
	Status            MarketplaceOperationStatus `json:"status"`
	RemoteID          string                     `json:"remote_id,omitempty"`
	RemoteOperationID string                     `json:"remote_operation_id,omitempty"`
	ErrorCode         string                     `json:"error_code,omitempty"`
	ReadAfterWrite    bool                       `json:"read_after_write"`
	ObservedAt        time.Time                  `json:"observed_at"`
}

func (receipt MarketplaceOperationReceipt) Validate() error {
	if !receipt.Status.Valid() || receipt.ObservedAt.IsZero() || receipt.ObservedAt.Location() != time.UTC {
		return ErrInvalidMarketplaceOperation
	}
	for _, value := range []string{receipt.RemoteID, receipt.RemoteOperationID} {
		if value != "" && !validRemoteID(value) {
			return ErrInvalidMarketplaceOperation
		}
	}
	if receipt.ErrorCode != "" && !safeCodePattern.MatchString(receipt.ErrorCode) {
		return ErrInvalidMarketplaceOperation
	}
	if receipt.Status == MarketplaceOperationRejected && receipt.ErrorCode == "" {
		return ErrInvalidMarketplaceOperation
	}
	if receipt.Status == MarketplaceOperationApplied && receipt.RemoteID == "" && receipt.RemoteOperationID == "" {
		return ErrInvalidMarketplaceOperation
	}
	return nil
}

// ReservationRequest asks a marketplace connector to reserve or release a
// remote order quantity. The local WMS allocation remains authoritative.
type ReservationRequest struct {
	OrderRemoteID    string `json:"order_remote_id"`
	VariantRemoteID  string `json:"variant_remote_id"`
	LocationRemoteID string `json:"location_remote_id,omitempty"`
	ReservationID    string `json:"reservation_id,omitempty"`
	Quantity         string `json:"quantity"`
	Unit             string `json:"unit"`
	IdempotencyKey   string `json:"idempotency_key"`
	DryRun           bool   `json:"dry_run"`
}

func (request ReservationRequest) Validate() error {
	if !validRemoteID(request.OrderRemoteID) || !validRemoteID(request.VariantRemoteID) || !validOptionalRemoteID(request.LocationRemoteID) || !validOptionalRemoteID(request.ReservationID) || !marketplaceQuantityPattern.MatchString(request.Quantity) || request.Quantity == "0" || !validUnitCode(request.Unit) || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidMarketplaceOperation
	}
	return nil
}

// ReservationWriter is an additive SDK port. Providers must be separately
// qualified before runtime admission; merely implementing this interface does
// not change a manifest capability.
type ReservationWriter interface {
	ReserveMarketplaceOrder(context.Context, Account, Runtime, ReservationRequest) (MarketplaceOperationReceipt, error)
	ReleaseMarketplaceReservation(context.Context, Account, Runtime, ReservationRequest) (MarketplaceOperationReceipt, error)
}

// MarketplaceOrderAction is the outbound order lifecycle vocabulary. Provider
// status values stay inside adapters and are never exposed as Core branches.
type MarketplaceOrderAction string

const (
	MarketplaceOrderConfirm MarketplaceOrderAction = "confirm"
	MarketplaceOrderCancel  MarketplaceOrderAction = "cancel"
	MarketplaceOrderHandoff MarketplaceOrderAction = "handoff"
)

func (action MarketplaceOrderAction) Valid() bool {
	return action == MarketplaceOrderConfirm || action == MarketplaceOrderCancel || action == MarketplaceOrderHandoff
}

// MarketplaceOrderActionRequest is a typed, retry-safe order command.
type MarketplaceOrderActionRequest struct {
	OrderRemoteID  string                 `json:"order_remote_id"`
	Action         MarketplaceOrderAction `json:"action"`
	ReasonCode     string                 `json:"reason_code,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key"`
	DryRun         bool                   `json:"dry_run"`
}

func (request MarketplaceOrderActionRequest) Validate() error {
	if !validRemoteID(request.OrderRemoteID) || !request.Action.Valid() || (request.ReasonCode != "" && !safeCodePattern.MatchString(request.ReasonCode)) || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidMarketplaceOperation
	}
	return nil
}

// MarketplaceOrderActionWriter is the qualified outbound order status port.
type MarketplaceOrderActionWriter interface {
	ApplyMarketplaceOrderAction(context.Context, Account, Runtime, MarketplaceOrderActionRequest) (MarketplaceOperationReceipt, error)
}

// MarketplaceFulfillmentAction is the provider-neutral FBS/DBS lifecycle.
type MarketplaceFulfillmentAction string

const (
	FulfillmentPick           MarketplaceFulfillmentAction = "pick"
	FulfillmentPack           MarketplaceFulfillmentAction = "pack"
	FulfillmentCreateShipment MarketplaceFulfillmentAction = "create_shipment"
	FulfillmentHandoff        MarketplaceFulfillmentAction = "handoff"
	FulfillmentTrack          MarketplaceFulfillmentAction = "track"
)

func (action MarketplaceFulfillmentAction) Valid() bool {
	return action == FulfillmentPick || action == FulfillmentPack || action == FulfillmentCreateShipment || action == FulfillmentHandoff || action == FulfillmentTrack
}

// MarketplaceFulfillmentRequest represents one stage transition. Local WMS
// task and allocation references are required so the connector cannot invent
// stock or fulfillment state.
type MarketplaceFulfillmentRequest struct {
	OrderRemoteID       string                       `json:"order_remote_id"`
	ReservationRemoteID string                       `json:"reservation_remote_id,omitempty"`
	ShipmentRemoteID    string                       `json:"shipment_remote_id,omitempty"`
	Action              MarketplaceFulfillmentAction `json:"action"`
	TrackingNumber      string                       `json:"tracking_number,omitempty"`
	IdempotencyKey      string                       `json:"idempotency_key"`
	DryRun              bool                         `json:"dry_run"`
}

func (request MarketplaceFulfillmentRequest) Validate() error {
	if !validRemoteID(request.OrderRemoteID) || !validOptionalRemoteID(request.ReservationRemoteID) || !validOptionalRemoteID(request.ShipmentRemoteID) || !request.Action.Valid() || (request.TrackingNumber != "" && !validReadText(request.TrackingNumber, 192)) || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidMarketplaceOperation
	}
	if request.Action == FulfillmentTrack && request.TrackingNumber == "" && request.ShipmentRemoteID == "" {
		return ErrInvalidMarketplaceOperation
	}
	return nil
}

// MarketplaceFulfillmentWriter is qualified separately from order reads and
// status writes because shipment acceptance has different recovery semantics.
type MarketplaceFulfillmentWriter interface {
	ApplyMarketplaceFulfillment(context.Context, Account, Runtime, MarketplaceFulfillmentRequest) (MarketplaceOperationReceipt, error)
}

// MarketplaceReturnAction is the typed return/compensation vocabulary.
type MarketplaceReturnAction string

const (
	MarketplaceReturnAuthorize MarketplaceReturnAction = "authorize"
	MarketplaceReturnReceive   MarketplaceReturnAction = "receive"
	MarketplaceReturnReject    MarketplaceReturnAction = "reject"
	MarketplaceReturnRefund    MarketplaceReturnAction = "refund"
)

func (action MarketplaceReturnAction) Valid() bool {
	return action == MarketplaceReturnAuthorize || action == MarketplaceReturnReceive || action == MarketplaceReturnReject || action == MarketplaceReturnRefund
}

// MarketplaceReturnActionRequest carries only bounded references and exact
// decimal quantity. Refund money stays in the payment/settlement boundaries.
type MarketplaceReturnActionRequest struct {
	ReturnRemoteID string                  `json:"return_remote_id"`
	OrderRemoteID  string                  `json:"order_remote_id"`
	Action         MarketplaceReturnAction `json:"action"`
	Quantity       string                  `json:"quantity,omitempty"`
	Unit           string                  `json:"unit,omitempty"`
	IdempotencyKey string                  `json:"idempotency_key"`
	DryRun         bool                    `json:"dry_run"`
}

func (request MarketplaceReturnActionRequest) Validate() error {
	if !validRemoteID(request.ReturnRemoteID) || !validRemoteID(request.OrderRemoteID) || !request.Action.Valid() || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidMarketplaceOperation
	}
	if request.Quantity != "" && (!marketplaceQuantityPattern.MatchString(request.Quantity) || request.Quantity == "0" || !validUnitCode(request.Unit)) {
		return ErrInvalidMarketplaceOperation
	}
	return nil
}

// MarketplaceReturnWriter is admitted only after return/refund reconciliation
// and approval-bound qualification.
type MarketplaceReturnWriter interface {
	ApplyMarketplaceReturnAction(context.Context, Account, Runtime, MarketplaceReturnActionRequest) (MarketplaceOperationReceipt, error)
}

func validUnitCode(value string) bool {
	if value == "" || len(value) > 16 || value != strings.ToUpper(value) {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '.' && r != '-' {
			return false
		}
	}
	return true
}
