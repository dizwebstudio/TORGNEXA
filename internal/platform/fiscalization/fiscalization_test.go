package fiscalization

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"strings"
	"testing"
	"time"
)

type p struct{}

func (p) Create(_ context.Context, _ tenancy.Scope, r Request) (Status, error) {
	return Status{RequestID: r.ID, RemoteID: "fiscal-1", State: "accepted", ObservedAt: r.CreatedAt}, nil
}
func (p) Status(_ context.Context, _ tenancy.Scope, id string) (Status, error) {
	return Status{RemoteID: id, State: "fiscalized", FiscalDocumentRef: "fd:1", ObservedAt: time.Now().UTC()}, nil
}
func TestIdempotentRequestAndMarkingLink(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	rub, _ := domain.NewCurrency("RUB")
	m, _ := domain.NewMoney(10000, rub)
	r := Request{ID: "r1", ExternalRef: "order:1", IdempotencyKey: "i1", Kind: Sale, Total: m, Marking: []MarkingLink{{CodeFingerprint: strings.Repeat("a", 64), VerificationStatus: "verified"}}, CreatedAt: time.Now().UTC()}
	s := NewService(p{})
	a, e := s.Create(context.Background(), sc, r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Create(context.Background(), sc, r)
	if e != nil || a != b {
		t.Fatal("not idempotent")
	}
	x, e := s.Refresh(context.Background(), sc, a.RemoteID)
	if e != nil || x.State != "fiscalized" {
		t.Fatal(e)
	}
}
