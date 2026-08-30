package api

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reportrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reporting"
)

const ReportsPath = "/api/v1/reports"

type reportDefinition struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Source              string   `json:"source"`
	Availability        string   `json:"availability"`
	FreshnessSLASeconds int      `json:"freshness_sla_seconds"`
	Dimensions          []string `json:"dimensions"`
	Metrics             []string `json:"metrics"`
}

type reportReader interface {
	Report(context.Context, tenancy.Scope, string, reportrepo.Filter) (reportrepo.Data, error)
}

type clickHouseReportReader struct{ queries *reporting.QueryService }

// inventoryFallbackReportReader keeps the current-stock report useful while
// the disposable ClickHouse projection is empty or temporarily unavailable.
// PostgreSQL remains the operational source of truth for current inventory;
// event-backed ClickHouse rows are preferred whenever they are available.
type inventoryFallbackReportReader struct {
	primary  reportReader
	fallback reportReader
}

func newClickHouseReportReader(queries *reporting.QueryService) (reportReader, error) {
	if queries == nil {
		return nil, errors.New("reporting query service is required")
	}
	return &clickHouseReportReader{queries: queries}, nil
}

func newInventoryFallbackReportReader(primary, fallback reportReader) (reportReader, error) {
	if primary == nil {
		return nil, errors.New("primary report reader is required")
	}
	return &inventoryFallbackReportReader{primary: primary, fallback: fallback}, nil
}

func (r *inventoryFallbackReportReader) Report(ctx context.Context, scope tenancy.Scope, id string, filter reportrepo.Filter) (reportrepo.Data, error) {
	data, primaryErr := r.primary.Report(ctx, scope, id, filter)
	// PostgreSQL is also the truthful fallback for newly introduced factual
	// reports whose disposable ClickHouse projection has not been built yet.
	if r.fallback == nil || (primaryErr == nil && len(data.Rows) > 0) {
		return data, primaryErr
	}
	if fallbackData, fallbackErr := r.fallback.Report(ctx, scope, id, filter); fallbackErr == nil {
		return fallbackData, nil
	}
	if primaryErr != nil {
		return reportrepo.Data{}, primaryErr
	}
	return data, nil
}

func (r *clickHouseReportReader) Report(ctx context.Context, scope tenancy.Scope, id string, filter reportrepo.Filter) (reportrepo.Data, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 100
	}
	data := reportrepo.Data{ID: id, GeneratedAt: time.Now().UTC(), Source: "clickhouse", Rows: make([][]string, 0)}
	switch id {
	case "sales_daily":
		to := filter.To
		if to.IsZero() {
			to = time.Now().UTC().Add(24 * time.Hour).Truncate(24 * time.Hour)
		}
		from := filter.From
		if from.IsZero() {
			from = to.Add(-366 * 24 * time.Hour)
		}
		rows, err := r.queries.Sales(ctx, scope, reporting.SalesQuery{From: from.UTC(), To: to.UTC(), Limit: limit})
		if err != nil {
			return reportrepo.Data{}, err
		}
		data.Columns = []reportrepo.Column{{Key: "day", Label: "День"}, {Key: "currency", Label: "Валюта"}, {Key: "orders", Label: "Заказы"}, {Key: "fulfilled", Label: "Выполнены"}, {Key: "cancelled", Label: "Отменены"}, {Key: "gross_minor_units", Label: "Оборот"}}
		for _, row := range rows {
			if filter.Currency != "" && row.Currency != filter.Currency {
				continue
			}
			data.Rows = append(data.Rows, []string{row.Day.Format("2006-01-02"), row.Currency, strconv.FormatUint(row.Orders, 10), strconv.FormatUint(row.FulfilledOrders, 10), strconv.FormatUint(row.CancelledOrders, 10), strconv.FormatInt(row.GrossMinorUnits, 10)})
		}
	case "inventory_current":
		rows, err := r.queries.Inventory(ctx, scope, reporting.InventoryQuery{OfferID: filter.Query, Limit: limit})
		if err != nil {
			return reportrepo.Data{}, err
		}
		data.Columns = []reportrepo.Column{{Key: "offer_id", Label: "Предложение"}, {Key: "warehouse_id", Label: "Склад"}, {Key: "quantity", Label: "Количество"}, {Key: "changed_at", Label: "Обновлено"}}
		for _, row := range rows {
			data.Rows = append(data.Rows, []string{row.OfferID, row.WarehouseID, row.Quantity, row.ChangedAt.Format(time.RFC3339Nano)})
		}
	case "ingestion_freshness":
		rows, err := r.queries.Freshness(ctx, scope)
		if err != nil {
			return reportrepo.Data{}, err
		}
		data.Columns = []reportrepo.Column{{Key: "event_family", Label: "Семейство событий"}, {Key: "events", Label: "События"}, {Key: "last_occurred_at", Label: "Последнее событие"}, {Key: "last_ingested_at", Label: "Последняя загрузка"}, {Key: "source_lag_seconds", Label: "Задержка, сек."}}
		for _, row := range rows {
			if filter.Query != "" && !strings.Contains(row.EventFamily, filter.Query) {
				continue
			}
			if len(data.Rows) == limit {
				break
			}
			data.Rows = append(data.Rows, []string{row.EventFamily, strconv.FormatUint(row.EventCount, 10), row.LastOccurredAt.Format(time.RFC3339Nano), row.LastIngestedAt.Format(time.RFC3339Nano), strconv.FormatInt(int64(row.SourceLag/time.Second), 10)})
		}
	default:
		return reportrepo.Data{}, sql.ErrNoRows
	}
	return data, nil
}

func newReportRoutes(repository reportReader) []ProtectedRoute {
	return []ProtectedRoute{{Method: http.MethodGet, Path: ReportsPath, Permission: "reports.read", Handler: http.HandlerFunc(listReports)}, {Method: http.MethodGet, Path: ReportsPath + "/", PathPrefix: true, Permission: "reports.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		if !ok {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, ReportsPath+"/")
		if id == "" || strings.Contains(id, "/") {
			writeProblem(w, http.StatusNotFound, "Not Found")
			return
		}
		filter, err := parseReportFilter(r)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Invalid report filters")
			return
		}
		data, err := repository.Report(r.Context(), scope, id, filter)
		if errors.Is(err, sql.ErrNoRows) {
			writeProblem(w, http.StatusNotFound, "Not Found")
			return
		}
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		switch r.URL.Query().Get("format") {
		case "csv":
			writeReportCSV(w, data)
			return
		case "pdf":
			if err := writeReportPDF(w, data); err != nil {
				writeProblem(w, http.StatusInternalServerError, "PDF generation failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, data)
	})}}
}

func parseReportFilter(r *http.Request) (reportrepo.Filter, error) {
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status == "" {
		status = strings.TrimSpace(q.Get("completeness"))
	}
	filter := reportrepo.Filter{Query: strings.TrimSpace(q.Get("q")), Currency: strings.ToUpper(strings.TrimSpace(q.Get("currency"))), Status: status, Basis: strings.TrimSpace(q.Get("basis")), ChannelRef: strings.ToLower(strings.TrimSpace(q.Get("channel_ref"))), Limit: 100}
	if len(filter.Query) > 100 || len(filter.Currency) > 3 || len(filter.Status) > 32 {
		return filter, errors.New("filter too long")
	}
	if filter.Basis != "" && filter.Basis != "order_accrual" && filter.Basis != "settlement" && filter.Basis != "cash" {
		return filter, errors.New("invalid basis")
	}
	if len(filter.ChannelRef) > 192 {
		return filter, errors.New("channel_ref too long")
	}
	if raw := q.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return filter, errors.New("invalid limit")
		}
		filter.Limit = value
	}
	var err error
	if raw := q.Get("from"); raw != "" {
		filter.From, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, err
		}
	}
	if raw := q.Get("to"); raw != "" {
		filter.To, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, err
		}
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

func writeReportCSV(w http.ResponseWriter, data reportrepo.Data) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeReportFilename(data.ID)+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(w)
	head := make([]string, len(data.Columns))
	for i, column := range data.Columns {
		head[i] = column.Label
	}
	_ = writer.Write(head)
	_ = writer.WriteAll(data.Rows)
}

func listReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": []reportDefinition{
		{ID: "sales_daily", Title: "Продажи по дням", Description: "Replay-safe заказы и оборот без смешивания валют.", Source: "clickhouse", Availability: "ready", FreshnessSLASeconds: 60, Dimensions: []string{"day", "currency"}, Metrics: []string{"orders", "fulfilled", "cancelled", "gross_minor_units"}},
		{ID: "inventory_current", Title: "Текущие остатки", Description: "Последнее спроецированное состояние предложений по складам.", Source: "clickhouse", Availability: "ready", FreshnessSLASeconds: 60, Dimensions: []string{"offer_id", "warehouse_id"}, Metrics: []string{"quantity"}},
		{ID: "ingestion_freshness", Title: "Свежесть данных", Description: "Свежесть ClickHouse-проекций по семействам событий.", Source: "clickhouse", Availability: "ready", FreshnessSLASeconds: 60, Dimensions: []string{"event_family"}, Metrics: []string{"events", "last_occurred_at", "last_ingested_at", "source_lag_seconds"}},
		{ID: "unit_economics_by_channel", Title: "Юнит-экономика по каналам", Description: "Фактический вклад каналов с явной базой признания, источниками и покрытием данных.", Source: "postgresql", Availability: "ready", FreshnessSLASeconds: 300, Dimensions: []string{"channel_ref", "currency", "basis", "quality_status"}, Metrics: []string{"net_revenue_minor_units", "cogs_minor_units", "commission_minor_units", "logistics_minor_units", "advertising_minor_units", "refunds_minor_units", "contribution_profit_minor_units", "margin_basis_points", "coverage_percent"}},
	}})
}
