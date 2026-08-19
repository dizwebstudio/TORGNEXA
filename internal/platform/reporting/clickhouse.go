package reporting

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const maxClickHouseResponseBytes = 4 << 20

// ClickHouseConfig configures the host-owned analytical HTTP adapter. Password
// is sent only as an HTTP header and is never included in request URLs/errors.
type ClickHouseConfig struct {
	Endpoint string
	Username string
	Password string
	Timeout  time.Duration
	Client   *http.Client
}

// ClickHouseQueryPort reads disposable Task-049 projections through the
// ClickHouse HTTP protocol. Every statement binds both tenant identifiers.
type ClickHouseQueryPort struct {
	endpoint string
	username string
	password string
	timeout  time.Duration
	client   *http.Client
}

// newClickHouseCore validates cfg and builds the endpoint/client fields
// shared verbatim by NewClickHouseQueryPort and NewClickHouseSink, so the two
// adapters can never silently drift apart on which endpoints/timeouts they
// accept.
func newClickHouseCore(cfg ClickHouseConfig) (endpoint string, client *http.Client, err error) {
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", nil, errors.New("reporting clickhouse: invalid endpoint")
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 30*time.Second {
		return "", nil, errors.New("reporting clickhouse: invalid timeout")
	}
	client = cfg.Client
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return strings.TrimRight(cfg.Endpoint, "/"), client, nil
}

// NewClickHouseQueryPort creates a bounded analytical query adapter.
func NewClickHouseQueryPort(cfg ClickHouseConfig) (*ClickHouseQueryPort, error) {
	endpoint, client, err := newClickHouseCore(cfg)
	if err != nil {
		return nil, err
	}
	return &ClickHouseQueryPort{endpoint: endpoint, username: cfg.Username, password: cfg.Password, timeout: cfg.Timeout, client: client}, nil
}

const insertEventFactStatement = "INSERT INTO torgnexa_reporting.event_fact_v1 FORMAT JSONEachRow"

// ClickHouseSink is the production Task-049 Sink: it durably appends a batch
// to torgnexa_reporting.event_fact_v1 over the ClickHouse HTTP interface.
// Every derived table (order/inventory state, freshness, sales/inventory
// views) is populated by ClickHouse-side materialized views attached to this
// single base table, so this adapter never writes anywhere else.
type ClickHouseSink struct {
	endpoint string
	username string
	password string
	timeout  time.Duration
	client   *http.Client
}

// NewClickHouseSink creates a bounded analytical append adapter using the
// same host-owned HTTP endpoint/credentials as ClickHouseQueryPort.
func NewClickHouseSink(cfg ClickHouseConfig) (*ClickHouseSink, error) {
	endpoint, client, err := newClickHouseCore(cfg)
	if err != nil {
		return nil, err
	}
	return &ClickHouseSink{endpoint: endpoint, username: cfg.Username, password: cfg.Password, timeout: cfg.Timeout, client: client}, nil
}

type clickHouseEventFactWire struct {
	EventID           string    `json:"event_id"`
	EventType         string    `json:"event_type"`
	OccurredAt        time.Time `json:"occurred_at"`
	IngestedAt        time.Time `json:"ingested_at"`
	OrganizationID    string    `json:"organization_id"`
	WorkspaceID       string    `json:"workspace_id"`
	EntityType        string    `json:"entity_type"`
	EntityID          string    `json:"entity_id"`
	Source            string    `json:"source"`
	CorrelationID     string    `json:"correlation_id"`
	CausationID       string    `json:"causation_id"`
	ActorID           string    `json:"actor_id"`
	TraceID           string    `json:"trace_id"`
	AnalyticsDataJSON string    `json:"analytics_data_json"`
	ReplayID          string    `json:"replay_id"`
	SourceStream      string    `json:"source_stream"`
	SourcePartition   int32     `json:"source_partition"`
	SourceOffset      int64     `json:"source_offset"`
	IngestVersion     uint64    `json:"ingest_version"`
}

// Append durably inserts batch into event_fact_v1. It is safe to retry: the
// table's ReplacingMergeTree engine and the downstream AggregatingMergeTree
// materialized views are keyed by event_id, so a redelivered batch converges
// rather than double-counting.
func (s *ClickHouseSink) Append(ctx context.Context, batch Batch) error {
	if ctx == nil || s == nil {
		return ErrInvalid
	}
	if _, err := NewBatch(batch.Records); err != nil || batch.DedupToken != batchToken(batch.Records) {
		return ErrInvalid
	}
	rows, err := EventFactRows(batch)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, row := range rows {
		wire := clickHouseEventFactWire{
			EventID: row.EventID, EventType: row.EventType, OccurredAt: row.OccurredAt, IngestedAt: row.IngestedAt,
			OrganizationID: row.OrganizationID, WorkspaceID: row.WorkspaceID, EntityType: row.EntityType, EntityID: row.EntityID,
			Source: row.Source, CorrelationID: row.CorrelationID, CausationID: row.CausationID, ActorID: row.ActorID, TraceID: row.TraceID,
			AnalyticsDataJSON: string(row.AnalyticsDataJSON), ReplayID: row.ReplayID,
			SourceStream: row.SourceStream, SourcePartition: row.SourcePartition, SourceOffset: row.SourceOffset, IngestVersion: row.IngestVersion,
		}
		encoded, encodeErr := json.Marshal(wire)
		if encodeErr != nil {
			return fmt.Errorf("reporting clickhouse: encode row: %w", encodeErr)
		}
		body.Write(encoded)
		body.WriteByte('\n')
	}
	insertCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(insertCtx, http.MethodPost, s.endpoint+"/?query="+url.QueryEscape(insertEventFactStatement), &body)
	if err != nil {
		return fmt.Errorf("reporting clickhouse: create insert: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-ndjson")
	if s.username != "" {
		request.Header.Set("X-ClickHouse-User", s.username)
	}
	if s.password != "" {
		request.Header.Set("X-ClickHouse-Key", s.password)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("reporting clickhouse: insert failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("reporting clickhouse: insert status %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return nil
}

var _ Sink = (*ClickHouseSink)(nil)

func (p *ClickHouseQueryPort) Sales(ctx context.Context, scope tenancy.Scope, query SalesQuery) ([]SalesBucket, error) {
	if ctx == nil || !scope.Valid() || query.Validate() != nil {
		return nil, ErrInvalid
	}
	const statement = `SELECT toString(t.day) AS day,t.currency,t.orders,t.fulfilled_orders,t.cancelled_orders,t.gross_minor_units FROM torgnexa_reporting.sales_daily_v1 AS t WHERE t.organization_id={organization_id:String} AND t.workspace_id={workspace_id:String} AND t.day>=toDate({from:String}) AND t.day<toDate({to:String}) ORDER BY t.day DESC,t.currency LIMIT {limit:UInt64} FORMAT JSONEachRow`
	params := tenantParams(scope)
	params.Set("param_from", query.From.Format("2006-01-02"))
	params.Set("param_to", query.To.Format("2006-01-02"))
	params.Set("param_limit", strconv.Itoa(query.Limit))
	var wire []struct {
		Day       string `json:"day"`
		Currency  string `json:"currency"`
		Orders    uint64 `json:"orders"`
		Fulfilled uint64 `json:"fulfilled_orders"`
		Cancelled uint64 `json:"cancelled_orders"`
		Gross     int64  `json:"gross_minor_units"`
	}
	if err := p.query(ctx, statement, params, &wire); err != nil {
		return nil, err
	}
	rows := make([]SalesBucket, 0, len(wire))
	for _, value := range wire {
		day, err := time.Parse("2006-01-02", value.Day)
		if err != nil {
			return nil, fmt.Errorf("reporting clickhouse: invalid sales row")
		}
		bucket := SalesBucket{Day: day.UTC(), Currency: value.Currency, Orders: value.Orders, FulfilledOrders: value.Fulfilled, CancelledOrders: value.Cancelled, GrossMinorUnits: value.Gross}
		if bucket.Validate() != nil {
			return nil, fmt.Errorf("reporting clickhouse: invalid sales row")
		}
		rows = append(rows, bucket)
	}
	return rows, nil
}

func (p *ClickHouseQueryPort) Inventory(ctx context.Context, scope tenancy.Scope, query InventoryQuery) ([]InventoryPosition, error) {
	if ctx == nil || !scope.Valid() || query.Validate() != nil {
		return nil, ErrInvalid
	}
	const statement = `SELECT offer_id,warehouse_id,quantity,formatDateTime(changed_at,'%FT%T.%6fZ','UTC') AS changed_at,event_id FROM torgnexa_reporting.inventory_current_v1 WHERE organization_id={organization_id:String} AND workspace_id={workspace_id:String} AND ({offer_id:String}='' OR offer_id={offer_id:String}) AND ({warehouse_id:String}='' OR warehouse_id={warehouse_id:String}) ORDER BY changed_at DESC,offer_id,warehouse_id LIMIT {limit:UInt64} FORMAT JSONEachRow`
	params := tenantParams(scope)
	params.Set("param_offer_id", query.OfferID)
	params.Set("param_warehouse_id", query.WarehouseID)
	params.Set("param_limit", strconv.Itoa(query.Limit))
	var wire []struct {
		OfferID     string `json:"offer_id"`
		WarehouseID string `json:"warehouse_id"`
		Quantity    string `json:"quantity"`
		ChangedAt   string `json:"changed_at"`
		EventID     string `json:"event_id"`
	}
	if err := p.query(ctx, statement, params, &wire); err != nil {
		return nil, err
	}
	rows := make([]InventoryPosition, 0, len(wire))
	for _, value := range wire {
		changed, err := time.Parse(time.RFC3339Nano, value.ChangedAt)
		if err != nil {
			return nil, fmt.Errorf("reporting clickhouse: invalid inventory row")
		}
		position := InventoryPosition{OfferID: value.OfferID, WarehouseID: value.WarehouseID, Quantity: value.Quantity, ChangedAt: changed.UTC(), EventID: value.EventID}
		if position.Validate() != nil {
			return nil, fmt.Errorf("reporting clickhouse: invalid inventory row")
		}
		rows = append(rows, position)
	}
	return rows, nil
}

func (p *ClickHouseQueryPort) Freshness(ctx context.Context, scope tenancy.Scope) ([]Freshness, error) {
	if ctx == nil || !scope.Valid() {
		return nil, ErrInvalid
	}
	const statement = `SELECT event_family,formatDateTime(last_occurred_at,'%FT%T.%6fZ','UTC') AS last_occurred_at,formatDateTime(last_ingested_at,'%FT%T.%6fZ','UTC') AS last_ingested_at,formatDateTime(observed_at,'%FT%T.%6fZ','UTC') AS observed_at,source_lag_seconds,event_count FROM torgnexa_reporting.freshness_v1 WHERE organization_id={organization_id:String} AND workspace_id={workspace_id:String} ORDER BY event_family LIMIT 500 FORMAT JSONEachRow`
	var wire []struct {
		EventFamily      string `json:"event_family"`
		LastOccurredAt   string `json:"last_occurred_at"`
		LastIngestedAt   string `json:"last_ingested_at"`
		ObservedAt       string `json:"observed_at"`
		SourceLagSeconds int64  `json:"source_lag_seconds"`
		EventCount       uint64 `json:"event_count"`
	}
	if err := p.query(ctx, statement, tenantParams(scope), &wire); err != nil {
		return nil, err
	}
	rows := make([]Freshness, 0, len(wire))
	for _, value := range wire {
		occurred, e1 := time.Parse(time.RFC3339Nano, value.LastOccurredAt)
		ingested, e2 := time.Parse(time.RFC3339Nano, value.LastIngestedAt)
		observed, e3 := time.Parse(time.RFC3339Nano, value.ObservedAt)
		if e1 != nil || e2 != nil || e3 != nil || value.SourceLagSeconds < 0 {
			return nil, fmt.Errorf("reporting clickhouse: invalid freshness row")
		}
		freshness := Freshness{EventFamily: value.EventFamily, LastOccurredAt: occurred.UTC(), LastIngestedAt: ingested.UTC(), ObservedAt: observed.UTC(), SourceLag: time.Duration(value.SourceLagSeconds) * time.Second, EventCount: value.EventCount}
		if freshness.Validate() != nil {
			return nil, fmt.Errorf("reporting clickhouse: invalid freshness row")
		}
		rows = append(rows, freshness)
	}
	return rows, nil
}

func tenantParams(scope tenancy.Scope) url.Values {
	values := url.Values{}
	values.Set("param_organization_id", scope.OrganizationID().String())
	values.Set("param_workspace_id", scope.WorkspaceID().String())
	return values
}

func (p *ClickHouseQueryPort) query(ctx context.Context, statement string, params url.Values, target any) error {
	queryCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(queryCtx, http.MethodPost, p.endpoint+"/?"+params.Encode(), strings.NewReader(statement))
	if err != nil {
		return fmt.Errorf("reporting clickhouse: create query: %w", err)
	}
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if p.username != "" {
		request.Header.Set("X-ClickHouse-User", p.username)
	}
	if p.password != "" {
		request.Header.Set("X-ClickHouse-Key", p.password)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("reporting clickhouse: query failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("reporting clickhouse: status %d", response.StatusCode)
	}
	limited := &io.LimitedReader{R: response.Body, N: maxClickHouseResponseBytes + 1}
	decoder := bufio.NewScanner(limited)
	decoder.Buffer(make([]byte, 4096), 1<<20)
	rows := make([]json.RawMessage, 0)
	for decoder.Scan() {
		rows = append(rows, bytes.Clone(decoder.Bytes()))
	}
	if err := decoder.Err(); err != nil || limited.N <= 0 {
		return fmt.Errorf("reporting clickhouse: response exceeds limit")
	}
	array, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("reporting clickhouse: encode response")
	}
	if err := json.Unmarshal(array, target); err != nil {
		return fmt.Errorf("reporting clickhouse: invalid response")
	}
	return nil
}
