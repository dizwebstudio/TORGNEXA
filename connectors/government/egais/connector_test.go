package egais

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return candidateSecrets{} }
func acc() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "e", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "egais", Family: sdk.FamilyGovernment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestRegulatedWriteRequiresApprovalArtifactAndIdempotency(t *testing.T) {
	c := New(candidateTransport{}, nil)
	q := sdk.GovernmentWriteRequest{Kind: "waybill", ExternalID: "w1", ArtifactRef: "signed:1", IdempotencyKey: "idem:1"}
	if _, e := c.WriteGovernmentDocument(context.Background(), acc(), rt{}, q); e == nil {
		t.Fatal("approval bypass")
	}
	q.ApprovalRef = "approval:1"
	if _, e := c.WriteGovernmentDocument(context.Background(), acc(), rt{}, q); e != nil {
		t.Fatal(e)
	}
}
func TestUTMReadStatusAndReference(t *testing.T) {
	c := New(candidateTransport{}, nil)
	if _, e := c.ReadGovernmentDocument(context.Background(), acc(), rt{}, sdk.GovernmentDocumentRequest{RemoteID: "ticket:1"}); e != nil {
		t.Fatal(e)
	}
	if _, e := c.ReadGovernmentReference(context.Background(), acc(), rt{}, sdk.GovernmentReferenceRequest{Kind: "organization", Key: "fsrar:1"}); e != nil {
		t.Fatal(e)
	}
}
