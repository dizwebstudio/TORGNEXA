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
	From, To time.Time
	Query    string
	Currency string
	Status   string
	Limit    int
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
