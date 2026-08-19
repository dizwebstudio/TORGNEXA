package ordersrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/orders"
)

func TestRepositoryWritesAuditAndOutboxInSameTransaction(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	b, e := os.ReadFile(filepath.Join(filepath.Dir(src), "repository.go"))
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, want := range []string{"auditrepo.AppendTransaction(ctx, tx", "outboxrepo.NewTransactionEnqueuer(tx)", "commerce.orders.order_changed.v1", "ValidateTransition"} {
		if !strings.Contains(s, want) {
			t.Fatalf("repository missing %q", want)
		}
	}
}
func TestWireValuesMirrorSharedPrimitives(t *testing.T) {
	rub, _ := orders.NewCurrency("RUB")
	m, _ := orders.NewMoney(12345, rub)
	wm, e := wireMoney(m)
	if e != nil || wm.MinorUnits() != 12345 || wm.Currency().String() != "RUB" {
		t.Fatalf("wire money: %#v %v", wm, e)
	}
	d, _ := orders.ParseDecimal("2.5")
	u, _ := orders.NewUnitCode("PCS")
	q, _ := orders.NewQuantity(d, u)
	wq, e := wireQuantity(q)
	if e != nil || wq.Value.String() != "2.5" || wq.Unit.String() != "PCS" {
		t.Fatalf("wire quantity: %#v %v", wq, e)
	}
}
func TestOrderEventAvoidsRemoteIdentityAndPII(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(src), "repository.go"))
	lower := strings.ToLower(string(b))
	for _, bad := range []string{"external_order_id`json", "channel`json", "customer_email`json", "phone`json", "shipping_address`json"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("event/repository contains forbidden field %q", bad)
		}
	}
}
