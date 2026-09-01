// Package financialcompletenessrepo stores redacted financial source evidence
// and completeness findings. It never replaces the canonical money ledgers.
package financialcompletenessrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/financialcompleteness"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var (
	ErrInvalid  = errors.New("financial completeness repository: invalid request")
	ErrConflict = errors.New("financial completeness repository: source conflict")
)

// Filter bounds a tenant-scoped source query.
type Filter struct {
	Kind    core.SourceKind
	Quality core.Quality
	AfterID string
	From    time.Time
	To      time.Time
	Limit   int
}

// SourcePage is a bounded source evidence page.
type SourcePage struct {
	Items   []core.SourceRecord
	HasMore bool
}

// Summary is the catalog view consumed by the API and operator UI.
type Summary struct {
	Matrix           []core.Requirement `json:"matrix"`
	Evaluation       core.Evaluation    `json:"evaluation"`
	SourceCount      int                `json:"source_count"`
	OpenFindingCount int                `json:"open_finding_count"`
	LastObservedAt   time.Time          `json:"last_observed_at,omitempty"`
}

// FindingPage is a bounded reconciliation queue page.
type FindingPage struct {
	Items   []core.Finding
	HasMore bool
}

// Repository is a tenant-scoped PostgreSQL adapter.
type Repository struct{ db *sql.DB }

// New constructs a repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// AppendSource inserts one immutable, redacted source fact idempotently.
func (r *Repository) AppendSource(ctx context.Context, scope tenancy.Scope, record core.SourceRecord) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() {
		return ErrInvalid
	}
	if err := record.Validate(); err != nil {
		return err
	}
	org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
	_, err := r.db.ExecContext(ctx, applyScope, org, workspace)
	if err != nil {
		return fmt.Errorf("financial completeness repository: scope: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO financial_source_records(organization_id,workspace_id,record_id,kind,source_system,account_ref,source_ref,statement_id,order_id,payout_id,sku,campaign_id,attribution_status,amount_minor_units,currency,state,quality,occurred_at,posted_at,source_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,COALESCE(NULLIF($21::timestamptz,'0001-01-01 00:00:00+00'),clock_timestamp())) ON CONFLICT(organization_id,workspace_id,kind,source_system,account_ref,source_ref) DO NOTHING`, org, workspace, record.ID, record.Kind, record.SourceSystem, record.AccountRef, record.SourceRef, nullableString(record.StatementID), nullableString(record.OrderID), nullableString(record.PayoutID), nullableString(record.SKU), nullableString(record.CampaignID), nullableString(record.AttributionStatus), record.AmountMinor, record.Currency, record.State, record.Quality, record.OccurredAt, nullableTime(record.PostedAt), record.SourceDigest, nullableTime(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("financial completeness repository: append source: %w", err)
	}
	var amount int64
	var currency, digest string
	err = r.db.QueryRowContext(ctx, `SELECT amount_minor_units,currency,source_digest FROM financial_source_records WHERE organization_id=$1 AND workspace_id=$2 AND kind=$3 AND source_system=$4 AND account_ref=$5 AND source_ref=$6`, org, workspace, record.Kind, record.SourceSystem, record.AccountRef, record.SourceRef).Scan(&amount, &currency, &digest)
	if err != nil {
		return fmt.Errorf("financial completeness repository: verify source: %w", err)
	}
	if amount != record.AmountMinor || currency != record.Currency || digest != record.SourceDigest {
		return ErrConflict
	}
	return nil
}

// ListSources returns redacted evidence in stable record-id order.
func (r *Repository) ListSources(ctx context.Context, scope tenancy.Scope, filter Filter) (SourcePage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || filter.Limit < 1 || filter.Limit > 200 || filter.AfterID != "" && len(filter.AfterID) > 192 {
		return SourcePage{}, ErrInvalid
	}
	org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
	if _, err := r.db.ExecContext(ctx, applyScope, org, workspace); err != nil {
		return SourcePage{}, fmt.Errorf("financial completeness repository: scope: %w", err)
	}
	query := `SELECT record_id,kind,source_system,account_ref,source_ref,statement_id,order_id,payout_id,sku,campaign_id,attribution_status,amount_minor_units,currency,state,quality,occurred_at,posted_at,source_digest,created_at FROM financial_source_records WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR kind=$3) AND ($4='' OR quality=$4) AND ($5='' OR record_id>$5) AND ($6::timestamptz IS NULL OR occurred_at >= $6) AND ($7::timestamptz IS NULL OR occurred_at < $7) ORDER BY record_id LIMIT $8`
	rows, err := r.db.QueryContext(ctx, query, org, workspace, filter.Kind, filter.Quality, filter.AfterID, nullableTime(filter.From), nullableTime(filter.To), filter.Limit+1)
	if err != nil {
		return SourcePage{}, fmt.Errorf("financial completeness repository: list sources: %w", err)
	}
	defer rows.Close()
	page := SourcePage{Items: make([]core.SourceRecord, 0, filter.Limit)}
	for rows.Next() {
		var item core.SourceRecord
		var kind, quality string
		var posted, created sql.NullTime
		var statementID, orderID, payoutID, sku, campaignID, attributionStatus sql.NullString
		if err := rows.Scan(&item.ID, &kind, &item.SourceSystem, &item.AccountRef, &item.SourceRef, &statementID, &orderID, &payoutID, &sku, &campaignID, &attributionStatus, &item.AmountMinor, &item.Currency, &item.State, &quality, &item.OccurredAt, &posted, &item.SourceDigest, &created); err != nil {
			return SourcePage{}, fmt.Errorf("financial completeness repository: scan source: %w", err)
		}
		item.StatementID = statementID.String
		item.OrderID = orderID.String
		item.PayoutID = payoutID.String
		item.SKU = sku.String
		item.CampaignID = campaignID.String
		item.AttributionStatus = attributionStatus.String
		item.Kind = core.SourceKind(kind)
		item.Quality = core.Quality(quality)
		if posted.Valid {
			item.PostedAt = posted.Time.UTC()
		}
		if created.Valid {
			item.CreatedAt = created.Time.UTC()
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return SourcePage{}, err
	}
	if len(page.Items) > filter.Limit {
		page.HasMore = true
		page.Items = page.Items[:filter.Limit]
	}
	return page, nil
}

// ListFindings returns the tenant's reconciliation queue.
func (r *Repository) ListFindings(ctx context.Context, scope tenancy.Scope, afterID string, limit int) (FindingPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || limit < 1 || limit > 200 || len(afterID) > 192 {
		return FindingPage{}, ErrInvalid
	}
	org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
	if _, err := r.db.ExecContext(ctx, applyScope, org, workspace); err != nil {
		return FindingPage{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT finding_id,kind,subject_ref,expected_minor_units,observed_minor_units,currency,severity,status,explanation,owner_ref,detected_at,resolved_at FROM financial_completeness_findings WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR finding_id>$3) ORDER BY finding_id LIMIT $4`, org, workspace, afterID, limit+1)
	if err != nil {
		return FindingPage{}, err
	}
	defer rows.Close()
	page := FindingPage{Items: make([]core.Finding, 0, limit)}
	for rows.Next() {
		var item core.Finding
		var resolved sql.NullTime
		if err := rows.Scan(&item.ID, &item.Kind, &item.SubjectRef, &item.ExpectedMinor, &item.ObservedMinor, &item.Currency, &item.Severity, &item.Status, &item.Explanation, &item.OwnerRef, &item.DetectedAt, &resolved); err != nil {
			return FindingPage{}, err
		}
		if resolved.Valid {
			item.ResolvedAt = resolved.Time.UTC()
		}
		item.DetectedAt = item.DetectedAt.UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return FindingPage{}, err
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
	}
	return page, nil
}

// Summary evaluates the current bounded evidence window without mutating a
// financial run or claiming credentialed cash qualification.
func (r *Repository) Summary(ctx context.Context, scope tenancy.Scope, basis core.Basis, from, to time.Time, reportingCurrency string) (Summary, error) {
	page, err := r.ListSources(ctx, scope, Filter{From: from, To: to, Limit: 200})
	if err != nil {
		return Summary{}, err
	}
	records := append([]core.SourceRecord(nil), page.Items...)
	for page.HasMore {
		last := records[len(records)-1].ID
		page, err = r.ListSources(ctx, scope, Filter{AfterID: last, From: from, To: to, Limit: 200})
		if err != nil {
			return Summary{}, err
		}
		records = append(records, page.Items...)
	}
	evaluation, err := core.Evaluate(basis, from, to, reportingCurrency, records)
	if err != nil {
		return Summary{}, err
	}
	var open int
	var last time.Time
	org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
	if err := r.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(occurred_at),'0001-01-01'::timestamptz) FROM financial_source_records WHERE organization_id=$1 AND workspace_id=$2 AND occurred_at >= $3 AND occurred_at < $4`, org, workspace, from, to).Scan(&open, &last); err != nil {
		return Summary{}, err
	}
	var findings int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM financial_completeness_findings WHERE organization_id=$1 AND workspace_id=$2 AND status<>'resolved'`, org, workspace).Scan(&findings); err != nil {
		return Summary{}, err
	}
	return Summary{Matrix: core.Matrix(), Evaluation: evaluation, SourceCount: open, OpenFindingCount: findings, LastObservedAt: last.UTC()}, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
