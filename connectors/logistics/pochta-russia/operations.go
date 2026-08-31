package pochtarussia

import (
	"context"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReadLogisticsRates calculates a bounded delivery tariff preview.
func (c *Connector) ReadLogisticsRates(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out []sdk.RateQuote
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Rates(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, quote := range out {
		if quote.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return out, nil
}

// CreateLogisticsShipment creates one order in the Russian Post backlog.
// Final batch formation and hand-off remain separate provider operations.
func (c *Connector) CreateLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Create(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if out.RemoteID == "" || out.Status != "created" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// CancelLogisticsShipment removes one new order from the Russian Post
// backlog. The provider's response must confirm the exact order ID.
func (c *Connector) CancelLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || strings.TrimSpace(request.RemoteID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Cancel(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if out.RemoteID != strings.TrimSpace(request.RemoteID) || out.Status != "cancelled" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// CreateLogisticsReturn creates one return shipment for an existing RPO. The
// separate-return form and return-label generation remain outside this
// bounded operation.
func (c *Connector) CreateLogisticsReturn(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Return(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if out.RemoteID == "" || out.Status != "created" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// CreateLogisticsSeparateReturn creates one Russian Post return shipment
// without an existing direct shipment barcode.
func (c *Connector) CreateLogisticsSeparateReturn(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsSeparateReturnRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.CreateSeparateReturn(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if out.RemoteID == "" || out.Status != "created" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() || out.TrackingNumber != out.RemoteID {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// DeleteLogisticsSeparateReturn deletes one standalone Russian Post return
// shipment through the provider's dedicated delete operation.
func (c *Connector) DeleteLogisticsSeparateReturn(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsSeparateReturnDeleteRequest) (sdk.LogisticsSeparateReturnDeletion, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LogisticsSeparateReturnDeletion{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LogisticsSeparateReturnDeletion
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.DeleteSeparateReturn(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsSeparateReturnDeletion{}, err
	}
	if out.Validate() != nil || out.RemoteID != request.ReturnBarcode {
		return sdk.LogisticsSeparateReturnDeletion{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// EditLogisticsSeparateReturn updates one standalone Russian Post return
// shipment. The adapter must confirm the same provider barcode.
func (c *Connector) EditLogisticsSeparateReturn(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsSeparateReturnUpdateRequest) (sdk.LogisticsSeparateReturnUpdate, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LogisticsSeparateReturnUpdate{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LogisticsSeparateReturnUpdate
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.EditSeparateReturn(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsSeparateReturnUpdate{}, err
	}
	if out.Validate() != nil || out.RemoteID != request.ReturnBarcode {
		return sdk.LogisticsSeparateReturnUpdate{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// ReadPickupPoints reads a bounded Russian Post office directory by city.
// Provider postal indexes remain remote references and never become Core
// warehouse identifiers.
func (c *Connector) ReadPickupPoints(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || query.Validate(500) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out []sdk.PickupPoint
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Pickup(ctx, secret, query)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, point := range out {
		if point.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return out, nil
}

// ReadLogisticsTracking reads one current shipment status through the
// provider's separate tracking service.
func (c *Connector) ReadLogisticsTracking(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || strings.TrimSpace(request.RemoteID) == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Track(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if out.RemoteID == "" || out.Status == "" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// ReadLogisticsLabel requests the official order print form. The provider
// returns a PDF body; the host transport converts it to an opaque artifact
// reference so provider credentials and binary content do not cross the SDK
// boundary.
func (c *Connector) ReadLogisticsLabel(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LabelRequest) (sdk.LabelResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LabelResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LabelResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Label(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if out.ArtifactRef == "" || out.MediaType != "application/pdf" || out.ObservedAt.IsZero() {
		return sdk.LabelResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// ReadLogisticsBatches reads a bounded page of Russian Post batches. The
// provider order list is intentionally not projected into this SDK surface.
func (c *Connector) ReadLogisticsBatches(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.LogisticsBatchQuery) ([]sdk.LogisticsBatch, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || query.Validate(100) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out []sdk.LogisticsBatch
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Batches(ctx, secret, query)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if len(out) > query.Limit {
		return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	for _, batch := range out {
		if batch.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return out, nil
}

// ReadArchivedLogisticsBatches reads a bounded page of Russian Post archived
// batches without projecting the provider's individual order rows.
func (c *Connector) ReadArchivedLogisticsBatches(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.LogisticsArchiveBatchQuery) ([]sdk.LogisticsBatch, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || query.Validate(100) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out []sdk.LogisticsBatch
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.ArchivedBatches(ctx, secret, query)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if len(out) > query.Limit {
		return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	for _, batch := range out {
		if batch.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return out, nil
}

// CreateLogisticsBatch forms one Russian Post batch from existing backlog
// orders. Handoff to postal processing remains a separate operation.
func (c *Connector) CreateLogisticsBatch(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsBatchCreateRequest) (sdk.LogisticsBatch, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LogisticsBatch{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LogisticsBatch
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.CreateBatch(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsBatch{}, err
	}
	if out.Validate() != nil || out.ShipmentCount != len(request.OrderIDs) {
		return sdk.LogisticsBatch{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// SubmitLogisticsBatch hands one formed Russian Post batch to postal
// processing through the provider's F103/check-in operation.
func (c *Connector) SubmitLogisticsBatch(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsBatchSubmitRequest) (sdk.LogisticsBatchSubmission, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LogisticsBatchSubmission{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LogisticsBatchSubmission
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.SubmitBatch(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsBatchSubmission{}, err
	}
	if out.Validate() != nil || out.RemoteID != request.BatchID {
		return sdk.LogisticsBatchSubmission{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// ArchiveLogisticsBatch moves one formed Russian Post batch to the provider
// archive. Archiving is reversible through a separate provider operation.
func (c *Connector) ArchiveLogisticsBatch(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsBatchArchiveRequest) (sdk.LogisticsBatchArchive, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LogisticsBatchArchive{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LogisticsBatchArchive
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.ArchiveBatch(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsBatchArchive{}, err
	}
	if out.Validate() != nil || out.RemoteID != request.BatchID {
		return sdk.LogisticsBatchArchive{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

// UnarchiveLogisticsBatch restores one Russian Post batch from the provider
// archive. Restoring is separate from archiving so approval can be scoped to
// the exact lifecycle transition.
func (c *Connector) UnarchiveLogisticsBatch(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LogisticsBatchUnarchiveRequest) (sdk.LogisticsBatchUnarchive, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LogisticsBatchUnarchive{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LogisticsBatchUnarchive
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.UnarchiveBatch(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsBatchUnarchive{}, err
	}
	if out.Validate() != nil || out.RemoteID != request.BatchID {
		return sdk.LogisticsBatchUnarchive{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

func useSecret(ctx context.Context, runtime sdk.Runtime, account sdk.Account, fn func([]byte) error) error {
	if runtime == nil || runtime.Secrets() == nil {
		return remote(sdk.ErrorUnauthorized, "credential_missing")
	}
	return runtime.Secrets().UseSecret(ctx, account.SecretReference, fn)
}

var _ sdk.PickupPointReader = (*Connector)(nil)
var _ sdk.LogisticsRateReader = (*Connector)(nil)
var _ sdk.LogisticsShipmentCreator = (*Connector)(nil)
var _ sdk.LogisticsShipmentCanceler = (*Connector)(nil)
var _ sdk.LogisticsReturnCreator = (*Connector)(nil)
var _ sdk.LogisticsSeparateReturnCreator = (*Connector)(nil)
var _ sdk.LogisticsSeparateReturnDeleter = (*Connector)(nil)
var _ sdk.LogisticsSeparateReturnEditor = (*Connector)(nil)
var _ sdk.LogisticsTracker = (*Connector)(nil)
var _ sdk.LogisticsLabelReader = (*Connector)(nil)
var _ sdk.LogisticsBatchReader = (*Connector)(nil)
var _ sdk.LogisticsArchivedBatchReader = (*Connector)(nil)
var _ sdk.LogisticsBatchCreator = (*Connector)(nil)
var _ sdk.LogisticsBatchSubmitter = (*Connector)(nil)
var _ sdk.LogisticsBatchArchiver = (*Connector)(nil)
var _ sdk.LogisticsBatchUnarchiver = (*Connector)(nil)
