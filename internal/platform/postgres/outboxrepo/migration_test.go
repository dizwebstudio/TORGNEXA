package outboxrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOutboxMigrationAddsLeasesRLSAndImmutableBody(t *testing.T) {
	t.Parallel()
	migration := strings.ToUpper(strings.Join(strings.Fields(readOutboxMigration(t)), " "))
	for _, fragment := range []string{
		"ADD COLUMN EVENT_ENVELOPE JSONB",
		"ADD COLUMN AVAILABLE_AT TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"ADD COLUMN LEASE_TOKEN TEXT",
		"CREATE POLICY OUTBOX_EVENTS_TENANT_SELECT ON OUTBOX_EVENTS FOR SELECT",
		"CREATE POLICY OUTBOX_EVENTS_TENANT_INSERT ON OUTBOX_EVENTS FOR INSERT",
		"CREATE POLICY OUTBOX_EVENTS_TENANT_UPDATE ON OUTBOX_EVENTS FOR UPDATE",
		"REVOKE DELETE, TRUNCATE ON OUTBOX_EVENTS FROM PUBLIC",
		"OUTBOX EVENT IDENTITY AND BODY ARE IMMUTABLE",
		"PUBLISHED OUTBOX EVENT IS IMMUTABLE",
		"CREATE TRIGGER OUTBOX_EVENTS_NO_DELETE",
		"CREATE TRIGGER OUTBOX_EVENTS_NO_CLEAR",
		"INSERT INTO MIGRATION_HISTORY",
	} {
		if !strings.Contains(migration, fragment) {
			t.Errorf("outbox migration missing %q", fragment)
		}
	}
}

func TestOutboxMigrationPreservesLegacyRowsDuringExpand(t *testing.T) {
	t.Parallel()
	migration := strings.ToUpper(strings.Join(strings.Fields(readOutboxMigration(t)), " "))
	if strings.Contains(migration, "EVENT_ENVELOPE JSONB NOT NULL") {
		t.Fatal("expand migration must not reject legacy rows/writers before backfill/contract")
	}
	if !strings.Contains(migration, "EVENT_ENVELOPE IS NOT NULL") {
		t.Fatal("new relay index/claim path must distinguish Task-008 rows from legacy rows")
	}
}

func readOutboxMigration(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve source")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000007_transactional_outbox.sql")
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from compile-time source location.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
