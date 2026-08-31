package pochtarussia

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Transport is the host-mediated Russian Post Otpravka API boundary.
// Plaintext credentials are valid only for the duration of each callback.
type Transport interface {
	Ping(context.Context, []byte) error
	Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error)
	Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error)
	Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error)
	Return(context.Context, []byte, sdk.ReturnCreateRequest) (sdk.ShipmentResult, error)
	CreateSeparateReturn(context.Context, []byte, sdk.LogisticsSeparateReturnRequest) (sdk.ShipmentResult, error)
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error)
	Batches(context.Context, []byte, sdk.LogisticsBatchQuery) ([]sdk.LogisticsBatch, error)
	CreateBatch(context.Context, []byte, sdk.LogisticsBatchCreateRequest) (sdk.LogisticsBatch, error)
	SubmitBatch(context.Context, []byte, sdk.LogisticsBatchSubmitRequest) (sdk.LogisticsBatchSubmission, error)
	ArchiveBatch(context.Context, []byte, sdk.LogisticsBatchArchiveRequest) (sdk.LogisticsBatchArchive, error)
	UnarchiveBatch(context.Context, []byte, sdk.LogisticsBatchUnarchiveRequest) (sdk.LogisticsBatchUnarchive, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}
