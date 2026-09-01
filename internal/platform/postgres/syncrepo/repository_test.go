package syncrepo

import (
	"testing"
	"time"
)

type syncScanRow func(...any) error

func (row syncScanRow) Scan(dest ...any) error { return row(dest...) }

func TestScanPolicyNormalizesDatabaseTimeToUTC(t *testing.T) {
	local := time.Date(2026, 8, 31, 12, 0, 0, 0, time.FixedZone("local", 0))
	got, err := scanPolicy(syncScanRow(func(dest ...any) error {
		*dest[0].(*string) = "policy-1"
		*dest[1].(*string) = "org-1"
		*dest[2].(*string) = "workspace-1"
		*dest[3].(*string) = "account-1"
		*dest[4].(*string) = "products"
		*dest[5].(*string) = "inbound"
		*dest[6].(*string) = "remote"
		*dest[7].(*bool) = true
		*dest[8].(*int64) = 1
		*dest[9].(*time.Time) = local
		*dest[10].(*time.Time) = local
		return nil
	}))
	if err != nil {
		t.Fatalf("scanPolicy: %v", err)
	}
	if got.CreatedAt.Location() != time.UTC || got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("database timestamps were not normalized to UTC: created=%v updated=%v", got.CreatedAt.Location(), got.UpdatedAt.Location())
	}
}

func TestValidSyncIDRejectsControlAndOversizedValues(t *testing.T) {
	for _, value := range []string{"policy-1", "evt:2026/09/01", "A_valid.id"} {
		if !validSyncID(value) {
			t.Fatalf("valid sync id %q was rejected", value)
		}
	}
	for _, value := range []string{"", "with space", "with\nnewline", "with?query", string(make([]byte, 129))} {
		if validSyncID(value) {
			t.Fatalf("invalid sync id %q was accepted", value)
		}
	}
}
