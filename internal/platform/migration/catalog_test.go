package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const safeMigrationSQL = `BEGIN;
SET LOCAL lock_timeout = '1s';
SET LOCAL statement_timeout = '10s';
CREATE TABLE synthetic_table (id text PRIMARY KEY);
COMMIT;
`

func TestRepositoryCatalogPasses(t *testing.T) {
	t.Parallel()
	catalog, err := LoadCatalog(context.Background(), testRepositoryRoot(t))
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	expected := []string{
		"platform", "tenancy", "migration_framework", "security_eventing", "commerce_core",
		"operations_foundation", "commerce_extensions", "regulated_integrations", "control_plane",
		"legacy_contract", "runtime_operations", "ai_advisory", "ai_provider_credential_class", "mcp_client_accounts",
		"ai_provider_qwen_deepseek", "trust_control_plane", "social_publication_runtime", "payments_core", "payments_remote_id_lookup", "ai_provider_claude", "ai_provider_local_runtime", "ai_provider_gemini_grok", "offline_demo_product_images", "user_profiles",
		"upload_security_pg18_compat", "audit_realtime_lookup_index", "workflow_automation", "payment_reconciliation_worker", "returns_cancellations_refunds", "wms_operator_tasks", "wms_task_batches", "replenishment_forecast_planning", "product_publication_quality", "channel_unit_economics", "integration_state_center", "logistics_shipment_payload_reference",
		"marking_execution",
		"ai_operator_assistant",
		"logistics_webhook_evidence",
		"return_logistics_operations",
		"return_logistics_tariff_code",
		"social_publication_buttons",
		"operator_assistant_runtime",
		"marketplace_product_publication",
		"supplier_procurement_operations",
		"seller_financial_analytics",
		"marketplace_advertising_runtime",
		"marketplace_operations_runtime",
		"marketplace_operations_findings",
		"replenishment_runtime",
		"marketplace_order_fulfillment",
	}
	if len(catalog.Migrations) != len(expected) {
		t.Fatalf("catalog migration count = %d, want %d", len(catalog.Migrations), len(expected))
	}
	for i, want := range expected {
		if got := catalog.Migrations[i].Name; got != want {
			t.Fatalf("catalog migration %d name = %q, want %q", i+1, got, want)
		}
	}
}

func TestCatalogRejectsUnsafeMetadataAndSQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		sql       string
		mutate    func(*Catalog)
		postWrite func(*testing.T, string)
		want      string
	}{
		{
			name: "checksum drift",
			mutate: func(catalog *Catalog) {
				catalog.Migrations[0].SHA256 = strings.Repeat("0", 64)
			},
			want: "checksum drift",
		},
		{
			name: "missing timeout",
			sql:  "BEGIN;\nCREATE TABLE synthetic_table (id text PRIMARY KEY);\nCOMMIT;\n",
			want: "missing set local lock_timeout",
		},
		{
			name: "destructive expand",
			sql:  "BEGIN;\nSET LOCAL lock_timeout='1s';\nSET LOCAL statement_timeout='10s';\nDROP TABLE synthetic_old;\nCOMMIT;\n",
			want: "destructive SQL requires a contract phase",
		},
		{
			name: "truncate remains destructive",
			sql:  "BEGIN;\nSET LOCAL lock_timeout='1s';\nSET LOCAL statement_timeout='10s';\nTRUNCATE synthetic_old;\nCOMMIT;\n",
			want: "destructive SQL requires a contract phase",
		},
		{
			name: "arbitrary execute remains forbidden",
			sql:  "BEGIN;\nSET LOCAL lock_timeout='1s';\nSET LOCAL statement_timeout='10s';\nEXECUTE synthetic_statement;\nCOMMIT;\n",
			want: "forbidden SQL construct",
		},
		{
			name: "high risk without backup",
			mutate: func(catalog *Catalog) {
				catalog.Migrations[0].Risk = RiskHigh
			},
			want: "requires a backup checkpoint",
		},
		{
			name: "migrate without backfill",
			mutate: func(catalog *Catalog) {
				catalog.Migrations[0].Phase = PhaseMigrate
			},
			want: "migrate requires a backfill plan",
		},
		{
			name: "contract without gates",
			mutate: func(catalog *Catalog) {
				migration := &catalog.Migrations[0]
				migration.Phase = PhaseContract
				migration.RequiresBackup = true
				migration.Compatibility = Compatibility{}
			},
			want: "contract requires backup and completion preconditions",
		},
		{
			name: "unregistered SQL",
			postWrite: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "migrations", "000002_extra.sql"), []byte(safeMigrationSQL), 0o600); err != nil {
					t.Fatalf("write extra migration: %v", err)
				}
			},
			want: "SQL inventory",
		},
		{
			name: "symlink",
			postWrite: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Symlink("000001_synthetic.sql", filepath.Join(root, "migrations", "000002_link.sql")); err != nil {
					t.Fatalf("create symlink: %v", err)
				}
			},
			want: "symlink is forbidden",
		},
		{
			name: "dynamic SQL",
			sql:  "BEGIN;\nSET LOCAL lock_timeout='1s';\nSET LOCAL statement_timeout='10s';\nDO $$ BEGIN EXECUTE 'SELECT 1'; END $$;\nCOMMIT;\n",
			want: "dollar-quoted/dynamic blocks are forbidden",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sql := test.sql
			if sql == "" {
				sql = safeMigrationSQL
			}
			root := writeCatalogFixture(t, sql, test.mutate)
			if test.postWrite != nil {
				test.postWrite(t, root)
			}
			_, err := LoadCatalog(context.Background(), root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadCatalog() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCatalogStrictJSONAndCancellation(t *testing.T) {
	t.Parallel()
	root := writeCatalogFixture(t, safeMigrationSQL, nil)
	catalogPath := filepath.Join(root, "migrations", "catalog.json")
	// #nosec G304 -- catalogPath is constructed beneath this test's private t.TempDir.
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"schema_version":1`, `"schema_version":1,"unexpected":true`, 1))
	// #nosec G703 -- catalogPath is constructed beneath this test's private t.TempDir.
	if err := os.WriteFile(catalogPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(context.Background(), root); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}

	duplicateRoot := writeCatalogFixture(t, safeMigrationSQL, nil)
	duplicatePath := filepath.Join(duplicateRoot, "migrations", "catalog.json")
	// #nosec G304 -- duplicatePath is constructed beneath this test's private t.TempDir.
	duplicateData, err := os.ReadFile(duplicatePath)
	if err != nil {
		t.Fatal(err)
	}
	duplicateData = []byte(strings.Replace(string(duplicateData), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1))
	// #nosec G703 -- duplicatePath is constructed beneath this test's private t.TempDir.
	if err := os.WriteFile(duplicatePath, duplicateData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(context.Background(), duplicateRoot); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("duplicate-key error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadCatalog(canceled, testRepositoryRoot(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled LoadCatalog() error = %v", err)
	}
}

func TestPlanRejectsDriftDowngradeAndGaps(t *testing.T) {
	t.Parallel()
	catalog, err := LoadCatalog(context.Background(), testRepositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	applied := make([]AppliedMigration, len(catalog.Migrations)-1)
	for index := range applied {
		migration := catalog.Migrations[index]
		applied[index] = AppliedMigration{Version: migration.Version, Name: migration.Name, SHA256: migration.SHA256}
	}
	pending, err := Plan(catalog, applied)
	if err != nil || len(pending) != 1 || pending[0].Version != catalog.Migrations[len(catalog.Migrations)-1].Version {
		t.Fatalf("Plan() = %#v, %v", pending, err)
	}

	drift := append([]AppliedMigration(nil), applied...)
	drift[1].SHA256 = strings.Repeat("0", 64)
	if _, err := Plan(catalog, drift); !errors.Is(err, ErrChecksumDrift) {
		t.Fatalf("checksum drift error = %v", err)
	}
	if _, err := Plan(catalog, []AppliedMigration{{Version: 2}}); !errors.Is(err, ErrHistoryGap) {
		t.Fatalf("history gap error = %v", err)
	}
	last := catalog.Migrations[len(catalog.Migrations)-1]
	newer := append(applied, AppliedMigration{Version: last.Version, Name: last.Name, SHA256: last.SHA256}, AppliedMigration{Version: last.Version + 1})
	if _, err := Plan(catalog, newer); !errors.Is(err, ErrUnknownAppliedMigration) {
		t.Fatalf("newer database error = %v", err)
	}
}

func writeCatalogFixture(t *testing.T, sql string, mutate func(*Catalog)) string {
	t.Helper()
	root := t.TempDir()
	migrationsDir := filepath.Join(root, "migrations")
	if err := os.Mkdir(migrationsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(sql))
	catalog := Catalog{
		SchemaVersion: 1,
		Migrations: []Migration{{
			Version:        1,
			Name:           "synthetic",
			File:           "000001_synthetic.sql",
			Phase:          PhaseExpand,
			Risk:           RiskLow,
			Transaction:    "embedded",
			Policy:         "v1",
			HistoryMode:    "bootstrap",
			RequiresBackup: false,
			Dependencies:   []int{},
			Compatibility: Compatibility{
				OldReaders:            true,
				OldWriters:            true,
				NewBinaryOnOldSchema:  true,
				ContractPreconditions: []string{},
			},
			SHA256: hex.EncodeToString(digest[:]),
		}},
	}
	if mutate != nil {
		mutate(&catalog)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "catalog.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "000001_synthetic.sql"), []byte(sql), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}
