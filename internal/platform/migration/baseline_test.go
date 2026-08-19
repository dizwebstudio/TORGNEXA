package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type baselineManifest struct {
	SchemaVersion          int    `json:"schema_version"`
	BaselineMigrationCount int    `json:"baseline_migration_count"`
	LegacyHeadVersion      int    `json:"legacy_head_version"`
	LegacyMigrationCount   int    `json:"legacy_migration_count"`
	LegacyCatalogSHA256    string `json:"legacy_catalog_sha256"`
}

func TestPreV1BaselineManifestPinsLegacyHead(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	manifestBytes, err := os.ReadFile(filepath.Join(root, "migrations", "baseline-manifest.json")) // #nosec G304 -- repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var manifest baselineManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || manifest.BaselineMigrationCount != 11 || manifest.LegacyHeadVersion != 74 || manifest.LegacyMigrationCount != 74 {
		t.Fatalf("unexpected baseline manifest: %+v", manifest)
	}
	legacyCatalog, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "catalog.json")) // #nosec G304 -- repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(legacyCatalog)
	if got := hex.EncodeToString(digest[:]); got != manifest.LegacyCatalogSHA256 {
		t.Fatalf("legacy catalog digest = %s, want %s", got, manifest.LegacyCatalogSHA256)
	}
}

func TestActiveMigrationInventoryIsCompact(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(filepath.Join(testRepositoryRoot(t), "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			count++
		}
	}
	if count < 11 {
		t.Fatalf("active migration SQL count = %d, want at least 11", count)
	}
}
