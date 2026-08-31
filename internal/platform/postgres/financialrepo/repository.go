// Package financialrepo owns immutable, tenant-scoped seller-finance
// calculation snapshots. PostgreSQL remains the authoritative source for the
// input facts; ClickHouse may project these snapshots but never recalculates
// or replaces them.
package financialrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	core "github.com/torgnexa/torgnexa/internal/core/uniteconomics"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reportrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var (
	ErrInvalid  = errors.New("financial repository: invalid request")
	ErrNotFound = errors.New("financial repository: run not found")
)

type RunStatus string

const (
	StatusQueued    RunStatus = "queued"
	StatusRunning   RunStatus = "running"
	StatusCompleted RunStatus = "completed"
	StatusPartial   RunStatus = "partial"
	StatusStale     RunStatus = "stale"
	StatusFailed    RunStatus = "failed"
)

func (s RunStatus) valid() bool {
	return s == StatusQueued || s == StatusRunning || s == StatusCompleted || s == StatusPartial || s == StatusStale || s == StatusFailed
}

// RunRequest describes one immutable calculation request. IdempotencyKey is
// also used by the daily worker and is therefore safe to retry.
type RunRequest struct {
	ID                string
	IdempotencyKey    string
	From              time.Time
	To                time.Time
	Basis             core.Basis
	ReportingCurrency string
}

// Run is the public calculation status and its immutable evidence snapshot.
type Run struct {
	ID             string                 `json:"id"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Status         RunStatus              `json:"status"`
	CreatedAt      time.Time              `json:"created_at"`
	CompletedAt    time.Time              `json:"completed_at"`
	Snapshot       core.FinancialSnapshot `json:"snapshot"`
}

// Filter is deliberately narrower than SQL. It cannot select another tenant
// or request an unbounded detail export.
type Filter struct {
	RunID             string
	From              time.Time
	To                time.Time
	Basis             core.Basis
	ReportingCurrency string
	ChannelRef        string
	SKU               string
	OrderID           string
	Query             string
	Limit             int
	Cursor            string
}

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// RefreshDaily creates the deterministic previous UTC day snapshot used by
// the background worker. Repeated polls are idempotent for the same day.
func (r *Repository) RefreshDaily(ctx context.Context, scope tenancy.Scope, now time.Time) (Run, error) {
	if now.IsZero() || !now.Equal(now.UTC()) {
		return Run{}, ErrInvalid
	}
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.Add(-24 * time.Hour)
	return r.Calculate(ctx, scope, RunRequest{IdempotencyKey: "auto:daily:" + start.Format("2006-01-02"), From: start, To: end, Basis: core.BasisOrderAccrual})
}

func validRunRequest(req RunRequest) bool {
	return req.IdempotencyKey != "" && len(req.IdempotencyKey) <= 192 && !req.From.IsZero() && !req.To.IsZero() && req.To.After(req.From) && req.To.Sub(req.From) <= 366*24*time.Hour && req.From.Equal(req.From.UTC()) && req.To.Equal(req.To.UTC()) && req.Basis.Valid() && (req.ReportingCurrency == "" || len(req.ReportingCurrency) == 3)
}

func validFilter(filter Filter) bool {
	if filter.Limit < 0 || filter.Limit > 200 || len(filter.RunID) > 192 || len(filter.ChannelRef) > 192 || len(filter.SKU) > 128 || len(filter.OrderID) > 192 || len(filter.Query) > 100 || len(filter.Cursor) > 128 || (filter.Cursor != "" && !strings.HasPrefix(filter.Cursor, "v1.")) || (filter.Cursor != "" && !validFinancialCursor(filter.Cursor)) || (filter.Basis != "" && !filter.Basis.Valid()) || (filter.ReportingCurrency != "" && len(filter.ReportingCurrency) != 3) {
		return false
	}
	return (filter.From.IsZero() || filter.From.Equal(filter.From.UTC())) && (filter.To.IsZero() || filter.To.Equal(filter.To.UTC())) && (filter.From.IsZero() || filter.To.IsZero() || filter.To.After(filter.From))
}

func runID(request RunRequest, scope tenancy.Scope) string {
	if request.ID != "" {
		return request.ID
	}
	sum := sha256.Sum256([]byte(scope.OrganizationID().String() + "\x00" + scope.WorkspaceID().String() + "\x00" + request.IdempotencyKey))
	return "finrun-" + hex.EncodeToString(sum[:])[:24]
}

// Calculate loads current canonical facts, calculates them once, and stores a
// snapshot. Existing idempotency keys return the original snapshot verbatim.
func (r *Repository) Calculate(ctx context.Context, scope tenancy.Scope, request RunRequest) (Run, error) {
	request.ReportingCurrency = strings.ToUpper(strings.TrimSpace(request.ReportingCurrency))
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || !validRunRequest(request) {
		return Run{}, ErrInvalid
	}
	id := runID(request, scope)
	input, err := r.loadInput(ctx, scope, request)
	if err != nil {
		return Run{}, fmt.Errorf("financial repository: load facts: %w", err)
	}
	snapshot, err := core.CalculateFinancial(input, time.Now().UTC())
	if err != nil {
		return Run{}, fmt.Errorf("financial repository: calculate: %w", err)
	}
	document, err := json.Marshal(snapshot)
	if err != nil || len(document) > 8<<20 {
		return Run{}, ErrInvalid
	}
	status := StatusPartial
	if snapshot.QualityStatus == core.QualityComplete {
		status = StatusCompleted
	}
	now := snapshot.GeneratedAt
	watermarks, _ := json.Marshal(map[string]any{"orders": len(input.SaleLines), "facts": len(input.Facts), "calculation": "daily_or_manual"})
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("financial repository: begin: %w", err)
	}
	defer tx.Rollback()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	if _, err = tx.ExecContext(ctx, applyScope, org, ws); err != nil {
		return Run{}, fmt.Errorf("financial repository: scope: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO financial_calculation_runs(organization_id,workspace_id,run_id,idempotency_key,from_at,to_at,basis,reporting_currency,algorithm_version,metric_definition_version,allocation_policy_version,valuation_policy_version,attribution_policy_version,input_digest,status,quality_status,coverage_percent,source_watermarks,created_at,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19) ON CONFLICT(organization_id,workspace_id,idempotency_key) DO NOTHING`, org, ws, id, request.IdempotencyKey, request.From, request.To, request.Basis, request.ReportingCurrency, snapshot.AlgorithmVersion, snapshot.MetricDefinitionVersion, snapshot.AllocationPolicyVersion, snapshot.ValuationPolicyVersion, snapshot.AttributionPolicyVersion, snapshot.InputDigest, status, snapshot.QualityStatus, snapshot.CoveragePercent, watermarks, now)
	if err != nil {
		return Run{}, fmt.Errorf("financial repository: insert run: %w", err)
	}
	var existingID string
	var existingStatus RunStatus
	var created, completed time.Time
	var existingDocument []byte
	err = tx.QueryRowContext(ctx, `SELECT r.run_id,r.status,r.created_at,COALESCE(r.completed_at,r.created_at),s.snapshot_document FROM financial_calculation_runs r JOIN financial_calculation_snapshots s ON s.organization_id=r.organization_id AND s.workspace_id=r.workspace_id AND s.run_id=r.run_id WHERE r.organization_id=$1 AND r.workspace_id=$2 AND r.idempotency_key=$3 ORDER BY s.created_at DESC LIMIT 1`, org, ws, request.IdempotencyKey).Scan(&existingID, &existingStatus, &created, &completed, &existingDocument)
	if err != nil {
		return Run{}, fmt.Errorf("financial repository: read run: %w", err)
	}
	if existingID == id {
		var existing core.FinancialSnapshot
		if err := json.Unmarshal(existingDocument, &existing); err != nil {
			return Run{}, ErrInvalid
		}
		if err := tx.Commit(); err != nil {
			return Run{}, err
		}
		return Run{ID: existingID, IdempotencyKey: request.IdempotencyKey, Status: existingStatus, CreatedAt: created.UTC(), CompletedAt: completed.UTC(), Snapshot: existing}, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO financial_calculation_snapshots(organization_id,workspace_id,run_id,snapshot_id,snapshot_document,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, org, ws, id, snapshot.ID, document, now); err != nil {
		return Run{}, fmt.Errorf("financial repository: insert snapshot: %w", err)
	}
	for index, reason := range snapshot.QualityReasons {
		if _, err = tx.ExecContext(ctx, `INSERT INTO financial_calculation_quality_issues(organization_id,workspace_id,run_id,issue_id,code,explanation,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, org, ws, id, fmt.Sprintf("quality-%03d", index+1), reason, reason, now); err != nil {
			return Run{}, fmt.Errorf("financial repository: insert quality issue: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO financial_calculation_events(organization_id,workspace_id,run_id,event_id,from_status,to_status,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, org, ws, id, "event-001", string(StatusQueued), string(status), now); err != nil {
		return Run{}, fmt.Errorf("financial repository: insert run event: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("financial repository: commit: %w", err)
	}
	return Run{ID: id, IdempotencyKey: request.IdempotencyKey, Status: status, CreatedAt: now, CompletedAt: now, Snapshot: snapshot}, nil
}

func (r *Repository) loadInput(ctx context.Context, scope tenancy.Scope, request RunRequest) (core.FinancialInput, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return core.FinancialInput{}, err
	}
	defer tx.Rollback()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	if _, err = tx.ExecContext(ctx, applyScope, org, ws); err != nil {
		return core.FinancialInput{}, err
	}
	input := core.FinancialInput{OrganizationID: org, WorkspaceID: ws, Basis: request.Basis, From: request.From, To: request.To, ReportingCurrency: request.ReportingCurrency}
	rows, err := tx.QueryContext(ctx, `SELECT oi.id,o.id,oi.sku_snapshot,COALESCE(CASE WHEN a.assignment_state='resolved' THEN NULLIF(a.channel_ref,'') END,'unattributed'),o.currency,o.placed_at,(oi.quantity_coefficient::numeric*1000/power(10::numeric,oi.quantity_scale))::bigint,oi.subtotal_minor_units,oi.discount_minor_units,CASE WHEN o.status='cancelled' THEN oi.line_total_minor_units ELSE 0 END,COALESCE(c.cogs_minor_units,NULL),c.currency FROM orders o JOIN order_items oi ON oi.organization_id=o.organization_id AND oi.workspace_id=o.workspace_id AND oi.order_id=o.id LEFT JOIN unit_economics_order_attributions a ON a.organization_id=o.organization_id AND a.workspace_id=o.workspace_id AND a.order_id=o.id LEFT JOIN LATERAL (SELECT cogs_minor_units,currency FROM unit_economics_cost_snapshots c0 WHERE c0.organization_id=oi.organization_id AND c0.workspace_id=oi.workspace_id AND c0.order_item_id=oi.id ORDER BY c0.cost_as_of DESC,c0.created_at DESC,c0.snapshot_id DESC LIMIT 1)c ON true WHERE o.organization_id=$1 AND o.workspace_id=$2 AND o.placed_at >= $3 AND o.placed_at < $4 ORDER BY o.placed_at,oi.id`, org, ws, request.From, request.To)
	if err != nil {
		return core.FinancialInput{}, err
	}
	for rows.Next() {
		var line core.SaleLineFact
		var cogs sql.NullInt64
		var costCurrency sql.NullString
		if err := rows.Scan(&line.ID, &line.OrderID, &line.SKU, &line.ChannelRef, &line.Currency, &line.OccurredAt, &line.QuantityMilli, &line.GrossMinor, &line.DiscountMinor, &line.CancellationMinor, &cogs, &costCurrency); err != nil {
			rows.Close()
			return core.FinancialInput{}, err
		}
		line.OccurredAt = line.OccurredAt.UTC()
		if cogs.Valid && costCurrency.Valid && costCurrency.String == line.Currency {
			line.COGSMinor = &cogs.Int64
		}
		input.SaleLines = append(input.SaleLines, line)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return core.FinancialInput{}, err
	}
	rows.Close()
	settlementRows, err := tx.QueryContext(ctx, `SELECT s.entry_id,s.provider,s.provider_account_id,s.provider_entry_ref,s.order_id,s.currency,s.kind,s.amount_minor,s.occurred_at,s.disputed,COALESCE(CASE WHEN a.assignment_state='resolved' THEN NULLIF(a.channel_ref,'') END,'') FROM settlement_entries s LEFT JOIN unit_economics_order_attributions a ON a.organization_id=s.organization_id AND a.workspace_id=s.workspace_id AND a.order_id=s.order_id WHERE s.organization_id=$1 AND s.workspace_id=$2 AND s.occurred_at >= $3 AND s.occurred_at < $4 ORDER BY s.occurred_at,s.entry_id`, org, ws, request.From, request.To)
	if err != nil {
		return core.FinancialInput{}, err
	}
	for settlementRows.Next() {
		var id, system, account, ref, order, currency, kind, channel string
		var amount int64
		var occurred time.Time
		var disputed bool
		if err := settlementRows.Scan(&id, &system, &account, &ref, &order, &currency, &kind, &amount, &occurred, &disputed, &channel); err != nil {
			settlementRows.Close()
			return core.FinancialInput{}, err
		}
		mapped := mapSettlementKind(kind)
		if mapped == "" {
			continue
		}
		input.Facts = append(input.Facts, core.FinancialFact{ID: id, SourceSystem: system, SourceAccount: account, SourceRef: ref, IdempotencyKey: ref, OrderID: order, ChannelRef: channel, Kind: mapped, AmountMinor: amount, Currency: currency, Basis: request.Basis, OccurredAt: occurred.UTC(), Confirmed: true, Quality: core.ValueObserved, Disputed: disputed})
	}
	if err := settlementRows.Err(); err != nil {
		settlementRows.Close()
		return core.FinancialInput{}, err
	}
	settlementRows.Close()
	ads, err := tx.QueryContext(ctx, `SELECT aa.action_id,aa.currency,aa.requested_spend_minor,COALESCE(aa.executed_at,clock_timestamp()) FROM advertising_actions aa WHERE aa.organization_id=$1 AND aa.workspace_id=$2 AND aa.executed_at IS NOT NULL AND aa.executed_at >= $3 AND aa.executed_at < $4 ORDER BY aa.executed_at,aa.action_id`, org, ws, request.From, request.To)
	if err != nil {
		return core.FinancialInput{}, err
	}
	for ads.Next() {
		var id, currency string
		var amount int64
		var at time.Time
		if err := ads.Scan(&id, &currency, &amount, &at); err != nil {
			ads.Close()
			return core.FinancialInput{}, err
		}
		input.Facts = append(input.Facts, core.FinancialFact{ID: id, SourceSystem: "advertising", SourceAccount: "campaign", SourceRef: id, IdempotencyKey: id, Kind: core.FactAdvertising, AmountMinor: -amount, Currency: currency, Basis: request.Basis, OccurredAt: at.UTC(), Expected: true, Confirmed: false, Quality: core.ValueEstimated})
	}
	if err := ads.Err(); err != nil {
		ads.Close()
		return core.FinancialInput{}, err
	}
	ads.Close()
	logistics, err := tx.QueryContext(ctx, `SELECT shipment_id,provider_account_id,currency,cost_minor_units,updated_at FROM logistics_shipments WHERE organization_id=$1 AND workspace_id=$2 AND updated_at >= $3 AND updated_at < $4 AND cost_minor_units > 0 ORDER BY updated_at,shipment_id`, org, ws, request.From, request.To)
	if err != nil {
		return core.FinancialInput{}, err
	}
	for logistics.Next() {
		var id, account, currency string
		var amount int64
		var at time.Time
		if err := logistics.Scan(&id, &account, &currency, &amount, &at); err != nil {
			logistics.Close()
			return core.FinancialInput{}, err
		}
		input.Facts = append(input.Facts, core.FinancialFact{ID: id, SourceSystem: "logistics", SourceAccount: account, SourceRef: id, IdempotencyKey: id, Kind: core.FactLogistics, AmountMinor: -amount, Currency: currency, Basis: request.Basis, OccurredAt: at.UTC(), Confirmed: true, Quality: core.ValueObserved})
	}
	if err := logistics.Err(); err != nil {
		logistics.Close()
		return core.FinancialInput{}, err
	}
	logistics.Close()
	return input, nil
}

func mapSettlementKind(kind string) core.FinancialFactKind {
	switch kind {
	case "fee":
		return core.FactCommission
	case "refund":
		return core.FactRefund
	case "payout":
		return core.FactPayout
	case "logistics":
		return core.FactLogistics
	case "storage":
		return core.FactStorage
	case "advertising":
		return core.FactAdvertising
	case "penalty":
		return core.FactPenalty
	case "compensation":
		return core.FactCompensation
	case "withholding":
		return core.FactOther
	default:
		return ""
	}
}

// Report returns a previously stored snapshot and never recalculates it.
func (r *Repository) Report(ctx context.Context, scope tenancy.Scope, id string, filter Filter) (reportrepo.Data, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || id == "" || !validFilter(filter) {
		return reportrepo.Data{}, ErrInvalid
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	snapshot, err := r.snapshot(ctx, scope, filter)
	if err != nil {
		return reportrepo.Data{}, err
	}
	return toReport(id, snapshot, filter), nil
}

func (r *Repository) snapshot(ctx context.Context, scope tenancy.Scope, filter Filter) (core.FinancialSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return core.FinancialSnapshot{}, err
	}
	defer tx.Rollback()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	if _, err = tx.ExecContext(ctx, applyScope, org, ws); err != nil {
		return core.FinancialSnapshot{}, err
	}
	query := `SELECT s.snapshot_document FROM financial_calculation_snapshots s JOIN financial_calculation_runs r ON r.organization_id=s.organization_id AND r.workspace_id=s.workspace_id AND r.run_id=s.run_id WHERE s.organization_id=$1 AND s.workspace_id=$2 AND r.status IN ('completed','partial','stale')`
	args := []any{org, ws}
	if filter.RunID != "" {
		query += ` AND r.run_id=$3`
		args = append(args, filter.RunID)
	} else {
		query += ` AND ($3='' OR r.basis=$3) AND ($4='' OR r.reporting_currency=$4) AND ($5::timestamptz IS NULL OR r.from_at >= $5) AND ($6::timestamptz IS NULL OR r.to_at <= $6) ORDER BY r.completed_at DESC,r.run_id DESC LIMIT 1`
		args = append(args, string(filter.Basis), filter.ReportingCurrency, nullableFinancialTime(filter.From), nullableFinancialTime(filter.To))
	}
	var document []byte
	if err = tx.QueryRowContext(ctx, query, args...).Scan(&document); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.FinancialSnapshot{}, ErrNotFound
		}
		return core.FinancialSnapshot{}, err
	}
	var snapshot core.FinancialSnapshot
	if err = json.Unmarshal(document, &snapshot); err != nil {
		return core.FinancialSnapshot{}, ErrInvalid
	}
	return snapshot, nil
}

func nullableFinancialTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func toReport(id string, snapshot core.FinancialSnapshot, filter Filter) reportrepo.Data {
	data := reportrepo.Data{ID: id, GeneratedAt: snapshot.GeneratedAt, Source: "postgresql.snapshot", Rows: make([][]string, 0)}
	switch id {
	case "seller_profit_and_loss", "seller_unit_economics":
		data.Columns = financialColumns()
		for _, row := range snapshot.Rows {
			if matches(row, filter) {
				data.Rows = append(data.Rows, financialRowValues(row))
			}
		}
	case "seller_profit_and_loss_details":
		data.Columns = financialDetailColumns()
		for _, row := range snapshot.DetailRows {
			if matches(row, filter) {
				data.Rows = append(data.Rows, financialDetailValues(row))
			}
		}
	case "seller_cash_flow":
		data.Columns = cashColumns()
		for _, row := range snapshot.CashRows {
			if filter.ChannelRef != "" && row.ChannelRef != filter.ChannelRef {
				continue
			}
			data.Rows = append(data.Rows, cashValues(row))
		}
	case "seller_financial_quality":
		data.Columns = []reportrepo.Column{{Key: "channel_ref", Label: "Канал"}, {Key: "currency", Label: "Валюта"}, {Key: "quality_status", Label: "Качество"}, {Key: "coverage_percent", Label: "Покрытие, %"}, {Key: "quality_reasons", Label: "Причины"}}
		for _, row := range snapshot.Rows {
			if matches(row, filter) {
				data.Rows = append(data.Rows, []string{row.ChannelRef, row.Currency, string(row.QualityStatus), strconv.FormatInt(row.CoveragePercent, 10), strings.Join(row.QualityReasons, ", ")})
			}
		}
	}
	data.Rows, data.NextCursor = paginateFinancialRows(data.Rows, filter)
	return data
}

func paginateFinancialRows(rows [][]string, filter Filter) ([][]string, string) {
	offset := 0
	if filter.Cursor != "" {
		var valid bool
		offset, valid = financialCursorOffset(filter.Cursor)
		if !valid {
			return rows[:0], ""
		}
	}
	if offset >= len(rows) {
		return rows[:0], ""
	}
	rows = rows[offset:]
	limit := filter.Limit
	if limit == 0 {
		limit = 100
	}
	if len(rows) <= limit {
		return rows, ""
	}
	next := offset + limit
	return rows[:limit], "v1." + base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(next)))
}

func financialCursorOffset(cursor string) (int, bool) {
	if !strings.HasPrefix(cursor, "v1.") {
		return 0, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "v1."))
	if err != nil || len(decoded) == 0 {
		return 0, false
	}
	value, err := strconv.Atoi(string(decoded))
	return value, err == nil && value >= 0
}

func validFinancialCursor(cursor string) bool {
	_, ok := financialCursorOffset(cursor)
	return ok
}

func matches(row core.FinancialRow, filter Filter) bool {
	return (filter.ChannelRef == "" || row.ChannelRef == filter.ChannelRef) && (filter.SKU == "" || row.SKU == filter.SKU) && (filter.OrderID == "" || row.OrderID == filter.OrderID) && (filter.Query == "" || strings.Contains(row.ChannelRef, filter.Query) || strings.Contains(row.SKU, filter.Query) || strings.Contains(row.OrderID, filter.Query))
}
func financialColumns() []reportrepo.Column {
	return []reportrepo.Column{{Key: "level", Label: "Уровень"}, {Key: "channel_ref", Label: "Канал"}, {Key: "currency", Label: "Валюта"}, {Key: "orders", Label: "Заказы"}, {Key: "units_milli", Label: "Единицы, ‰"}, {Key: "gross_minor_units", Label: "Валовая выручка"}, {Key: "discount_minor_units", Label: "Скидки"}, {Key: "cancellation_minor_units", Label: "Отмены"}, {Key: "refund_minor_units", Label: "Возвраты"}, {Key: "net_sales_minor_units", Label: "Чистые продажи"}, {Key: "cogs_minor_units", Label: "FIFO-себестоимость"}, {Key: "commission_minor_units", Label: "Комиссии"}, {Key: "payment_fee_minor_units", Label: "Платёжные сборы"}, {Key: "logistics_minor_units", Label: "Логистика"}, {Key: "storage_minor_units", Label: "Хранение"}, {Key: "advertising_minor_units", Label: "Реклама"}, {Key: "promotion_minor_units", Label: "Продвижение"}, {Key: "penalty_minor_units", Label: "Штрафы"}, {Key: "compensation_minor_units", Label: "Компенсации"}, {Key: "contribution_profit_minor_units", Label: "Вклад"}, {Key: "margin_basis_points", Label: "Маржа, б.п."}, {Key: "take_rate_basis_points", Label: "Take rate, б.п."}, {Key: "refund_rate_basis_points", Label: "Возвраты, б.п."}, {Key: "quality_status", Label: "Качество"}, {Key: "coverage_percent", Label: "Покрытие, %"}}
}
func financialDetailColumns() []reportrepo.Column {
	columns := financialColumns()
	return append([]reportrepo.Column{{Key: "order_id", Label: "Заказ"}, {Key: "sku", Label: "SKU"}}, columns...)
}
func financialDetailValues(r core.FinancialRow) []string {
	return append([]string{r.OrderID, r.SKU}, financialRowValues(r)...)
}
func financialRowValues(r core.FinancialRow) []string {
	return []string{r.Level, r.ChannelRef, r.Currency, strconv.FormatInt(r.Orders, 10), strconv.FormatInt(r.UnitsMilli, 10), strconv.FormatInt(r.GrossMinor, 10), strconv.FormatInt(r.DiscountMinor, 10), strconv.FormatInt(r.CancellationMinor, 10), strconv.FormatInt(r.RefundMinor, 10), strconv.FormatInt(r.NetSalesMinor, 10), strconv.FormatInt(r.COGSMinor, 10), strconv.FormatInt(r.CommissionMinor, 10), strconv.FormatInt(r.PaymentFeeMinor, 10), strconv.FormatInt(r.LogisticsMinor, 10), strconv.FormatInt(r.StorageMinor, 10), strconv.FormatInt(r.AdvertisingMinor, 10), strconv.FormatInt(r.PromotionMinor, 10), strconv.FormatInt(r.PenaltyMinor, 10), strconv.FormatInt(r.CompensationMinor, 10), strconv.FormatInt(r.ContributionMinor, 10), strconv.FormatInt(r.MarginBPS, 10), strconv.FormatInt(r.TakeRateBPS, 10), strconv.FormatInt(r.RefundRateBPS, 10), string(r.QualityStatus), strconv.FormatInt(r.CoveragePercent, 10)}
}
func cashColumns() []reportrepo.Column {
	return []reportrepo.Column{{Key: "channel_ref", Label: "Канал"}, {Key: "currency", Label: "Валюта"}, {Key: "payout_minor_units", Label: "Выплаты"}, {Key: "bank_receipt_minor_units", Label: "Поступления банка"}, {Key: "refund_minor_units", Label: "Возвраты"}, {Key: "supplier_payment_minor_units", Label: "Поставщики"}, {Key: "logistics_minor_units", Label: "Логистика"}, {Key: "advertising_minor_units", Label: "Реклама"}, {Key: "storage_minor_units", Label: "Хранение"}, {Key: "fee_minor_units", Label: "Комиссии и сборы"}, {Key: "penalty_minor_units", Label: "Штрафы"}, {Key: "tax_minor_units", Label: "Налоги"}, {Key: "other_minor_units", Label: "Прочее"}, {Key: "net_cash_minor_units", Label: "Чистый денежный поток"}, {Key: "quality_status", Label: "Качество"}, {Key: "coverage_percent", Label: "Покрытие, %"}}
}
func cashValues(r core.CashFlowRow) []string {
	return []string{r.ChannelRef, r.Currency, strconv.FormatInt(r.PayoutMinor, 10), strconv.FormatInt(r.BankReceiptMinor, 10), strconv.FormatInt(r.RefundMinor, 10), strconv.FormatInt(r.SupplierPaymentMinor, 10), strconv.FormatInt(r.LogisticsMinor, 10), strconv.FormatInt(r.AdvertisingMinor, 10), strconv.FormatInt(r.StorageMinor, 10), strconv.FormatInt(r.FeeMinor, 10), strconv.FormatInt(r.PenaltyMinor, 10), strconv.FormatInt(r.TaxMinor, 10), strconv.FormatInt(r.OtherMinor, 10), strconv.FormatInt(r.NetCashMinor, 10), string(r.QualityStatus), strconv.FormatInt(r.CoveragePercent, 10)}
}
