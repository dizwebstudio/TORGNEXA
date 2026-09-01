package connectors

import "testing"

func TestReadinessCatalogIsCompleteAndRedacted(t *testing.T) {
	profiles, err := ReadinessCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 61 {
		t.Fatalf("profiles = %d, want 61", len(profiles))
	}
	snapshot, err := ReadinessSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Total != 61 || snapshot.Summary.HealthOnly != 16 || snapshot.Summary.ManifestOnly != 2 {
		t.Fatalf("unexpected summary: %+v", snapshot.Summary)
	}
	for _, profile := range profiles {
		if profile.Status == ReadinessQualified {
			t.Fatalf("repository fixture must not claim qualified without live evidence: %s", profile.ConnectorID)
		}
		for _, blocker := range profile.Blockers {
			if blocker == "" {
				t.Fatalf("empty blocker for %s", profile.ConnectorID)
			}
		}
	}
}

func TestAllowsRemoteOperationFailsClosed(t *testing.T) {
	profile := ReadinessProfile{Status: ReadinessReady, Capabilities: []ReadinessCapability{{Name: "products.read", Status: "ready"}}}
	if !AllowsRemoteOperation(profile, "products.read", false) {
		t.Fatal("ready read capability should be admitted to caller-owned policy gates")
	}
	if AllowsRemoteOperation(profile, "products.read", true) {
		t.Fatal("ready profile must not admit a write without qualification")
	}
	profile.Status = ReadinessQualified
	if !AllowsRemoteOperation(profile, "products.read", true) {
		t.Fatal("qualified write capability should be admitted")
	}
}

func TestReadinessProfileForReturnsDefensiveProfile(t *testing.T) {
	profile, err := ReadinessProfileFor("ozon")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != ReadinessReady || profile.ConnectorID != "ozon" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	profile.Blockers = append(profile.Blockers, "test-only")
	again, err := ReadinessProfileFor("ozon")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Blockers) == len(profile.Blockers) {
		t.Fatal("profile slices are not defensive")
	}
}
