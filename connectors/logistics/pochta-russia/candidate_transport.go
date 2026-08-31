package pochtarussia

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// candidateTransport is deterministic and never opens a network connection.
// It is used by unit and conformance tests only.
type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }

func (candidateTransport) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	observed := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	return []sdk.RateQuote{{
		ServiceCode:   "pochta_parcel_online",
		Cost:          sdk.LogisticsMoney{MinorUnits: 65000, Currency: "RUB"},
		MinDeliveryAt: observed.Add(3 * 24 * time.Hour),
		MaxDeliveryAt: observed.Add(6 * 24 * time.Hour),
		ObservedAt:    observed,
	}}, nil
}

func (candidateTransport) Create(_ context.Context, _ []byte, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: "57565818", Status: "created", TrackingNumber: "",
		Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) Cancel(_ context.Context, _ []byte, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: request.RemoteID, Status: "cancelled", Cost: sdk.LogisticsMoney{Currency: "RUB"},
		ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) Return(_ context.Context, _ []byte, request sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: "RA644000002RU", Status: "created", TrackingNumber: "RA644000002RU",
		Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) CreateSeparateReturn(_ context.Context, _ []byte, request sdk.LogisticsSeparateReturnRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: "RA644000003RU", Status: "created", TrackingNumber: "RA644000003RU",
		Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) DeleteSeparateReturn(_ context.Context, _ []byte, request sdk.LogisticsSeparateReturnDeleteRequest) (sdk.LogisticsSeparateReturnDeletion, error) {
	return sdk.LogisticsSeparateReturnDeletion{RemoteID: request.ReturnBarcode, Status: "DELETED", Deleted: true, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) EditSeparateReturn(_ context.Context, _ []byte, request sdk.LogisticsSeparateReturnUpdateRequest) (sdk.LogisticsSeparateReturnUpdate, error) {
	return sdk.LogisticsSeparateReturnUpdate{RemoteID: request.ReturnBarcode, Status: "UPDATED", Updated: true, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) Track(_ context.Context, _ []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: request.RemoteID, Status: "in_transit", TrackingNumber: request.RemoteID,
		Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) Label(_ context.Context, _ []byte, request sdk.LabelRequest) (sdk.LabelResult, error) {
	artifactRef := "pochta-russia:form:backlog:" + request.RemoteID
	if request.Format == "formed_order_pdf" {
		artifactRef = "pochta-russia:form:formed-order:" + request.RemoteID
	}
	if request.Format == "batch_f103_pdf" {
		artifactRef = "pochta-russia:form:batch-f103:" + request.RemoteID
	}
	return sdk.LabelResult{ArtifactRef: artifactRef, MediaType: "application/pdf", ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) Pickup(_ context.Context, _ []byte, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{
		RemoteID: "101000", Name: "Почта России · ОПС 101000", Country: query.Country,
		City: query.City, Address: "Москва, Чистопрудный бульвар, 1", Active: true,
		UpdatedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC),
	}}, nil
}

func (candidateTransport) Batches(_ context.Context, _ []byte, query sdk.LogisticsBatchQuery) ([]sdk.LogisticsBatch, error) {
	if err := query.Validate(100); err != nil {
		return nil, err
	}
	now := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	return []sdk.LogisticsBatch{{RemoteID: "batch-conformance-001", Status: "CREATED", ShipmentCount: 2, ObservedAt: now}}, nil
}

func (candidateTransport) BatchByName(_ context.Context, _ []byte, query sdk.LogisticsBatchLookupQuery) (sdk.LogisticsBatch, error) {
	if err := query.Validate(); err != nil {
		return sdk.LogisticsBatch{}, err
	}
	return sdk.LogisticsBatch{RemoteID: query.BatchID, Status: "CREATED", ShipmentCount: 2, ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) BatchOrders(_ context.Context, _ []byte, query sdk.LogisticsBatchOrdersQuery) ([]sdk.LogisticsBatchOrder, error) {
	if err := query.Validate(100); err != nil {
		return nil, err
	}
	now := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	return []sdk.LogisticsBatchOrder{
		{RemoteID: "57565818", BatchID: query.BatchID, TrackingNumber: "80084740397510", Status: "created", ObservedAt: now},
		{RemoteID: "57565819", BatchID: query.BatchID, TrackingNumber: "80084740397527", Status: "created", ObservedAt: now},
	}, nil
}

func (candidateTransport) OrderInBatch(_ context.Context, _ []byte, query sdk.LogisticsOrderQuery) (sdk.LogisticsBatchOrder, error) {
	if err := query.Validate(); err != nil {
		return sdk.LogisticsBatchOrder{}, err
	}
	now := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	return sdk.LogisticsBatchOrder{RemoteID: query.RemoteID, BatchID: "24", TrackingNumber: "80084740397510", Status: "created", ObservedAt: now}, nil
}

func (candidateTransport) SearchOrders(_ context.Context, _ []byte, query sdk.LogisticsOrderSearchQuery) ([]sdk.LogisticsOrderSummary, error) {
	if err := query.Validate(100); err != nil {
		return nil, err
	}
	return []sdk.LogisticsOrderSummary{{RemoteID: "57565818", ExternalID: query.ExternalID, BatchID: "24", TrackingNumber: "80084740397510", Status: "created", ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)}}, nil
}

func (candidateTransport) ArchivedBatches(_ context.Context, _ []byte, query sdk.LogisticsArchiveBatchQuery) ([]sdk.LogisticsBatch, error) {
	if err := query.Validate(100); err != nil {
		return nil, err
	}
	now := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	return []sdk.LogisticsBatch{{RemoteID: "batch-archive-conformance-001", Status: "ARCHIVED", ShipmentCount: 2, ObservedAt: now}}, nil
}

func (candidateTransport) CreateBatch(_ context.Context, _ []byte, request sdk.LogisticsBatchCreateRequest) (sdk.LogisticsBatch, error) {
	return sdk.LogisticsBatch{RemoteID: "batch-conformance-created-001", Status: "CREATED", ShipmentCount: len(request.OrderIDs), ObservedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) SubmitBatch(_ context.Context, _ []byte, request sdk.LogisticsBatchSubmitRequest) (sdk.LogisticsBatchSubmission, error) {
	return sdk.LogisticsBatchSubmission{RemoteID: request.BatchID, Status: "SUBMITTED", Accepted: true, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) ArchiveBatch(_ context.Context, _ []byte, request sdk.LogisticsBatchArchiveRequest) (sdk.LogisticsBatchArchive, error) {
	return sdk.LogisticsBatchArchive{RemoteID: request.BatchID, Status: "ARCHIVED", Archived: true, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) UnarchiveBatch(_ context.Context, _ []byte, request sdk.LogisticsBatchUnarchiveRequest) (sdk.LogisticsBatchUnarchive, error) {
	return sdk.LogisticsBatchUnarchive{RemoteID: request.BatchID, Status: "RESTORED", Archived: false, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) UpdateBatchSendingDate(_ context.Context, _ []byte, request sdk.LogisticsBatchSendingDateRequest) (sdk.LogisticsBatchSendingDateUpdate, error) {
	return sdk.LogisticsBatchSendingDateUpdate{RemoteID: request.BatchID, SendingDate: request.SendingDate, Status: "UPDATED", Updated: true, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}

func (candidateTransport) RestoreOrders(_ context.Context, _ []byte, request sdk.LogisticsOrderRestoreRequest) (sdk.LogisticsOrderRestore, error) {
	return sdk.LogisticsOrderRestore{OrderIDs: append([]string(nil), request.OrderIDs...), Status: "restored", ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}, nil
}
