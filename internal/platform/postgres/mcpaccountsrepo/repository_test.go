package mcpaccountsrepo

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

type tokenAccountRow struct{ now time.Time }

func (row tokenAccountRow) Scan(dest ...any) error {
	if len(dest) != 14 {
		return fmt.Errorf("scan destinations = %d, want 14", len(dest))
	}
	*dest[0].(*string) = "account-1"
	*dest[1].(*string) = "Agent"
	*dest[2].(*string) = "agent-1"
	*dest[3].(*string) = "model-1"
	*dest[4].(*string) = "integration-1"
	*dest[5].(*[]byte) = []byte(`["commerce.products.read"]`)
	*dest[6].(*bool) = true
	*dest[7].(*int64) = 2
	*dest[8].(*time.Time) = row.now.Add(time.Hour)
	*dest[9].(*string) = "predecessor"
	*dest[10].(*sql.NullTime) = sql.NullTime{}
	*dest[11].(*time.Time) = row.now.Add(-time.Hour)
	*dest[12].(*time.Time) = row.now
	*dest[13].(*[]byte) = make([]byte, 32)
	return nil
}

func TestScanAccountWithTokenIncludesLifecycleColumns(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	account, hash, err := scanAccountWithToken(tokenAccountRow{now: now})
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != "account-1" || account.RotatedFromID != "predecessor" || account.ExpiresAt != now.Add(time.Hour) || len(account.Permissions) != 1 || len(hash) != 32 {
		t.Fatalf("unexpected account scan: %#v hash=%d", account, len(hash))
	}
}
