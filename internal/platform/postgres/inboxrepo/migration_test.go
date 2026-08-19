package inboxrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInboxMigrationDefinesImmutableTenantScopedReceipts(t *testing.T) {
	t.Parallel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000008_inbox_idempotency.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(string(data))
	for _, required := range []string{
		"CREATE TABLE INBOX_RECEIPTS",
		"PRIMARY KEY (ORGANIZATION_ID, WORKSPACE_ID, CONSUMER, EVENT_ID)",
		"REFERENCES WORKSPACES (ORGANIZATION_ID, ID) ON DELETE RESTRICT",
		"ALTER TABLE INBOX_RECEIPTS ENABLE ROW LEVEL SECURITY",
		"ALTER TABLE INBOX_RECEIPTS FORCE ROW LEVEL SECURITY",
		"CREATE POLICY INBOX_RECEIPTS_TENANT_SELECT",
		"CREATE POLICY INBOX_RECEIPTS_TENANT_INSERT",
		"REVOKE UPDATE, DELETE, TRUNCATE ON INBOX_RECEIPTS FROM PUBLIC",
		"INBOX_RECEIPTS_NO_UPDATE",
		"INBOX_RECEIPTS_NO_DELETE",
		"INBOX_RECEIPTS_NO_CLEAR",
		"EVENT_FINGERPRINT ~ '^[0-9A-F]{64}$'",
		"PROCESSED_ATTEMPT BETWEEN 1 AND 1000",
		"INSERT INTO MIGRATION_HISTORY",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	if strings.Contains(sql, "CREATE POLICY INBOX_EVENTS_") {
		t.Fatal("legacy placeholder inbox_events must remain deny-all during expand phase")
	}
}

func TestLegacyInboxRetirementFailsClosedUnlessPlaceholderIsEmpty(t *testing.T) {
	t.Parallel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000064_retire_legacy_inbox_events.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(strings.Fields(string(data)), " "))
	for _, required := range []string{
		"LOCK TABLE INBOX_EVENTS IN ACCESS EXCLUSIVE MODE",
		"CHECK (FALSE) NOT VALID",
		"VALIDATE CONSTRAINT INBOX_EVENTS_RETIREMENT_EMPTY_CHK",
		"DROP TABLE INBOX_EVENTS",
		"INSERT INTO MIGRATION_HISTORY",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("legacy inbox retirement migration missing %q", required)
		}
	}
	if strings.Index(sql, "VALIDATE CONSTRAINT INBOX_EVENTS_RETIREMENT_EMPTY_CHK") > strings.Index(sql, "DROP TABLE INBOX_EVENTS") {
		t.Fatal("legacy inbox table is dropped before the empty-table guard is validated")
	}
}
