package sabyedo

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
func (candidateTransport) Read(_ context.Context, _ []byte, id string) (RemoteDocument, error) {
	return RemoteDocument{RemoteID: id, Kind: "upd", Status: "delivered", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) Send(_ context.Context, _ []byte, q sdk.EDOSendRequest) (RemoteDocument, error) {
	return RemoteDocument{RemoteID: "message:synthetic", ExternalID: q.ExternalID, Kind: q.Kind, Status: "sent", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) RequestSigning(context.Context, []byte, sdk.EDOSignWorkflowRequest) (sdk.EDOSignWorkflowResult, error) {
	return sdk.EDOSignWorkflowResult{WorkflowRef: "workflow:synthetic", Status: "requested", CreatedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
