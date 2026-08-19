package connectors

import "testing"

func TestRequiredSyncCapabilitiesRemainProviderNeutral(t *testing.T) {
	t.Parallel()
	read, write, ok := RequiredSyncCapabilities(FamilyMarketplace, "orders")
	if !ok || read != "orders.read" || write != "orders.status.write" {
		t.Fatalf("orders capabilities = %q %q %v", read, write, ok)
	}
	if _, _, ok = RequiredSyncCapabilities(FamilySocial, "orders"); ok {
		t.Fatal("unsupported social order sync admitted")
	}
}
