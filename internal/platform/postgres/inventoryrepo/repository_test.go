package inventoryrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
)

func TestRepositoryWritesAuditAndOutboxInSameTransaction(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	b, e := os.ReadFile(filepath.Join(filepath.Dir(src), "repository.go"))
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, n := range []string{"auditrepo.AppendTransaction(ctx, tx", "outboxrepo.NewTransactionEnqueuer(tx)", "commerce.inventory.position_changed.v1", "ErrInsufficientAvailable"} {
		if !strings.Contains(s, n) {
			t.Fatalf("repository missing %q", n)
		}
	}
}

func TestWireQuantityMirrorsSharedPrimitive(t *testing.T) {
	d, err := inventory.ParseDecimal("12.345")
	if err != nil {
		t.Fatal(err)
	}
	u, err := inventory.NewUnitCode("KG")
	if err != nil {
		t.Fatal(err)
	}
	local, err := inventory.NewQuantity(d, u)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := wireQuantity(local)
	if err != nil {
		t.Fatal(err)
	}
	if wire.Value.String() != "12.345" || wire.Unit.String() != "KG" {
		t.Fatalf("unexpected wire quantity: %s %s", wire.Value.String(), wire.Unit.String())
	}
}
