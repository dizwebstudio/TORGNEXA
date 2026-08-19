package advertising

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"testing"
)

type fake struct{ calls int }

func (f *fake) Apply(context.Context, tenancy.Scope, Action) error { f.calls++; return nil }
func m(t *testing.T, n int64) domain.Money {
	c, _ := domain.NewCurrency("RUB")
	v, e := domain.NewMoney(n, c)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func sc(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func act(t *testing.T) Action {
	return Action{Campaign: Campaign{"c", "launch", StatusDraft, m(t, 1000), m(t, 5000), Attribution{"torgnexa", "paid", "launch"}, 1}, RequestedSpend: m(t, 800), DryRun: true}
}
func TestDryRunNeverWritesAndCarriesAttribution(t *testing.T) {
	f := &fake{}
	e := Engine{f, Limits{m(t, 2000), 10, 10, m(t, 500)}}
	p, err := e.Execute(context.Background(), sc(t), act(t))
	if !errors.Is(err, ErrApprovalRequired) || f.calls != 0 || p.Attribution.Source != "torgnexa" {
		t.Fatalf("p=%+v err=%v calls=%d", p, err, f.calls)
	}
	a := act(t)
	a.ApprovalRef = "apr"
	p, err = e.Execute(context.Background(), sc(t), a)
	if err != nil || !p.DryRun || f.calls != 0 {
		t.Fatalf("p=%+v err=%v calls=%d", p, err, f.calls)
	}
}
func TestBudgetLimitFailsClosed(t *testing.T) {
	a := act(t)
	a.RequestedSpend = m(t, 1200)
	_, err := Preview(a, Limits{m(t, 2000), 10, 10, m(t, 500)})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal(err)
	}
}
