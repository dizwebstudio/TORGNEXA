package reconciliationrepo

import (
	"database/sql"
	"testing"
	"time"
)

type reconciliationScanRow func(...any) error

func (row reconciliationScanRow) Scan(dest ...any) error { return row(dest...) }

func TestScanRunNormalizesDatabaseTimeToUTC(t *testing.T) {
	local := time.Date(2026, 8, 31, 12, 0, 0, 0, time.FixedZone("local", 0))
	got, err := scanRun(reconciliationScanRow(func(dest ...any) error {
		*dest[0].(*string) = "run-1"
		*dest[1].(*string) = "policy-1"
		*dest[2].(*string) = "incremental"
		*dest[3].(*sql.NullString) = sql.NullString{String: "test", Valid: true}
		*dest[4].(*string) = "running"
		*dest[5].(*string) = "cursor"
		*dest[6].(*int64) = 1
		*dest[7].(*int64) = 0
		*dest[8].(*int64) = 1
		*dest[9].(*time.Time) = local
		*dest[10].(*time.Time) = local
		*dest[11].(*sql.NullTime) = sql.NullTime{}
		return nil
	}))
	if err != nil {
		t.Fatalf("scanRun: %v", err)
	}
	if got.StartedAt.Location() != time.UTC || got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("database timestamps were not normalized to UTC: started=%v updated=%v", got.StartedAt.Location(), got.UpdatedAt.Location())
	}
}

func TestScanDriftNormalizesDatabaseTimeToUTC(t *testing.T) {
	local := time.Date(2026, 8, 31, 12, 0, 0, 0, time.FixedZone("local", 0))
	got, err := scanDrift(reconciliationScanRow(func(dest ...any) error {
		*dest[0].(*string) = "drift-1"
		*dest[1].(*string) = "run-1"
		*dest[2].(*string) = "policy-1"
		*dest[3].(*string) = "content_drift"
		*dest[4].(*sql.NullString) = sql.NullString{String: "local-1", Valid: true}
		*dest[5].(*sql.NullString) = sql.NullString{String: "remote-1", Valid: true}
		*dest[6].(*sql.NullString) = sql.NullString{String: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Valid: true}
		*dest[7].(*sql.NullString) = sql.NullString{String: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Valid: true}
		*dest[8].(*sql.NullString) = sql.NullString{String: "active", Valid: true}
		*dest[9].(*sql.NullString) = sql.NullString{String: "updated", Valid: true}
		*dest[10].(*int64) = 1
		*dest[11].(*sql.NullString) = sql.NullString{String: "revision-1", Valid: true}
		*dest[12].(*int) = 1
		*dest[13].(*int) = 1
		*dest[14].(*time.Time) = local
		*dest[15].(*string) = "open"
		*dest[16].(*string) = "approval"
		*dest[17].(*int64) = 1
		*dest[18].(*sql.NullTime) = sql.NullTime{}
		return nil
	}))
	if err != nil {
		t.Fatalf("scanDrift: %v", err)
	}
	if got.DetectedAt.Location() != time.UTC {
		t.Fatalf("database timestamp was not normalized to UTC: detected=%v", got.DetectedAt.Location())
	}
}
