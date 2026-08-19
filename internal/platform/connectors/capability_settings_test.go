package connectors

import (
	"errors"
	"testing"
)

func TestAccountCapabilitySettingsAreManifestBoundAndDefaultDeny(t *testing.T) {
	manifest := validMarketplaceManifest()
	settings, err := BuildAccountCapabilitySettings(manifest, []Capability{"orders.read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != len(manifest.Capabilities) || !CapabilityEnabled(settings, "orders.read") || CapabilityEnabled(settings, "products.write") {
		t.Fatalf("unexpected settings: %#v", settings)
	}
	for _, setting := range settings {
		if setting.Direction == CapabilityWrite && (!setting.ApprovalRequired || setting.Risk != CapabilityRiskWriteSensitive) {
			t.Fatalf("write capability is not approval classified: %#v", setting)
		}
	}
	if _, err = BuildAccountCapabilitySettings(manifest, []Capability{"payments.refund"}); !errors.Is(err, ErrInvalidCapabilitySettings) {
		t.Fatalf("undeclared capability accepted: %v", err)
	}
	if _, err = BuildAccountCapabilitySettings(manifest, []Capability{"orders.read", "orders.read"}); !errors.Is(err, ErrInvalidCapabilitySettings) {
		t.Fatalf("duplicate capability accepted: %v", err)
	}
}

func TestEveryCanonicalCapabilityHasFailClosedPolicyMetadata(t *testing.T) {
	for _, capability := range KnownCapabilities() {
		definition, ok := CapabilityDefinitionFor(capability)
		if !ok || !validCapabilityPolicy(definition) {
			t.Errorf("capability %q has invalid policy metadata: %#v", capability, definition)
		}
	}
}
