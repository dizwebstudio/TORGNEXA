package edo

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

type remote struct{ status string }

func (remote) Send(context.Context, tenancy.Scope, Document, string) (string, error) {
	return "remote-1", nil
}
func (r remote) Status(context.Context, tenancy.Scope, string) (string, time.Time, error) {
	return r.status, time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC), nil
}
func TestRemoteStatusAuthoritative(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	s := NewService(remote{status: "signed_by_counterparty"})
	d := Document{ID: "d1", AdapterID: "document-rail", AccountID: "a1", ExternalID: "e1", Kind: "upd", ArtifactRef: "artifact:1", SignatureRef: "sig:1", CounterpartyRef: "cp:1"}
	x, e := s.Send(context.Background(), sc, d, "idem")
	if e != nil || x.Status != "submitted" {
		t.Fatal(e)
	}
	x, e = s.Refresh(context.Background(), sc, "d1")
	if e != nil || x.Status != "signed_by_counterparty" {
		t.Fatalf("%+v %v", x, e)
	}
}
