package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

// FulfillmentMode describes who owns the physical execution of a plan.
// Provider names are deliberately not part of this value.
type FulfillmentMode string

const (
	FulfillmentFBS    FulfillmentMode = "fbs"
	FulfillmentFBO    FulfillmentMode = "fbo"
	FulfillmentHybrid FulfillmentMode = "hybrid"
	FulfillmentSplit  FulfillmentMode = "split"
)

// FulfillmentOwner identifies the bounded context that performs a step.
type FulfillmentOwner string

const (
	OwnerSellerWarehouse FulfillmentOwner = "seller_warehouse"
	OwnerMarketplace     FulfillmentOwner = "marketplace"
	OwnerCarrier         FulfillmentOwner = "carrier"
)

// MobileOperation is a command that may be exposed by a handheld workspace.
type MobileOperation string

const (
	MobileOperationPick    MobileOperation = "pick"
	MobileOperationPack    MobileOperation = "pack"
	MobileOperationPrint   MobileOperation = "print"
	MobileOperationHandoff MobileOperation = "handoff"
	MobileOperationScan    MobileOperation = "scan"
	MobileOperationObserve MobileOperation = "observe"
)

// MobileScanKind separates location, product, package and label scans.
type MobileScanKind string

const (
	ScanProduct  MobileScanKind = "product"
	ScanLocation MobileScanKind = "location"
	ScanPackage  MobileScanKind = "package"
	ScanSerial   MobileScanKind = "serial"
	ScanLabel    MobileScanKind = "label"
)

// MobilePrintDocument is a bounded document kind accepted by the print queue.
type MobilePrintDocument string

const (
	PrintLabel        MobilePrintDocument = "label"
	PrintPickList     MobilePrintDocument = "pick_list"
	PrintPackingSlip  MobilePrintDocument = "packing_slip"
	PrintManifest     MobilePrintDocument = "manifest"
	PrintInternalCode MobilePrintDocument = "internal_barcode"
)

// MobileFulfillmentModeValid reports whether a mode is a supported generic
// fulfillment mode.
func MobileFulfillmentModeValid(value FulfillmentMode) bool {
	switch value {
	case FulfillmentFBS, FulfillmentFBO, FulfillmentHybrid, FulfillmentSplit:
		return true
	default:
		return false
	}
}

// MobileOwnerForMode returns the default physical owner for a mode.
func MobileOwnerForMode(mode FulfillmentMode) FulfillmentOwner {
	if mode == FulfillmentFBO {
		return OwnerMarketplace
	}
	return OwnerSellerWarehouse
}

// MobileLocalOperationAllowed keeps remote FBO work out of the local WMS.
// Hybrid and split plans are allowed locally only for their seller-owned
// allocations; the plan's allocation mapping remains authoritative.
func MobileLocalOperationAllowed(mode FulfillmentMode, operation MobileOperation) bool {
	if !MobileFulfillmentModeValid(mode) {
		return false
	}
	if mode == FulfillmentFBO {
		return operation == MobileOperationObserve
	}
	return operation != MobileOperationObserve
}

// MobileCodeDigest returns a stable redacted reference for a transient scan
// value. Callers must never persist or log the original value.
func MobileCodeDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// ValidateMobileCode validates a transient barcode without retaining it.
func ValidateMobileCode(value string) error {
	if strings.TrimSpace(value) != value || value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
		return ErrInvalidRecord
	}
	return nil
}

// ValidateMobileLocation validates a warehouse location or zone code.
func ValidateMobileLocation(value string) error {
	if strings.TrimSpace(value) != value || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
		return ErrInvalidRecord
	}
	return nil
}

// MobileScanInput is the domain input for a scanner command. Code is
// transient and must be replaced by MobileCodeDigest at persistence edges.
type MobileScanInput struct {
	TaskID          string
	DeviceID        string
	Kind            MobileScanKind
	Code            string
	LocationCode    string
	ExpectedVersion int64
	Quantity        Quantity
}

// Validate checks the scan command before it reaches a repository.
func (input MobileScanInput) Validate() error {
	if !domain.ValidSortableID(input.TaskID) || !domain.ValidSortableID(input.DeviceID) || input.ExpectedVersion < 1 || input.Quantity.Validate() != nil || input.Quantity.Value.IsZero() || ValidateMobileCode(input.Code) != nil || ValidateMobileLocation(input.LocationCode) != nil {
		return ErrInvalidRecord
	}
	switch input.Kind {
	case ScanProduct, ScanLocation, ScanPackage, ScanSerial, ScanLabel:
		return nil
	default:
		return ErrInvalidRecord
	}
}

// PackageFacts are the exact physical facts captured at a packing station.
// Dimensions are millimetres and weight is grams; no binary floating point is
// accepted at this boundary.
type PackageFacts struct {
	PackageCount int
	WeightGrams  int64
	LengthMM     int64
	WidthMM      int64
	HeightMM     int64
}

// Validate checks conservative package bounds used by the mobile surface.
func (facts PackageFacts) Validate() error {
	if facts.PackageCount < 1 || facts.PackageCount > 100 || facts.WeightGrams < 0 || facts.WeightGrams > 2_000_000 || facts.LengthMM < 0 || facts.LengthMM > 10_000 || facts.WidthMM < 0 || facts.WidthMM > 10_000 || facts.HeightMM < 0 || facts.HeightMM > 10_000 {
		return ErrInvalidRecord
	}
	return nil
}

// ValidateMobilePrintRequest checks a print intent before it is queued.
func ValidateMobilePrintRequest(document MobilePrintDocument, copies int) error {
	if copies < 1 || copies > 20 {
		return ErrInvalidRecord
	}
	switch document {
	case PrintLabel, PrintPickList, PrintPackingSlip, PrintManifest, PrintInternalCode:
		return nil
	default:
		return ErrInvalidRecord
	}
}

// ValidateMobilePlan enforces the canonical mode/owner relationship.
func ValidateMobilePlan(mode FulfillmentMode, owner FulfillmentOwner, localExecution bool, warehouseID string) error {
	if !MobileFulfillmentModeValid(mode) || owner == "" || !domain.ValidSortableID(warehouseID) && localExecution {
		return ErrInvalidRecord
	}
	if mode == FulfillmentFBO && (owner != OwnerMarketplace || localExecution) {
		return errors.New("inventory: fbo plan cannot execute locally")
	}
	if mode != FulfillmentFBO && owner != OwnerSellerWarehouse && localExecution {
		return ErrInvalidRecord
	}
	return nil
}
