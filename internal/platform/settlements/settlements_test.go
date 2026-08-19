package settlements

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func TestImportIsAppendOnlyIdempotentAndAdjustmentDoesNotRewrite(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	st := NewMemoryStore()
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	raw := []byte(`[{"id":"e1","provider":"market-source","provider_account_id":"a1","provider_entry_ref":"r1","order_id":"o1","adjusts_entry_id":"","fee_code":"","fx_rate_ref":"","kind":"sale","minor_units":10000,"currency":"RUB","occurred_at":"2026-08-11T10:00:00Z","disputed":false}]`)
	if n, e := ImportJSON(context.Background(), st, sc, raw, now); e != nil || n != 1 {
		t.Fatalf("n=%d e=%v", n, e)
	}
	if n, e := ImportJSON(context.Background(), st, sc, raw, now); e != nil || n != 1 {
		t.Fatalf("retry n=%d e=%v", n, e)
	}
	adj := []byte(`[{"id":"e2","provider":"market-source","provider_account_id":"a1","provider_entry_ref":"r2","order_id":"o1","adjusts_entry_id":"e1","fee_code":"correction","fx_rate_ref":"","kind":"adjustment","minor_units":-500,"currency":"RUB","occurred_at":"2026-08-12T00:00:00Z","disputed":false}]`)
	if _, e := ImportJSON(context.Background(), st, sc, adj, now); e != nil {
		t.Fatal(e)
	}
	rows := st.List(context.Background(), sc)
	if len(rows) != 2 || rows[0].Amount.MinorUnits() != 10000 || rows[1].AdjustsEntryID != "e1" {
		t.Fatalf("%+v", rows)
	}
}
