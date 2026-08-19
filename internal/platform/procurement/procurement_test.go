package procurement

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func TestImportAndPOStateMachineAudit(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	raw := []byte(`{"id":"po1","supplier_id":"sup1","currency":"RUB","lines":[{"id":"l1","offer_id":"o1","sku":"SKU1","quantity":"2","unit":"PCS","unit_price_minor":1000}]}`)
	po, err := ParseImport(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	calls := 0
	s := Service{Audit: func(tenancy.Scope, AuditRecord) error { calls++; return nil }}
	for _, st := range []POStatus{POApproved, POSent, POPartiallyReceived, POReceived} {
		po, err = s.ChangeStatus(sc, po, st, now.Add(time.Duration(calls+1)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	if po.Status != POReceived || calls != 4 {
		t.Fatalf("po=%+v calls=%d", po, calls)
	}
	if _, err := Transition(po, POSent, now); err == nil {
		t.Fatal("terminal PO regressed")
	}
}
