package vetismercury

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
func (candidateTransport) Read(_ context.Context, _ []byte, q sdk.GovernmentDocumentRequest) (sdk.GovernmentDocument, error) {
	return sdk.GovernmentDocument{RemoteID: q.RemoteID, Kind: "vetd", Status: "completed", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Write(context.Context, []byte, sdk.GovernmentWriteRequest) (sdk.GovernmentWriteResult, error) {
	return sdk.GovernmentWriteResult{RemoteID: "vetd:synthetic", Status: "accepted", AcceptedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Inventory(context.Context, []byte, sdk.GovernmentInventoryRequest) (sdk.GovernmentInventoryObservation, error) {
	return sdk.GovernmentInventoryObservation{ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Reconcile(context.Context, []byte, sdk.GovernmentReconciliationRequest) (sdk.GovernmentReconciliationResult, error) {
	return sdk.GovernmentReconciliationResult{}, nil
}
