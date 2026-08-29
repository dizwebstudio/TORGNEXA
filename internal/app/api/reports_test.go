package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reportrepo"
)

type reportReaderStub struct{ data reportrepo.Data }

func (r reportReaderStub) Report(context.Context, tenancy.Scope, string, reportrepo.Filter) (reportrepo.Data, error) {
	return r.data, nil
}

func TestInventoryReportFallsBackToOperationalStock(t *testing.T) {
	primary := reportrepo.Data{ID: "inventory_current", Source: "clickhouse", Columns: []reportrepo.Column{{Key: "quantity", Label: "Количество"}}, Rows: [][]string{}}
	fallback := reportrepo.Data{ID: "inventory_current", Source: "postgresql", Columns: []reportrepo.Column{{Key: "sku", Label: "SKU"}}, Rows: [][]string{{"DEMO-SKU"}}}
	reader, err := newInventoryFallbackReportReader(reportReaderStub{data: primary}, reportReaderStub{data: fallback})
	if err != nil {
		t.Fatal(err)
	}
	data, err := reader.Report(context.Background(), validTestScope(t), "inventory_current", reportrepo.Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if data.Source != "postgresql" || len(data.Rows) != 1 || data.Rows[0][0] != "DEMO-SKU" {
		t.Fatalf("unexpected fallback report: %+v", data)
	}
}

func TestListReportsRequiresScopeAndReturnsCatalog(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, ReportsPath, nil)
	denied := httptest.NewRecorder()
	listReports(denied, request)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing scope status = %d", denied.Code)
	}

	scope := validTestScope(t)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	listReports(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"sales_daily"`) || !strings.Contains(response.Body.String(), `"id":"ingestion_freshness"`) || !strings.Contains(response.Body.String(), `"source":"clickhouse"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReportRouteGeneratesDownloadablePDF(t *testing.T) {
	data := reportrepo.Data{
		ID:          "sales_daily",
		GeneratedAt: time.Date(2026, 8, 16, 9, 30, 0, 0, time.UTC),
		Source:      "clickhouse",
		Columns:     []reportrepo.Column{{Key: "day", Label: "День"}, {Key: "currency", Label: "Валюта"}, {Key: "orders", Label: "Заказы"}},
		Rows:        [][]string{{"2026-08-15", "RUB", "12"}, {"2026-08-16", "RUB", "7"}},
	}
	route := newReportRoutes(reportReaderStub{data: data})[1]
	request := httptest.NewRequest(http.MethodGet, ReportsPath+"/sales_daily?format=pdf", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, validTestScope(t)))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("content type=%q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="sales_daily.pdf"` {
		t.Fatalf("content disposition=%q", got)
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) || !bytes.Contains(response.Body.Bytes(), []byte("%%EOF")) {
		t.Fatal("response is not a complete PDF document")
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("/ToUnicode")) {
		t.Fatal("PDF does not contain an embedded Unicode character map")
	}
	if response.Body.Len() > 2<<20 {
		t.Fatalf("PDF unexpectedly large: %d bytes", response.Body.Len())
	}
}

func TestReportFilterAcceptsPDFAndRejectsUnknownFormat(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, ReportsPath+"/sales_daily?format=pdf", nil)
	if _, err := parseReportFilter(request); err != nil {
		t.Fatalf("PDF format rejected: %v", err)
	}
	request = httptest.NewRequest(http.MethodGet, ReportsPath+"/sales_daily?format=docx", nil)
	if _, err := parseReportFilter(request); err == nil {
		t.Fatal("unknown report format accepted")
	}
}

func TestFormatPDFMinorUnitsUsesExactDecimalText(t *testing.T) {
	tests := map[string]string{
		"0":         "0,00 RUB",
		"5":         "0,05 RUB",
		"123456789": "1 234 567,89 RUB",
		"-42":       "-0,42 RUB",
	}
	for input, expected := range tests {
		if got := formatPDFMinorUnits(input, "RUB"); got != expected {
			t.Errorf("formatPDFMinorUnits(%q)=%q, want %q", input, got, expected)
		}
	}
}
