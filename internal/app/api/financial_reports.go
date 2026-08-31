package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/uniteconomics"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/financialrepo"
)

const financialReportsPath = ReportsPath + "/"

func newFinancialReportRoutes(repository *financialrepo.Repository, auditor *audit.Service) []ProtectedRoute {
	read := func(id string) ProtectedRoute {
		return ProtectedRoute{Method: http.MethodGet, Path: financialReportsPath + id, Permission: "finance.reports.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveFinancialReport(w, r, repository, auditor, id) })}
	}
	return []ProtectedRoute{
		read("seller_profit_and_loss"),
		read("seller_cash_flow"),
		read("seller_unit_economics"),
		read("seller_financial_quality"),
		{Method: http.MethodGet, Path: financialReportsPath + "seller_profit_and_loss/details", Permission: "finance.reports.detail.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveFinancialReport(w, r, repository, auditor, "seller_profit_and_loss_details")
		})},
		{Method: http.MethodPost, Path: ReportsPath + "/financial-runs", Permission: "finance.reports.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { createFinancialRun(w, r, repository) })},
	}
}

func serveFinancialReport(w http.ResponseWriter, r *http.Request, repository *financialrepo.Repository, auditor *audit.Service, id string) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	filter, err := parseFinancialFilter(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid financial report filters")
		return
	}
	data, err := repository.Report(r.Context(), scope, id, filter)
	if errors.Is(err, financialrepo.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Financial calculation is not available")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Financial report failed")
		return
	}
	switch r.URL.Query().Get("format") {
	case "csv":
		if !auditFinancialExport(r, scope, auditor, id, "csv", len(data.Rows)) {
			writeProblem(w, http.StatusServiceUnavailable, "Financial export audit unavailable")
			return
		}
		writeReportCSV(w, data)
	case "pdf":
		if !auditFinancialExport(r, scope, auditor, id, "pdf", len(data.Rows)) {
			writeProblem(w, http.StatusServiceUnavailable, "Financial export audit unavailable")
			return
		}
		if err := writeReportPDF(w, data); err != nil {
			writeProblem(w, http.StatusInternalServerError, "PDF generation failed")
		}
	default:
		writeJSON(w, http.StatusOK, data)
	}
}

func auditFinancialExport(r *http.Request, scope tenancy.Scope, auditor *audit.Service, reportID, format string, rows int) bool {
	if auditor == nil || r == nil || rows < 0 || rows > 200 {
		return false
	}
	principal, ok := PrincipalFromContext(r.Context())
	if !ok || principal.SubjectRef == "" {
		return false
	}
	_, err := auditor.Capture(r.Context(), scope, audit.Entry{
		ActorID:       principal.SubjectRef,
		Source:        "api.financial_reports",
		Action:        "financial_report.export",
		ResourceType:  "financial_report",
		ResourceID:    reportID,
		CorrelationID: r.Header.Get("X-Request-ID"),
		Risk:          audit.RiskRead,
		Summary: audit.Summary{
			"format": format,
			"rows":   rows,
			"run_id": strings.TrimSpace(r.URL.Query().Get("run_id")),
		},
	})
	return err == nil
}

func parseFinancialFilter(r *http.Request) (financialrepo.Filter, error) {
	q := r.URL.Query()
	filter := financialrepo.Filter{RunID: strings.TrimSpace(q.Get("run_id")), Basis: uniteconomics.Basis(strings.TrimSpace(q.Get("basis"))), ReportingCurrency: strings.ToUpper(strings.TrimSpace(q.Get("currency"))), ChannelRef: strings.ToLower(strings.TrimSpace(q.Get("channel_ref"))), SKU: strings.TrimSpace(q.Get("sku")), OrderID: strings.TrimSpace(q.Get("order_id")), Query: strings.TrimSpace(q.Get("q")), Cursor: strings.TrimSpace(q.Get("cursor")), Limit: 100}
	if raw := q.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return filter, errors.New("invalid limit")
		}
		filter.Limit = value
	}
	if len(filter.RunID) > 192 || len(filter.ReportingCurrency) > 3 || len(filter.ChannelRef) > 192 || len(filter.SKU) > 128 || len(filter.OrderID) > 192 || len(filter.Query) > 100 || len(filter.Cursor) > 128 {
		return filter, errors.New("filter too long")
	}
	if filter.Basis != "" && !filter.Basis.Valid() {
		return filter, errors.New("invalid basis")
	}
	if filter.Cursor != "" && (!strings.HasPrefix(filter.Cursor, "v1.") || len(filter.Cursor) > 128) {
		return filter, errors.New("invalid cursor")
	}
	if filter.ReportingCurrency != "" && !regexpCurrency(filter.ReportingCurrency) {
		return filter, errors.New("invalid currency")
	}
	if raw := q.Get("from"); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, err
		}
		filter.From = value.UTC()
	}
	if raw := q.Get("to"); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, err
		}
		filter.To = value.UTC()
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && !filter.To.After(filter.From) {
		return filter, errors.New("invalid range")
	}
	format := q.Get("format")
	if format != "" && format != "json" && format != "csv" && format != "pdf" {
		return filter, errors.New("invalid format")
	}
	return filter, nil
}

func regexpCurrency(value string) bool {
	return len(value) == 3 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z' && value[2] >= 'A' && value[2] <= 'Z'
}

type financialRunRequest struct {
	From              time.Time           `json:"from"`
	To                time.Time           `json:"to"`
	Basis             uniteconomics.Basis `json:"basis"`
	ReportingCurrency string              `json:"reporting_currency"`
}

func createFinancialRun(w http.ResponseWriter, r *http.Request, repository *financialrepo.Repository) {
	scope, ok := ScopeFromContext(r.Context())
	_, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input financialRunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid financial run request")
		return
	}
	input.ReportingCurrency = strings.ToUpper(strings.TrimSpace(input.ReportingCurrency))
	if input.Basis == "" || !input.Basis.Valid() || input.From.IsZero() || input.To.IsZero() || !input.To.After(input.From) || input.To.Sub(input.From) > 366*24*time.Hour {
		writeProblem(w, http.StatusBadRequest, "Invalid financial run period")
		return
	}
	run, err := repository.Calculate(r.Context(), scope, financialrepo.RunRequest{IdempotencyKey: key, From: input.From.UTC(), To: input.To.UTC(), Basis: input.Basis, ReportingCurrency: input.ReportingCurrency})
	if err != nil {
		if errors.Is(err, financialrepo.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Invalid financial run request")
		} else {
			writeProblem(w, http.StatusInternalServerError, "Financial calculation failed")
		}
		return
	}
	writeJSON(w, http.StatusCreated, run)
}
