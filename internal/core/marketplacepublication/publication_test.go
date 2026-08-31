package marketplacepublication

import (
	"testing"
	"time"
)

func testSnapshot(t *testing.T) Snapshot {
	t.Helper()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	return Snapshot{
		ID: "mps_01", Target: Target{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ProductID: "product-1", ConnectorAccountID: "account-1", ConnectorID: "marketplace", Locale: "ru-RU", Jurisdiction: "RU"},
		Version: 1, SKU: "SKU-1", Title: "Тестовый товар", CategoryCode: "category-1", PriceMinor: 10000, Currency: "RUB", ProductStatus: "active", CatalogVersion: 1, AssembledAt: now,
	}
}

func TestSnapshotDigestIsStableAndExcludesDigest(t *testing.T) {
	snapshot := testSnapshot(t)
	first, err := snapshot.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Digest = first
	second, err := snapshot.ComputeDigest()
	if err != nil || first != second {
		t.Fatalf("digest changed after persistence: %q %q %v", first, second, err)
	}
	if len(first) != 64 {
		t.Fatalf("unexpected digest: %q", first)
	}
}

func TestPublicationStateMachineRejectsSkippingPreflight(t *testing.T) {
	if CanTransition(StateDraft, StatePublished) {
		t.Fatal("draft must not become published without preflight and worker stages")
	}
	if !CanTransition(StateSending, StateUnknown) || !CanTransition(StateUnknown, StatePublished) {
		t.Fatal("unknown write outcome must be recoverable by reconciliation")
	}
}

func TestReconcileClassifiesSafeDrifts(t *testing.T) {
	snapshot := testSnapshot(t)
	digest, err := snapshot.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	observation := RemoteObservation{RemoteID: "remote-1", State: StatePublished, Moderation: ModerationApproved, SnapshotDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObservedAt: time.Date(2026, 8, 31, 12, 1, 0, 0, time.UTC)}
	drifts, err := Reconcile(snapshot, observation, "remote-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 2 || drifts[0].ExpectedDigest != digest {
		t.Fatalf("unexpected drifts: %#v", drifts)
	}
}

func TestMediaRejectsArbitraryURL(t *testing.T) {
	snapshot := testSnapshot(t)
	snapshot.Media = []MediaAsset{{ID: "image-1", ReleasedObjectRef: "https://example.invalid/image.jpg", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Format: "image/jpeg", Bytes: 10, Position: 0}}
	if snapshot.Validate() == nil {
		t.Fatal("arbitrary URL must not cross the publication boundary")
	}
}
