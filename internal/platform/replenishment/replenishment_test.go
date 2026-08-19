package replenishment

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func TestRecommendationPinsSnapshotAndNeverAutoSends(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	s, err := BuildSnapshot("snap1", "reorder-v1", []Input{{"SKU1", "offer1", 3, 5, 4, 10, 2}}, now)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	repo := NewMemoryRepository()
	r, err := (Service{Repository: repo}).Generate(scope, s, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.Snapshot(scope, "snap1"); !ok {
		t.Fatal("snapshot not persisted")
	}
	if r[0].RecommendedUnits != 11 || r[0].AutoSendPO || r[0].SnapshotID != "snap1" || r[0].AlgorithmVersion != "reorder-v1" {
		t.Fatalf("%+v", r[0])
	}
}
