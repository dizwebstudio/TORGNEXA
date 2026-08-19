package cloudbilling

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"testing"
	"time"
)

func sc(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestCommunityIsIndependentOfBilling(t *testing.T) {
	if RequireBilling(ModeCommunity) != ErrCommunityBypass {
		t.Fatal("community unexpectedly depends on billing")
	}
}
func TestUsageIdempotentAndInvoiceExact(t *testing.T) {
	st := NewStore()
	u := UsageRecord{ID: "u1", Meter: "api_call", SourceEventID: "evt1", Quantity: 3, OccurredAt: time.Now().UTC()}
	if e := st.RecordUsage(sc(t), u); e != nil {
		t.Fatal(e)
	}
	if e := st.RecordUsage(sc(t), u); e != nil {
		t.Fatal(e)
	}
	sub := Subscription{ID: "sub1", PlanID: "p", PlanVersion: 1, State: Active, CurrentPeriodStart: time.Now().UTC(), CurrentPeriodEnd: time.Now().UTC().Add(24 * time.Hour), UpdatedAt: time.Now().UTC(), Version: 1}
	rub, _ := domain.NewCurrency("RUB")
	base, _ := domain.NewMoney(1000, rub)
	inv, e := BuildInvoice("i1", sub, []UsageRecord{u}, base, 100)
	if e != nil || inv.Amount.MinorUnits() != 1300 {
		t.Fatalf("%+v %v", inv, e)
	}
}
func TestGraceLifecycle(t *testing.T) {
	if NextState(Active, false, false) != PastDue || NextState(PastDue, false, false) != Grace || NextState(Grace, false, true) != Suspended || NextState(Suspended, true, false) != Active {
		t.Fatal("bad lifecycle")
	}
}

type entSink struct{ got map[string]int64 }

func (e *entSink) Sync(_ tenancy.Scope, _ string, values map[string]int64, _ int64) error {
	e.got = values
	return nil
}
func TestAdjustmentIdempotencyPaymentAndEntitlements(t *testing.T) {
	st := NewStore()
	rub, _ := domain.NewCurrency("RUB")
	money, _ := domain.NewMoney(100, rub)
	now := time.Now().UTC()
	a := Adjustment{ID: "adj1", InvoiceID: "i1", Reason: "credit", Amount: money, CreatedAt: now}
	if err := st.RecordAdjustment(sc(t), a); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordAdjustment(sc(t), a); err != nil {
		t.Fatal(err)
	}
	inv := Invoice{ID: "i1", SubscriptionID: "s1", Amount: money, State: "issued", Version: 1}
	paid, err := ApplyPaymentObservation(inv, PaymentObservation{Reference: "pay1", Succeeded: true, ObservedAt: now})
	if err != nil || paid.State != "paid" {
		t.Fatalf("paid=%+v err=%v", paid, err)
	}
	plan := PlanVersion{PlanID: "p", Version: 1, Name: "Pro", MonthlyPrice: money, Entitlements: map[string]int64{"users": 5}, EffectiveAt: now}
	sub := Subscription{ID: "s1", PlanID: "p", PlanVersion: 1, State: Active, Version: 1}
	sink := &entSink{}
	if err := SyncEntitlements(sc(t), sub, plan, sink); err != nil {
		t.Fatal(err)
	}
	if sink.got["users"] != 5 {
		t.Fatalf("entitlements=%v", sink.got)
	}
}
