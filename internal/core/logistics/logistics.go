// Package logistics defines the provider-neutral shipment lifecycle boundary.
// It contains no carrier, transport or persistence implementation.
package logistics

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrInvalidRecord = errors.New("logistics: invalid record")
	ErrInvalidScope  = errors.New("logistics: invalid tenant scope")
	ErrNotFound      = errors.New("logistics: shipment not found")
	ErrConflict      = errors.New("logistics: optimistic or idempotency conflict")
	ErrInProgress    = errors.New("logistics: operation is already in progress")
	ErrInvalidState  = errors.New("logistics: invalid shipment state")
)

var referencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ShipmentID is the canonical TORGNEXA identity of a shipment.
type ShipmentID string

// ParseShipmentID validates a canonical shipment identity.
func ParseShipmentID(value string) (ShipmentID, error) {
	if !referencePattern.MatchString(value) {
		return "", ErrInvalidRecord
	}
	return ShipmentID(value), nil
}

// String returns the textual shipment identity.
func (id ShipmentID) String() string { return string(id) }

// Valid reports whether the shipment identity is valid.
func (id ShipmentID) Valid() bool { return referencePattern.MatchString(string(id)) }

// Status is the bounded normalized status retained on the canonical shipment.
// Provider-specific status text belongs only in adapter-local translation.
type Status string

const (
	StatusPending   Status = "pending"
	StatusCreated   Status = "created"
	StatusInTransit Status = "in_transit"
	StatusDelivered Status = "delivered"
	StatusCancelled Status = "cancelled"
	StatusUnknown   Status = "unknown"
)

// Valid reports whether the status is a known canonical lifecycle value.
func (status Status) Valid() bool {
	switch status {
	case StatusPending, StatusCreated, StatusInTransit, StatusDelivered, StatusCancelled, StatusUnknown:
		return true
	default:
		return false
	}
}

// Shipment is the tenant-scoped local projection. Remote identifiers are
// references only; the canonical shipment ID is always owned by TORGNEXA.
type Shipment struct {
	ID             ShipmentID
	OrganizationID string
	WorkspaceID    string
	AccountID      string
	ExternalID     string
	RemoteID       string
	ServiceCode    string
	Status         Status
	TrackingNumber string
	CostMinorUnits int64
	Currency       string
	MinDeliveryAt  *time.Time
	MaxDeliveryAt  *time.Time
	Version        int64
	UpdatedAt      time.Time
}

// Validate checks tenant, reference, money, lifecycle and timestamp bounds.
func (shipment Shipment) Validate() error {
	if !shipment.ID.Valid() ||
		!referencePattern.MatchString(shipment.AccountID) || !referencePattern.MatchString(shipment.ExternalID) ||
		(shipment.RemoteID != "" && !referencePattern.MatchString(shipment.RemoteID)) ||
		!shipment.Status.Valid() || shipment.CostMinorUnits < 0 ||
		!currencyPattern.MatchString(shipment.Currency) || shipment.Version < 1 || shipment.UpdatedAt.IsZero() || shipment.UpdatedAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	if _, err := tenancy.ParseScope(shipment.OrganizationID, shipment.WorkspaceID); err != nil {
		return ErrInvalidRecord
	}
	if shipment.MinDeliveryAt != nil && shipment.MinDeliveryAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	if shipment.MaxDeliveryAt != nil && shipment.MaxDeliveryAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	if shipment.MinDeliveryAt != nil && shipment.MaxDeliveryAt != nil && shipment.MaxDeliveryAt.Before(*shipment.MinDeliveryAt) {
		return ErrInvalidRecord
	}
	return nil
}

// CreateCommand starts a local shipment before the remote side effect. The
// request payload is encrypted separately; only its opaque reference and
// digest cross the local durable boundary.
type CreateCommand struct {
	ID               ShipmentID
	AccountID        string
	ExternalID       string
	ServiceCode      string
	IdempotencyKey   string
	PayloadReference string
	PayloadDigest    string
}

// Validate checks the bounded local identity of a create request.
func (command CreateCommand) Validate() error {
	if !command.ID.Valid() || !referencePattern.MatchString(command.AccountID) || !referencePattern.MatchString(command.ExternalID) || !referencePattern.MatchString(command.ServiceCode) || !referencePattern.MatchString(command.IdempotencyKey) || !referencePattern.MatchString(command.PayloadReference) || !digestPattern.MatchString(command.PayloadDigest) {
		return ErrInvalidRecord
	}
	return nil
}

// ParsePayloadDigest validates the lowercase SHA-256 digest used to bind an
// encrypted request payload to its idempotent shipment command.
func ParsePayloadDigest(value string) (string, error) {
	if !digestPattern.MatchString(value) {
		return "", ErrInvalidRecord
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", ErrInvalidRecord
	}
	return strings.ToLower(value), nil
}

// RemoteResult contains only normalized data returned by a connector adapter.
// Raw provider payloads, credentials and recipient PII are not part of it.
type RemoteResult struct {
	RemoteID       string
	Status         Status
	TrackingNumber string
	CostMinorUnits int64
	Currency       string
	MinDeliveryAt  *time.Time
	MaxDeliveryAt  *time.Time
	ObservedAt     time.Time
}

// Validate checks the normalized remote result before persistence.
func (result RemoteResult) Validate() error {
	if !referencePattern.MatchString(result.RemoteID) || !result.Status.Valid() || result.CostMinorUnits < 0 || !currencyPattern.MatchString(result.Currency) || result.ObservedAt.IsZero() || result.ObservedAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	if result.TrackingNumber != "" && !referencePattern.MatchString(result.TrackingNumber) {
		return ErrInvalidRecord
	}
	if result.MinDeliveryAt != nil && result.MinDeliveryAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	if result.MaxDeliveryAt != nil && result.MaxDeliveryAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	if result.MinDeliveryAt != nil && result.MaxDeliveryAt != nil && result.MaxDeliveryAt.Before(*result.MinDeliveryAt) {
		return ErrInvalidRecord
	}
	return nil
}

// Mutation carries the audit/outbox identity of a local shipment change.
type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID, CausationID string
	ApprovalRequestID                                             string
	OccurredAt                                                    time.Time
}

// Validate checks the bounded mutation metadata.
func (mutation Mutation) Validate() error {
	if !referencePattern.MatchString(mutation.EventID) || !referencePattern.MatchString(mutation.AuditID) || !referencePattern.MatchString(mutation.ActorID) || !referencePattern.MatchString(mutation.Source) || !referencePattern.MatchString(mutation.CorrelationID) || (mutation.CausationID != "" && !referencePattern.MatchString(mutation.CausationID)) || (mutation.ApprovalRequestID != "" && !referencePattern.MatchString(mutation.ApprovalRequestID)) || mutation.OccurredAt.IsZero() || mutation.OccurredAt.Location() != time.UTC {
		return ErrInvalidRecord
	}
	return nil
}

// Repository is the host persistence port used by a future shipment worker.
// Begin methods return fresh=true only for the caller that owns the external
// side effect; replayed keys never issue a second remote command.
type Repository interface {
	Shipment(context.Context, tenancy.Scope, ShipmentID) (Shipment, error)
	BeginCreate(context.Context, tenancy.Scope, CreateCommand, Mutation) (Shipment, bool, error)
	ApplyCreateResult(context.Context, tenancy.Scope, ShipmentID, int64, RemoteResult, Mutation) (Shipment, error)
	ApplyCreateUnknown(context.Context, tenancy.Scope, ShipmentID, int64, Mutation) (Shipment, error)
	BeginCancel(context.Context, tenancy.Scope, ShipmentID, string, Mutation) (Shipment, bool, error)
	ApplyCancelResult(context.Context, tenancy.Scope, ShipmentID, int64, RemoteResult, Mutation) (Shipment, error)
	ApplyCancelUnknown(context.Context, tenancy.Scope, ShipmentID, int64, Mutation) (Shipment, error)
	AppendTrackingEvidence(context.Context, tenancy.Scope, ShipmentID, Status, time.Time) error
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
