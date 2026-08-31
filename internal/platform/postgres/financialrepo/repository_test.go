package financialrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFinancialRowsUseBoundedCursorPagination(t *testing.T) {
	rows := [][]string{{"one"}, {"two"}, {"three"}}
	first, cursor := paginateFinancialRows(rows, Filter{Limit: 2})
	if len(first) != 2 || cursor == "" {
		t.Fatalf("first page = %#v, cursor = %q", first, cursor)
	}
	second, next := paginateFinancialRows(rows, Filter{Limit: 2, Cursor: cursor})
	if len(second) != 1 || second[0][0] != "three" || next != "" {
		t.Fatalf("second page = %#v, next cursor = %q", second, next)
	}
	if validFinancialCursor("v1.not-base64") {
		t.Fatal("malformed cursor accepted")
	}
}

func TestSellerFinancialAnalyticsMigrationIsExpandOnly(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(source), "..", "..", "..", "..")
	path := filepath.Join(root, "migrations", "000046_seller_financial_analytics.sql")
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from this repository test source.
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"SET LOCAL lock_timeout",
		"SET LOCAL statement_timeout",
		"CREATE TABLE financial_calculation_runs",
		"CREATE TABLE financial_calculation_snapshots",
		"CREATE TABLE financial_calculation_quality_issues",
		"CREATE TABLE financial_calculation_events",
		"FORCE ROW LEVEL SECURITY",
		"append-only",
		"COMMIT;",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(text), "DROP TABLE") {
		t.Fatal("financial analytics migration must remain expand-only")
	}
}
