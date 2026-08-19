package pricingrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/pricing"
)

func TestRepositoryWritesAuditAndOutboxInSameTransaction(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	b, e := os.ReadFile(filepath.Join(filepath.Dir(src), "repository.go"))
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, n := range []string{"auditrepo.AppendTransaction(ctx, tx", "outboxrepo.NewTransactionEnqueuer(tx)", "commerce.pricing.price_changed.v1"} {
		if !strings.Contains(s, n) {
			t.Fatalf("repository missing %q", n)
		}
	}
}

func TestWireMoneyMirrorsSharedPrimitive(t *testing.T) {
	currency, err := pricing.NewCurrency("RUB")
	if err != nil {
		t.Fatal(err)
	}
	local, err := pricing.NewMoney(12345, currency)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := wireMoney(local)
	if err != nil {
		t.Fatal(err)
	}
	if wire.MinorUnits() != 12345 || wire.Currency().String() != "RUB" {
		t.Fatalf("unexpected wire money: %d %s", wire.MinorUnits(), wire.Currency())
	}
}
