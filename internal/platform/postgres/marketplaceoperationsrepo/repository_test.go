package marketplaceoperationsrepo

import (
	"database/sql"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplaceoperations"
)

type flowScanRow func(...any) error

func (row flowScanRow) Scan(dest ...any) error { return row(dest...) }

func TestScanFlowNormalizesDatabaseTimesAndReferences(t *testing.T) {
	local := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("local", 0))
	got, err := scanFlow(flowScanRow(func(dest ...any) error {
		*dest[0].(*string) = "flow-1"
		*dest[1].(*string) = "org-1"
		*dest[2].(*string) = "workspace-1"
		*dest[3].(*string) = "account-1"
		*dest[4].(*string) = "account"
		*dest[5].(*string) = "pending"
		*dest[6].(*int64) = 1
		*dest[7].(*string) = ""
		*dest[8].(*string) = ""
		*dest[9].(*string) = ""
		*dest[10].(*string) = ""
		*dest[11].(*[]byte) = []byte(`[{"kind":"account","id":"account-1"}]`)
		*dest[12].(*time.Time) = local
		*dest[13].(*time.Time) = local
		return nil
	}))
	if err != nil {
		t.Fatalf("scanFlow: %v", err)
	}
	if got.CreatedAt.Location() != time.UTC || got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("database timestamps were not normalized: created=%v updated=%v", got.CreatedAt.Location(), got.UpdatedAt.Location())
	}
	if len(got.References) != 1 || got.References[0].Kind != "account" {
		t.Fatalf("references were not decoded: %+v", got.References)
	}
}

func TestDecodeCursorRejectsMalformedValues(t *testing.T) {
	if _, err := decodeCursor("not-base64"); err == nil {
		t.Fatal("malformed cursor was accepted")
	}
	if _, err := scanFlow(flowScanRow(func(dest ...any) error { return sql.ErrNoRows })); err != marketplaceoperations.ErrFlowNotFound {
		// This branch is intentionally unreachable in normal use, but keeping
		// the scanner contract explicit prevents accidental sql.ErrNoRows leaks.
		t.Fatalf("unexpected missing-row error: %v", err)
	}
}
