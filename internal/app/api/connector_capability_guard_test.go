package api

import (
	"testing"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func TestSyncCapabilitiesAreProviderNeutralAndFailClosed(t *testing.T) {
	cases := []struct {
		family sdk.Family
		entity string
		read   sdk.Capability
		write  sdk.Capability
		ok     bool
	}{
		{sdk.FamilyMarketplace, "orders", "orders.read", "orders.status.write", true},
		{sdk.FamilyERP, "products", "erp.catalog.read", "erp.catalog.write", true},
		{sdk.FamilyClassified, "inventory", "", "", false},
		{sdk.FamilySocial, "orders", "", "", false},
	}
	for _, test := range cases {
		read, write, ok := sdk.RequiredSyncCapabilities(test.family, test.entity)
		if read != test.read || write != test.write || ok != test.ok {
			t.Fatalf("%s/%s: got %q %q %v", test.family, test.entity, read, write, ok)
		}
	}
}
