package vetismercury

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type ft struct{}

func (ft) Ping(context.Context, []byte) error { return nil }
func (ft) Read(context.Context, []byte, sdk.GovernmentDocumentRequest) (sdk.GovernmentDocument, error) {
	return sdk.GovernmentDocument{RemoteID: "d1", Kind: "vetd", Status: "completed", ObservedAt: time.Now().UTC()}, nil
}
func (ft) Write(_ context.Context, _ []byte, q sdk.GovernmentWriteRequest) (sdk.GovernmentWriteResult, error) {
	return sdk.GovernmentWriteResult{RemoteID: "d2", Status: "accepted", AcceptedAt: time.Now().UTC()}, nil
}
func (ft) Inventory(context.Context, []byte, sdk.GovernmentInventoryRequest) (sdk.GovernmentInventoryObservation, error) {
	return sdk.GovernmentInventoryObservation{ObservedAt: time.Now().UTC()}, nil
}
func (ft) Reconcile(context.Context, []byte, sdk.GovernmentReconciliationRequest) (sdk.GovernmentReconciliationResult, error) {
	return sdk.GovernmentReconciliationResult{}, nil
}

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return sec{} }

type sec struct{}

func (sec) UseSecret(_ context.Context, _ sdk.SecretReference, f func([]byte) error) error {
	return f([]byte("x"))
}
func acc() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "v", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "vetis-mercury", Family: sdk.FamilyGovernment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestWriteRequiresApproval(t *testing.T) {
	q := sdk.GovernmentWriteRequest{Kind: "vetd", ExternalID: "e", ArtifactRef: "a", IdempotencyKey: "i"}
	if _, e := New(ft{}, nil).WriteGovernmentDocument(context.Background(), acc(), rt{}, q); e == nil {
		t.Fatal("approval bypass")
	}
	q.ApprovalRef = "approval:1"
	if _, e := New(ft{}, nil).WriteGovernmentDocument(context.Background(), acc(), rt{}, q); e != nil {
		t.Fatal(e)
	}
}
