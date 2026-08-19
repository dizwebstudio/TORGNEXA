package diadoc

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type ft struct{}

func (ft) Ping(context.Context, []byte) error { return nil }
func (ft) Read(_ context.Context, _ []byte, id string) (RemoteDocument, error) {
	return RemoteDocument{RemoteID: id, Kind: "upd", Status: "delivered", ObservedAt: time.Now().UTC()}, nil
}
func (ft) Send(_ context.Context, _ []byte, q sdk.EDOSendRequest) (RemoteDocument, error) {
	return RemoteDocument{RemoteID: "msg:1", ExternalID: q.ExternalID, Kind: q.Kind, Status: "sent", SignatureRef: q.SignatureRef, MChDRef: q.MChDRef, ObservedAt: time.Now().UTC()}, nil
}
func (ft) RequestSigning(_ context.Context, _ []byte, q sdk.EDOSignWorkflowRequest) (sdk.EDOSignWorkflowResult, error) {
	return sdk.EDOSignWorkflowResult{WorkflowRef: "wf:1", Status: "requested", CreatedAt: time.Now().UTC()}, nil
}

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return sec{} }

type sec struct{}

func (sec) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn([]byte("x"))
}
func a() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "d", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "diadoc", Family: sdk.FamilyEDO, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestSignedSend(t *testing.T) {
	q := sdk.EDOSendRequest{ExternalID: "e1", Kind: "upd", CounterpartyRef: "cp:1", ArtifactRef: "art:1", SignatureRef: "sig:1", MChDRef: "mchd:1", ApprovalRef: "app:1", IdempotencyKey: "idem:1"}
	x, e := New(ft{}, nil).SendEDODocument(context.Background(), a(), rt{}, q)
	if e != nil || x.Status != "sent" {
		t.Fatalf("%+v %v", x, e)
	}
}
