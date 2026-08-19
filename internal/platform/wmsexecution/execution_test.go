package wmsexecution

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func TestScannerCommandIsIdempotentAndCompletes(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	svc := NewService()
	now := time.Now().UTC()
	task := Task{"t1", TaskPick, StatePending, "wh", "loc1", "loc2", "SKU", 2, 0, 1, now}
	if e := svc.Create(sc, task); e != nil {
		t.Fatal(e)
	}
	cmd := ScanCommand{"t1", "460123", "PICK-01", "idem-1", 1, now}
	got, e := svc.Scan(sc, cmd)
	if e != nil {
		t.Fatal(e)
	}
	if got.ProcessedQuantity != 1 || got.State != StateInProgress {
		t.Fatal(got)
	}
	got, e = svc.Scan(sc, cmd)
	if e != nil || got.ProcessedQuantity != 1 || len(svc.Events(sc)) != 1 {
		t.Fatalf("got=%+v e=%v events=%d", got, e, len(svc.Events(sc)))
	}
	cmd.IdempotencyKey = "idem-2"
	got, e = svc.Scan(sc, cmd)
	if e != nil || got.State != StateCompleted || got.ProcessedQuantity != 2 {
		t.Fatalf("got=%+v e=%v", got, e)
	}
}
