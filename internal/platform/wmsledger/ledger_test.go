package wmsledger

import (
	"errors"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func s(t *testing.T) tenancy.Scope {
	x, e := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func TestConcurrentReservationsNeverOversell(t *testing.T) {
	l := NewMemoryLedger()
	sc := s(t)
	k := StockKey{"SKU", "loc", "", ""}
	now := time.Now().UTC()
	if err := l.Append(sc, Entry{"r", k, EntryReceive, 10, "asn", "receive-1", now}); err != nil {
		t.Fatal(err)
	}
	var ok int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := l.Reserve(sc, fmt.Sprintf("e%d", i), k, 1, "order", fmt.Sprintf("reserve-%d", i), now)
			if e == nil {
				atomic.AddInt64(&ok, 1)
			} else if !errors.Is(e, ErrInsufficient) {
				t.Errorf("%v", e)
			}
		}(i)
	}
	wg.Wait()
	b, _ := l.Balance(sc, k)
	if ok != 10 || b.Reserved != 10 || b.Available() != 0 {
		t.Fatalf("ok=%d balance=%+v", ok, b)
	}
}
func TestLedgerIsAppendOnlyAndIdempotent(t *testing.T) {
	l := NewMemoryLedger()
	sc := s(t)
	k := StockKey{"SKU", "loc", "lot", ""}
	now := time.Now().UTC()
	if err := l.RegisterLot(sc, Lot{ID: "lot", SKU: "SKU"}); err != nil {
		t.Fatal(err)
	}
	e := Entry{"r", k, EntryReceive, 5, "asn", "same", now}
	if err := l.Append(sc, e); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(sc, e); err != nil {
		t.Fatal(err)
	}
	if len(l.Entries(sc)) != 1 {
		t.Fatal("duplicate appended")
	}
}

func TestLocationValidationAndTenantIsolation(t *testing.T) {
	location := Location{ID: "loc-1", WarehouseID: "wh-1", Code: "A-01", Kind: LocationStorage, Active: true}
	if err := location.Validate(); err != nil {
		t.Fatalf("valid location rejected: %v", err)
	}
	other, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAX", "01ARZ3NDEKTSV4RRFFQ69G5FAY")
	ledger := NewMemoryLedger()
	now := time.Now().UTC()
	key := StockKey{SKU: "sku-tenant", LocationID: location.ID}
	if err := ledger.Append(s(t), Entry{ID: "e-tenant", Key: key, Kind: EntryReceive, Quantity: 1, IdempotencyKey: "idem-tenant", At: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Balance(other, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant balance error = %v, want ErrNotFound", err)
	}
}
