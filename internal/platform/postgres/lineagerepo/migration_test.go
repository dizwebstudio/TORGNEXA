package lineagerepo

import (
	"os"
	"strings"
	"testing"
)

func TestDataLineageMigrationGuardsEvidence(t *testing.T) {
	data, err := os.ReadFile("../../../../migrations_legacy_pre_v1/000014_data_lineage.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"CREATE TABLE lineage_records",
		"CREATE TABLE lineage_inputs",
		"FORCE ROW LEVEL SECURITY",
		"lineage audit evidence must belong to same tenant",
		"lineage event evidence must belong to same tenant",
		"lineage evidence is append-only",
		"lineage evidence cannot be cleared",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
