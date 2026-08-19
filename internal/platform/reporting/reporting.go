// Package reporting defines TORGNEXA's provider-neutral analytical ingestion
// and query boundary. PostgreSQL and canonical domain/event stores remain the
// source of truth; every ClickHouse projection is disposable and replayable.
package reporting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const (
	MaxBatchSize       = 5000
	MaxReplayBatchSize = 10000
	MaxReplayIDBytes   = 128
	MaxStreamBytes     = 192
	MaxPageSize        = 500
)

var (
	ErrInvalid          = errors.New("reporting: invalid value")
	ErrCrossTenantBatch = errors.New("reporting: cross-tenant batch")
	ErrReplayStalled    = errors.New("reporting: replay source made no progress")
)

// SourcePosition is optional transport provenance. Offset -1 means that the
// record was reconstructed from an authoritative backfill rather than consumed
// from a broker partition.
type SourcePosition struct {
	Stream    string
	Partition int32
	Offset    int64
}

func (p SourcePosition) Validate() error {
	if !validStream(p.Stream) || p.Partition < -1 || p.Offset < -1 {
		return ErrInvalid
	}
	if (p.Partition == -1) != (p.Offset == -1) {
		return ErrInvalid
	}
	return nil
}

// Record is the immutable analytics ingest unit. Event is kept byte-for-byte;
// projectors must never become a second transactional source of truth.
type Record struct {
	EventID        string
	EventType      eventbus.EventType
	OccurredAt     domain.UTCInstant
	OrganizationID string
	WorkspaceID    string
	EntityType     string
	EntityID       string
	SourceSystem   string
	CorrelationID  string
	CausationID    string
	ActorID        string
	TraceID        string
	// AnalyticsData is the explicit minimized payload admitted to ClickHouse.
	AnalyticsData []byte
	IngestedAt    domain.UTCInstant
	Source        SourcePosition
	ReplayID      string
	IngestVersion uint64
}

func (r Record) Validate() error {
	candidate := eventbus.Event{
		ID:             r.EventID,
		Type:           r.EventType,
		OccurredAt:     r.OccurredAt,
		OrganizationID: r.OrganizationID,
		WorkspaceID:    r.WorkspaceID,
		EntityType:     r.EntityType,
		EntityID:       r.EntityID,
		Source:         r.SourceSystem,
		CorrelationID:  r.CorrelationID,
		CausationID:    r.CausationID,
		ActorID:        r.ActorID,
		TraceID:        r.TraceID,
		Data:           r.AnalyticsData,
	}
	if err := candidate.Validate(); err != nil {
		return ErrInvalid
	}
	if err := r.IngestedAt.Validate(); err != nil || r.IngestVersion == 0 {
		return ErrInvalid
	}
	if err := r.Source.Validate(); err != nil || !validReplayID(r.ReplayID) {
		return ErrInvalid
	}
	return nil
}

func NewRecord(event eventbus.Event, ingestedAt time.Time, source SourcePosition, replayID string) (Record, error) {
	if err := event.Validate(); err != nil {
		return Record{}, ErrInvalid
	}
	instant, err := domain.NewUTCInstant(ingestedAt.UTC())
	if err != nil {
		return Record{}, ErrInvalid
	}
	record := Record{
		EventID: event.ID, EventType: event.Type, OccurredAt: event.OccurredAt,
		OrganizationID: event.OrganizationID, WorkspaceID: event.WorkspaceID,
		EntityType: event.EntityType, EntityID: event.EntityID, SourceSystem: event.Source,
		CorrelationID: event.CorrelationID, CausationID: event.CausationID, ActorID: event.ActorID, TraceID: event.TraceID,
		AnalyticsData: analyticsPayload(event), IngestedAt: instant, Source: source, ReplayID: replayID,
		IngestVersion: versionFor(instant, event.ID),
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Batch is one acknowledged ClickHouse insert. DedupToken is deterministic for
// the exact canonical records and must be reused on retry.
type Batch struct {
	Records    []Record
	DedupToken string
}

func NewBatch(records []Record) (Batch, error) {
	if len(records) == 0 || len(records) > MaxBatchSize {
		return Batch{}, ErrInvalid
	}
	copied := append([]Record(nil), records...)
	organizationID := copied[0].OrganizationID
	workspaceID := copied[0].WorkspaceID
	seen := make(map[string]struct{}, len(copied))
	for _, record := range copied {
		if err := record.Validate(); err != nil {
			return Batch{}, err
		}
		if record.OrganizationID != organizationID || record.WorkspaceID != workspaceID {
			return Batch{}, ErrCrossTenantBatch
		}
		if _, duplicate := seen[record.EventID]; duplicate {
			return Batch{}, ErrInvalid
		}
		seen[record.EventID] = struct{}{}
	}
	return Batch{Records: copied, DedupToken: batchToken(copied)}, nil
}

// Sink persists replayable analytical records. Implementations must acknowledge
// only after ClickHouse has durably accepted the batch. Fire-and-forget inserts
// are not permitted by this contract.
type Sink interface {
	Append(context.Context, Batch) error
}

// Ingestor is the EventBus-to-analytics boundary.
type Ingestor struct {
	sink Sink
	now  func() time.Time
}

func NewIngestor(sink Sink, now func() time.Time) (*Ingestor, error) {
	if sink == nil {
		return nil, ErrInvalid
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Ingestor{sink: sink, now: now}, nil
}

func (i *Ingestor) Ingest(ctx context.Context, events []eventbus.Event, positions []SourcePosition, replayID string) error {
	if ctx == nil || len(events) == 0 || len(events) > MaxBatchSize || (len(positions) != 0 && len(positions) != len(events)) || !validReplayID(replayID) {
		return ErrInvalid
	}
	now := i.now().UTC()
	records := make([]Record, 0, len(events))
	for index, event := range events {
		source := SourcePosition{Partition: -1, Offset: -1}
		if len(positions) != 0 {
			source = positions[index]
		} else {
			family, err := event.Type.Family()
			if err != nil {
				return ErrInvalid
			}
			source.Stream = family + ".events"
		}
		record, err := NewRecord(event, now, source, replayID)
		if err != nil {
			return err
		}
		records = append(records, record)
	}
	batch, err := NewBatch(records)
	if err != nil {
		return err
	}
	if err := i.sink.Append(ctx, batch); err != nil {
		return fmt.Errorf("reporting append: %w", err)
	}
	return nil
}

// ReplaySource pages canonical events from PostgreSQL/outbox archives or another
// explicitly authoritative source. A replay must be deterministic for a fixed
// checkpoint.
type ReplaySource interface {
	Page(context.Context, tenancy.Scope, string, int) (events []eventbus.Event, nextCheckpoint string, done bool, err error)
}

type ReplayCheckpointStore interface {
	Load(context.Context, tenancy.Scope, string) (string, error)
	Save(context.Context, tenancy.Scope, string, string, bool) error
}

type ReplayRunner struct {
	source      ReplaySource
	checkpoints ReplayCheckpointStore
	ingestor    *Ingestor
}

func NewReplayRunner(source ReplaySource, checkpoints ReplayCheckpointStore, ingestor *Ingestor) (*ReplayRunner, error) {
	if source == nil || checkpoints == nil || ingestor == nil {
		return nil, ErrInvalid
	}
	return &ReplayRunner{source: source, checkpoints: checkpoints, ingestor: ingestor}, nil
}

// RunPage executes one resumable backfill page. replayID identifies the logical
// backfill and is not a source of uniqueness; event_id remains canonical.
func (r *ReplayRunner) RunPage(ctx context.Context, scope tenancy.Scope, replayID string, limit int) (bool, int, error) {
	if ctx == nil || !scope.Valid() || !validReplayID(replayID) || limit < 1 || limit > MaxReplayBatchSize {
		return false, 0, ErrInvalid
	}
	checkpoint, err := r.checkpoints.Load(ctx, scope, replayID)
	if err != nil {
		return false, 0, fmt.Errorf("reporting replay load checkpoint: %w", err)
	}
	events, next, done, err := r.source.Page(ctx, scope, checkpoint, limit)
	if err != nil {
		return false, 0, fmt.Errorf("reporting replay read: %w", err)
	}
	if len(events) == 0 && !done {
		return false, 0, ErrReplayStalled
	}
	if !done && (next == "" || next == checkpoint) {
		return false, 0, ErrReplayStalled
	}
	for start := 0; start < len(events); start += MaxBatchSize {
		end := start + MaxBatchSize
		if end > len(events) {
			end = len(events)
		}
		if err := r.ingestor.Ingest(ctx, events[start:end], nil, replayID); err != nil {
			return false, start, err
		}
	}
	if err := r.checkpoints.Save(ctx, scope, replayID, next, done); err != nil {
		return false, len(events), fmt.Errorf("reporting replay save checkpoint: %w", err)
	}
	return done, len(events), nil
}

// SalesQuery intentionally has no target currency. Until Task 089b, all results
// remain grouped by original ISO currency and cross-currency totals are forbidden.
type SalesQuery struct {
	From  time.Time
	To    time.Time
	Limit int
}

func (q SalesQuery) Validate() error {
	if !isUTC(q.From) || !isUTC(q.To) || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour || q.Limit < 1 || q.Limit > MaxPageSize {
		return ErrInvalid
	}
	return nil
}

type SalesBucket struct {
	Day             time.Time `json:"day"`
	Currency        string    `json:"currency"`
	Orders          uint64    `json:"orders"`
	FulfilledOrders uint64    `json:"fulfilled_orders"`
	CancelledOrders uint64    `json:"cancelled_orders"`
	GrossMinorUnits int64     `json:"gross_minor_units"`
}

func (b SalesBucket) Validate() error {
	return validateBucket(b.Day, b.Currency, b.GrossMinorUnits)
}

type InventoryQuery struct {
	OfferID     string
	WarehouseID string
	Limit       int
}

func (q InventoryQuery) Validate() error {
	if !validOptionalOpaqueID(q.OfferID) || !validOptionalOpaqueID(q.WarehouseID) || q.Limit < 1 || q.Limit > MaxPageSize {
		return ErrInvalid
	}
	return nil
}

type InventoryPosition struct {
	OfferID     string    `json:"offer_id"`
	WarehouseID string    `json:"warehouse_id"`
	Quantity    string    `json:"quantity"`
	ChangedAt   time.Time `json:"changed_at"`
	EventID     string    `json:"event_id"`
}

func (p InventoryPosition) Validate() error {
	if !validOpaqueID(p.OfferID) || !validOpaqueID(p.WarehouseID) || !validDecimal(p.Quantity) || !isUTC(p.ChangedAt) || !validOpaqueID(p.EventID) {
		return ErrInvalid
	}
	return nil
}

type Freshness struct {
	EventFamily    string        `json:"event_family"`
	LastOccurredAt time.Time     `json:"last_occurred_at"`
	LastIngestedAt time.Time     `json:"last_ingested_at"`
	ObservedAt     time.Time     `json:"observed_at"`
	SourceLag      time.Duration `json:"source_lag"`
	EventCount     uint64        `json:"event_count"`
}

func (f Freshness) Validate() error {
	if !validStream(f.EventFamily) || !isUTC(f.LastOccurredAt) || !isUTC(f.LastIngestedAt) || !isUTC(f.ObservedAt) || f.SourceLag < 0 || f.EventCount == 0 {
		return ErrInvalid
	}
	return nil
}

// QueryPort is the only analytical read dependency exposed to applications.
// Implementations must scope every query by both organization and workspace.
type QueryPort interface {
	Sales(context.Context, tenancy.Scope, SalesQuery) ([]SalesBucket, error)
	Inventory(context.Context, tenancy.Scope, InventoryQuery) ([]InventoryPosition, error)
	Freshness(context.Context, tenancy.Scope) ([]Freshness, error)
}

type QueryService struct{ store QueryPort }

func NewQueryService(store QueryPort) (*QueryService, error) {
	if store == nil {
		return nil, ErrInvalid
	}
	return &QueryService{store: store}, nil
}

func (s *QueryService) Sales(ctx context.Context, scope tenancy.Scope, query SalesQuery) ([]SalesBucket, error) {
	if ctx == nil || !scope.Valid() || query.Validate() != nil {
		return nil, ErrInvalid
	}
	rows, err := s.store.Sales(ctx, scope, query)
	if err != nil {
		return nil, err
	}
	if len(rows) > query.Limit {
		return nil, ErrInvalid
	}
	for _, row := range rows {
		if row.Validate() != nil {
			return nil, ErrInvalid
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Day.Equal(rows[j].Day) {
			return rows[i].Currency < rows[j].Currency
		}
		return rows[i].Day.Before(rows[j].Day)
	})
	return rows, nil
}

func (s *QueryService) Inventory(ctx context.Context, scope tenancy.Scope, query InventoryQuery) ([]InventoryPosition, error) {
	if ctx == nil || !scope.Valid() || query.Validate() != nil {
		return nil, ErrInvalid
	}
	rows, err := s.store.Inventory(ctx, scope, query)
	if err != nil {
		return nil, err
	}
	if len(rows) > query.Limit {
		return nil, ErrInvalid
	}
	for _, row := range rows {
		if row.Validate() != nil {
			return nil, ErrInvalid
		}
	}
	return rows, nil
}

func (s *QueryService) Freshness(ctx context.Context, scope tenancy.Scope) ([]Freshness, error) {
	if ctx == nil || !scope.Valid() {
		return nil, ErrInvalid
	}
	rows, err := s.store.Freshness(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Validate() != nil {
			return nil, ErrInvalid
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EventFamily < rows[j].EventFamily })
	return rows, nil
}

func analyticsPayload(event eventbus.Event) []byte {
	switch event.Type.String() {
	case "commerce.orders.order_changed.v1", "commerce.inventory.stock_changed.v1":
		return append([]byte(nil), event.Data...)
	default:
		return []byte("{}")
	}
}

func batchToken(records []Record) string {
	h := sha256.New()
	for _, r := range records {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s\n", r.EventID, r.EventType, r.OrganizationID, r.WorkspaceID, r.Source.Partition, r.Source.Offset, r.ReplayID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func versionFor(instant domain.UTCInstant, eventID string) uint64 {
	micros := uint64(instant.Time().UnixMicro())
	sum := sha256.Sum256([]byte(eventID))
	return (micros << 12) ^ uint64(sum[0])<<4 ^ uint64(sum[1]&0x0f)
}

func validReplayID(v string) bool {
	if v == "" {
		return true
	}
	return validBounded(v, MaxReplayIDBytes)
}
func validStream(v string) bool           { return validBounded(v, MaxStreamBytes) }
func validOpaqueID(v string) bool         { return validBounded(v, 192) }
func validOptionalOpaqueID(v string) bool { return v == "" || validOpaqueID(v) }
func validBounded(v string, max int) bool {
	if v == "" || len(v) > max || v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validDecimal(v string) bool {
	if v == "0" {
		return true
	}
	if len(v) == 0 || len(v) > 64 || v[0] == '+' || strings.TrimSpace(v) != v {
		return false
	}
	dot := false
	for i, c := range v {
		if c == '-' && i == 0 {
			continue
		}
		if c == '.' && !dot && i > 0 && i < len(v)-1 {
			dot = true
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func validateBucket(day time.Time, currency string, amount int64) error {
	if !isUTC(day) || day.Hour() != 0 || day.Minute() != 0 || day.Second() != 0 || day.Nanosecond() != 0 || len(currency) != 3 || amount < 0 {
		return ErrInvalid
	}
	for _, c := range currency {
		if c < 'A' || c > 'Z' {
			return ErrInvalid
		}
	}
	return nil
}
func isUTC(v time.Time) bool { return !v.IsZero() && v.Location() == time.UTC }
