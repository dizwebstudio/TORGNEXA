package connectorsandbox

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type sequenceResolver struct {
	calls  int
	values [][]netip.Addr
}

func (r *sequenceResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	index := r.calls
	if index >= len(r.values) {
		index = len(r.values) - 1
	}
	r.calls++
	return append([]netip.Addr(nil), r.values[index]...), nil
}

func TestEgressRejectsPrivateSpecialAndDNSRebinding(t *testing.T) {
	plan := testPlan()
	destination := plan.Granted.Network[0]
	invalid := []string{"127.0.0.1", "10.1.2.3", "100.64.0.1", "169.254.1.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "::1", "fc00::1", "2001:db8::1"}
	for _, raw := range invalid {
		guard := EgressGuard{Resolver: fixedResolver{values: []netip.Addr{netip.MustParseAddr(raw)}}}
		if _, err := guard.Plan(context.Background(), plan, destination); !errors.Is(err, ErrEgressDenied) {
			t.Fatalf("%s should be denied, got %v", raw, err)
		}
	}
	resolver := &sequenceResolver{values: [][]netip.Addr{{netip.MustParseAddr("93.184.216.34")}, {netip.MustParseAddr("127.0.0.1")}}}
	guard := EgressGuard{Resolver: resolver}
	if _, err := guard.Plan(context.Background(), plan, destination); err != nil {
		t.Fatalf("first public resolution: %v", err)
	}
	if _, err := guard.Plan(context.Background(), plan, destination); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("rebinding to private IP must fail, got %v", err)
	}
}

func TestEgressRequiresExactGrantedDestination(t *testing.T) {
	plan := testPlan()
	guard := EgressGuard{Resolver: fixedResolver{values: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}
	changed := plan.Granted.Network[0]
	changed.Port = 8443
	if _, err := guard.Plan(context.Background(), plan, changed); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("ungranted port should fail, got %v", err)
	}
}
