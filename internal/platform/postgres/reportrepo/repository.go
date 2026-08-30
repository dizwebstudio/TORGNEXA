// Package reportrepo provides tenant-scoped operational report projections from PostgreSQL.
package reportrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Column describes one stable report table column.
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Data is a bounded report result whose row values follow Columns order.
type Data struct {
	ID          string     `json:"id"`
	GeneratedAt time.Time  `json:"generated_at"`
	Source      string     `json:"source"`
	Columns     []Column   `json:"columns"`
	Rows        [][]string `json:"rows"`
}

// Filter contains the bounded, report-independent filters accepted by the API.
// Empty values mean that the corresponding predicate is not applied.
type Filter struct {
	From, To   time.Time
	Query      string
	Currency   string
	Status     string
	Basis      string
	ChannelRef string
	Limit      int
}

// Repository reads report projections without making PostgreSQL analytical truth.
type Repository struct{ database *sql.DB }

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("report repository: database is required")
	}
	return &Repository{database: database}, nil
}

// Report returns one bounded report for the authenticated tenant scope.
func (r *Repository) Report(ctx context.Context, scope tenancy.Scope, id string, filter Filter) (Data, error) {
	if r == nil || r.database == nil || ctx == nil || !scope.Valid() {
		return Data{}, errors.New("report repository: invalid request")
	}
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Data{}, fmt.Errorf("report repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	var appliedOrg, appliedWS string
	if err = tx.QueryRowContext(ctx, applyScope, org, ws).Scan(&appliedOrg, &appliedWS); err != nil {
		return Data{}, fmt.Errorf("report repository: scope: %w", err)
	}
	var data Data
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	if filter.Limit < 1 || filter.Limit > 200 {
		return Data{}, errors.New("report repository: invalid limit")
	}
	switch id {
	case "sales_daily":
		data, err = salesDaily(ctx, tx, org, ws, filter)
	case "inventory_current":
		data, err = inventoryCurrent(ctx, tx, org, ws, filter)
	case "ingestion_freshness":
		data, err = ingestionFreshness(ctx, tx, org, ws, filter)
	case "unit_economics_by_channel":
		data, err = unitEconomicsByChannel(ctx, tx, org, ws, filter)
	default:
		return Data{}, sql.ErrNoRows
	}
	if err != nil {
		return Data{}, err
	}
	data.ID = id
	data.GeneratedAt = time.Now().UTC()
	data.Source = "postgresql"
	if err = tx.Commit(); err != nil {
		return Data{}, fmt.Errorf("report repository: commit: %w", err)
	}
	return data, nil
}

// unitEconomicsByChannel is an operational fallback for the factual channel
// report. The query only reads canonical PostgreSQL facts; ClickHouse remains
// a disposable projection and can be rebuilt without changing these totals.
func unitEconomicsByChannel(ctx context.Context, tx *sql.Tx, org, ws string, filter Filter) (Data, error) {
	basis := filter.Basis
	if basis == "" {
		basis = "order_accrual"
	}
	if basis != "order_accrual" && basis != "settlement" && basis != "cash" {
		return Data{}, errors.New("report repository: invalid unit economics basis")
	}
	// The current adapter has authoritative order facts and settlement facts,
	// but no persisted cash receipt/settlement-run watermark that can safely
	// combine them. Return an explicit empty, fail-closed view instead of
	// presenting a mixed-basis number.
	if basis != "order_accrual" {
		return Data{Columns: unitEconomicsColumns(), Rows: make([][]string, 0)}, nil
	}
	const statement = `WITH cost_latest AS (
	  SELECT DISTINCT ON (order_id,order_item_id) order_id,order_item_id,currency,cogs_minor_units,cost_as_of,created_at,snapshot_id
	  FROM unit_economics_cost_snapshots
	  WHERE organization_id=$1 AND workspace_id=$2
  ORDER BY order_id,order_item_id,cost_as_of DESC,created_at DESC,snapshot_id DESC
), order_cost AS (
  SELECT order_id, currency, COALESCE(SUM(cogs_minor_units),0) AS cogs
  FROM cost_latest GROUP BY order_id,currency
), order_units AS (
  SELECT order_id, COALESCE(SUM(quantity_coefficient::numeric / power(10::numeric,quantity_scale)),0)::text AS units
  FROM order_items WHERE organization_id=$1 AND workspace_id=$2 GROUP BY order_id
), order_rows AS (
  SELECT COALESCE(CASE WHEN a.assignment_state='resolved' THEN NULLIF(a.channel_ref,'') END,'unattributed') AS channel_ref,
    o.currency, count(*)::bigint AS orders,
    COALESCE(SUM(ou.units::numeric),0)::text AS units,
    COALESCE(SUM(o.subtotal_minor_units),0)::bigint AS gross,
    COALESCE(SUM(o.discount_minor_units),0)::bigint AS discounts,
    COALESCE(SUM(CASE WHEN o.status='cancelled' THEN o.grand_minor_units ELSE 0 END),0)::bigint AS cancellations,
    COALESCE(SUM(oc.cogs),0)::bigint AS cogs
  FROM orders o
  LEFT JOIN unit_economics_order_attributions a ON a.organization_id=o.organization_id AND a.workspace_id=o.workspace_id AND a.order_id=o.id
  LEFT JOIN order_cost oc ON oc.order_id=o.id AND oc.currency=o.currency
  LEFT JOIN order_units ou ON ou.order_id=o.id
  WHERE o.organization_id=$1 AND o.workspace_id=$2
    AND ($3::timestamptz IS NULL OR o.placed_at >= $3) AND ($4::timestamptz IS NULL OR o.placed_at < $4)
    AND ($5='' OR o.currency=$5)
    AND ($6='' OR COALESCE(CASE WHEN a.assignment_state='resolved' THEN NULLIF(a.channel_ref,'') END,'unattributed')=$6)
  GROUP BY 1,2
), settlement_rows AS (
  SELECT COALESCE(CASE WHEN a.assignment_state='resolved' THEN NULLIF(a.channel_ref,'') END,'unattributed') AS channel_ref,
    s.currency,
    COALESCE(SUM(CASE WHEN s.kind='fee' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS commission,
    COALESCE(SUM(CASE WHEN s.kind='refund' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS refunds,
    COALESCE(SUM(CASE WHEN s.kind='logistics' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS logistics,
    COALESCE(SUM(CASE WHEN s.kind='storage' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS storage,
    COALESCE(SUM(CASE WHEN s.kind='advertising' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS advertising,
    COALESCE(SUM(CASE WHEN s.kind='penalty' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS penalties,
    COALESCE(SUM(CASE WHEN s.kind='compensation' THEN ABS(s.amount_minor) ELSE 0 END),0)::bigint AS compensation,
    COALESCE(SUM(CASE WHEN s.kind='payout' THEN s.amount_minor ELSE 0 END),0)::bigint AS payout,
    bool_or(s.disputed) AS disputed
  FROM settlement_entries s
  LEFT JOIN unit_economics_order_attributions a ON a.organization_id=s.organization_id AND a.workspace_id=s.workspace_id AND a.order_id=s.order_id
  WHERE s.organization_id=$1 AND s.workspace_id=$2
    AND ($3::timestamptz IS NULL OR s.occurred_at >= $3) AND ($4::timestamptz IS NULL OR s.occurred_at < $4)
    AND ($5='' OR s.currency=$5)
    AND ($6='' OR COALESCE(CASE WHEN a.assignment_state='resolved' THEN NULLIF(a.channel_ref,'') END,'unattributed')=$6)
  GROUP BY 1,2
), merged AS (
  SELECT COALESCE(o.channel_ref,s.channel_ref) AS channel_ref, COALESCE(o.currency,s.currency) AS currency,
    COALESCE(o.orders,0)::bigint AS orders, COALESCE(o.units,'0') AS units,
    COALESCE(o.gross,0)::bigint AS gross, COALESCE(o.discounts,0)::bigint AS discounts,
    COALESCE(o.cancellations,0)::bigint AS cancellations, COALESCE(s.refunds,0)::bigint AS refunds,
    COALESCE(o.cogs,0)::bigint AS cogs, COALESCE(s.commission,0)::bigint AS commission,
    COALESCE(s.logistics,0)::bigint AS logistics, COALESCE(s.storage,0)::bigint AS storage,
    COALESCE(s.advertising,0)::bigint AS advertising, COALESCE(s.penalties,0)::bigint AS penalties,
    COALESCE(s.compensation,0)::bigint AS compensation, COALESCE(s.payout,0)::bigint AS payout,
    COALESCE(s.disputed,false) AS disputed
  FROM order_rows o FULL OUTER JOIN settlement_rows s ON s.channel_ref=o.channel_ref AND s.currency=o.currency
)
SELECT channel_ref,currency,orders,units,
  (gross-discounts-cancellations-refunds)::bigint AS net_revenue,
	  NULLIF(cogs,0)::bigint AS cogs,commission,NULL::bigint AS payment_fee,NULLIF(logistics,0)::bigint AS logistics,NULLIF(storage,0)::bigint AS storage,NULLIF(advertising,0)::bigint AS advertising,refunds,
  (gross-discounts-cancellations-refunds-cogs-commission-logistics-storage-advertising-penalties+compensation)::bigint AS contribution,
  CASE WHEN (gross-discounts-cancellations-refunds)>0 THEN ((gross-discounts-cancellations-refunds-cogs-commission-logistics-storage-advertising-penalties+compensation)*10000/(gross-discounts-cancellations-refunds)) ELSE 0 END::bigint AS margin_bps,
  CASE WHEN disputed THEN 'conflict' WHEN channel_ref='unattributed' THEN 'unmatched' WHEN cogs=0 OR logistics=0 OR storage=0 OR advertising=0 THEN 'partial' ELSE 'complete' END AS quality_status,
  CASE WHEN disputed THEN 35 WHEN channel_ref='unattributed' THEN 35 WHEN cogs=0 OR logistics=0 OR storage=0 OR advertising=0 THEN 55 ELSE 100 END::bigint AS coverage,
  payout
FROM merged
WHERE ($8='' OR (CASE WHEN disputed THEN 'conflict' WHEN channel_ref='unattributed' THEN 'unmatched' WHEN cogs=0 OR logistics=0 OR storage=0 OR advertising=0 THEN 'partial' ELSE 'complete' END)=$8)
ORDER BY quality_status,channel_ref,currency LIMIT $7`
	rows, err := tx.QueryContext(ctx, statement, org, ws, nullableTime(filter.From), nullableTime(filter.To), filter.Currency, filter.ChannelRef, filter.Limit, filter.Status)
	if err != nil {
		return Data{}, fmt.Errorf("report repository: unit economics query: %w", err)
	}
	defer rows.Close()
	data := Data{Columns: unitEconomicsColumns(), Rows: make([][]string, 0)}
	for rows.Next() {
		var channel, currency, units, quality string
		var orders, net, commission, refunds, contribution, margin, coverage, payout int64
		var cogs, paymentFee, logistics, storage, advertising sql.NullInt64
		if err := rows.Scan(&channel, &currency, &orders, &units, &net, &cogs, &commission, &paymentFee, &logistics, &storage, &advertising, &refunds, &contribution, &margin, &quality, &coverage, &payout); err != nil {
			return Data{}, err
		}
		data.Rows = append(data.Rows, []string{channel, currency, basis, strconv.FormatInt(orders, 10), units, strconv.FormatInt(net, 10), nullableMinor(cogs), strconv.FormatInt(commission, 10), nullableMinor(paymentFee), nullableMinor(logistics), nullableMinor(storage), nullableMinor(advertising), strconv.FormatInt(refunds, 10), strconv.FormatInt(contribution, 10), strconv.FormatInt(margin, 10), quality, strconv.FormatInt(coverage, 10), strconv.FormatInt(payout, 10)})
	}
	return data, rows.Err()
}

func unitEconomicsColumns() []Column {
	return []Column{
		{Key: "channel_ref", Label: "Канал"}, {Key: "currency", Label: "Валюта"}, {Key: "basis", Label: "База"}, {Key: "orders", Label: "Заказы"}, {Key: "units", Label: "Единицы"},
		{Key: "net_revenue_minor_units", Label: "Чистая выручка"}, {Key: "cogs_minor_units", Label: "Себестоимость"}, {Key: "commission_minor_units", Label: "Комиссия"}, {Key: "payment_fee_minor_units", Label: "Эквайринг"}, {Key: "logistics_minor_units", Label: "Логистика"}, {Key: "storage_minor_units", Label: "Хранение"}, {Key: "advertising_minor_units", Label: "Реклама"}, {Key: "refunds_minor_units", Label: "Возвраты"}, {Key: "contribution_profit_minor_units", Label: "Вклад"}, {Key: "margin_basis_points", Label: "Маржа, б.п."}, {Key: "quality_status", Label: "Качество"}, {Key: "coverage_percent", Label: "Покрытие, %"}, {Key: "payout_minor_units", Label: "Выплата"},
	}
}

func nullableMinor(value sql.NullInt64) string {
	if !value.Valid {
		return "—"
	}
	return strconv.FormatInt(value.Int64, 10)
}

func salesDaily(ctx context.Context, tx *sql.Tx, org, ws string, filter Filter) (Data, error) {
	rows, err := tx.QueryContext(ctx, `SELECT to_char(placed_at AT TIME ZONE 'UTC','YYYY-MM-DD'),currency,count(*),count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='fulfilled'),coalesce(sum(grand_minor_units),0) FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND ($3::timestamptz IS NULL OR placed_at >= $3) AND ($4::timestamptz IS NULL OR placed_at < $4) AND ($5='' OR currency=$5) AND ($6='' OR status=$6) AND (order_number NOT LIKE 'DEMO-%' OR NOT EXISTS(SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)) GROUP BY 1,2 ORDER BY 1 DESC,2 LIMIT $7`, org, ws, nullableTime(filter.From), nullableTime(filter.To), filter.Currency, filter.Status, filter.Limit)
	if err != nil {
		return Data{}, fmt.Errorf("report repository: sales query: %w", err)
	}
	defer rows.Close()
	data := Data{Columns: []Column{{"day", "День"}, {"currency", "Валюта"}, {"orders", "Заказы"}, {"pending", "Ожидают"}, {"fulfilled", "Выполнены"}, {"gross_minor_units", "Оборот"}}}
	data.Rows = make([][]string, 0)
	for rows.Next() {
		var day, currency string
		var total, pending, fulfilled, gross int64
		if err := rows.Scan(&day, &currency, &total, &pending, &fulfilled, &gross); err != nil {
			return Data{}, err
		}
		data.Rows = append(data.Rows, []string{day, currency, strconv.FormatInt(total, 10), strconv.FormatInt(pending, 10), strconv.FormatInt(fulfilled, 10), strconv.FormatInt(gross, 10)})
	}
	return data, rows.Err()
}

func inventoryCurrent(ctx context.Context, tx *sql.Tx, org, ws string, filter Filter) (Data, error) {
	rows, err := tx.QueryContext(ctx, `SELECT o.sku,w.name,i.on_hand_coefficient,i.on_hand_scale,i.reserved_coefficient,i.reserved_scale,i.unit,i.updated_at FROM inventory_positions i JOIN offers o ON o.id=i.offer_id AND o.organization_id=i.organization_id AND o.workspace_id=i.workspace_id JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id AND w.workspace_id=i.workspace_id WHERE i.organization_id=$1 AND i.workspace_id=$2 AND ($3='' OR o.sku ILIKE '%'||$3||'%' OR w.name ILIKE '%'||$3||'%') AND ($4::timestamptz IS NULL OR i.updated_at >= $4) AND ($5::timestamptz IS NULL OR i.updated_at < $5) AND (o.sku<>'DEMO-SKU' OR NOT EXISTS(SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)) ORDER BY i.updated_at DESC LIMIT $6`, org, ws, filter.Query, nullableTime(filter.From), nullableTime(filter.To), filter.Limit)
	if err != nil {
		return Data{}, fmt.Errorf("report repository: inventory query: %w", err)
	}
	defer rows.Close()
	data := Data{Columns: []Column{{"sku", "SKU"}, {"warehouse", "Склад"}, {"on_hand", "На складе"}, {"reserved", "Резерв"}, {"available", "Доступно"}, {"unit", "Единица"}, {"updated_at", "Обновлено"}}}
	data.Rows = make([][]string, 0)
	for rows.Next() {
		var sku, warehouse, unit string
		var onHand, onScale, reserved, reservedScale int64
		var updated time.Time
		if err := rows.Scan(&sku, &warehouse, &onHand, &onScale, &reserved, &reservedScale, &unit, &updated); err != nil {
			return Data{}, err
		}
		data.Rows = append(data.Rows, []string{sku, warehouse, decimal(onHand, onScale), decimal(reserved, reservedScale), available(onHand, onScale, reserved, reservedScale), unit, updated.UTC().Format(time.RFC3339)})
	}
	return data, rows.Err()
}

func ingestionFreshness(ctx context.Context, tx *sql.Tx, org, ws string, filter Filter) (Data, error) {
	rows, err := tx.QueryContext(ctx, `SELECT event_type,count(*),count(*) FILTER(WHERE published_at IS NULL),max(created_at) FROM outbox_events WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR event_type ILIKE '%'||$3||'%') AND ($4::timestamptz IS NULL OR created_at >= $4) AND ($5::timestamptz IS NULL OR created_at < $5) GROUP BY event_type ORDER BY max(created_at) DESC LIMIT $6`, org, ws, filter.Query, nullableTime(filter.From), nullableTime(filter.To), filter.Limit)
	if err != nil {
		return Data{}, fmt.Errorf("report repository: ingestion query: %w", err)
	}
	defer rows.Close()
	data := Data{Columns: []Column{{"event_type", "Семейство событий"}, {"events", "События"}, {"pending", "Ожидают публикации"}, {"last_created_at", "Последнее событие"}}}
	data.Rows = make([][]string, 0)
	for rows.Next() {
		var eventType string
		var count, pending int64
		var last time.Time
		if err := rows.Scan(&eventType, &count, &pending, &last); err != nil {
			return Data{}, err
		}
		data.Rows = append(data.Rows, []string{eventType, strconv.FormatInt(count, 10), strconv.FormatInt(pending, 10), last.UTC().Format(time.RFC3339)})
	}
	return data, rows.Err()
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func available(onHand, onScale, reserved, reservedScale int64) string {
	scale := onScale
	if reservedScale > scale {
		scale = reservedScale
	}
	coefficient := onHand*pow10(scale-onScale) - reserved*pow10(scale-reservedScale)
	return decimal(coefficient, scale)
}
func pow10(scale int64) int64 {
	value := int64(1)
	for i := int64(0); i < scale; i++ {
		value *= 10
	}
	return value
}
func decimal(coefficient, scale int64) string {
	if scale == 0 {
		return strconv.FormatInt(coefficient, 10)
	}
	negative := coefficient < 0
	if negative {
		coefficient = -coefficient
	}
	digits := strconv.FormatInt(coefficient, 10)
	for int64(len(digits)) <= scale {
		digits = "0" + digits
	}
	cut := len(digits) - int(scale)
	value := digits[:cut] + "." + digits[cut:]
	if negative {
		return "-" + value
	}
	return value
}
