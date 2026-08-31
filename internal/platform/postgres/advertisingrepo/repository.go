// Package advertisingrepo persists the normalized marketplace advertising
// projection. PostgreSQL is the source of truth for facts and watermarks;
// ClickHouse may consume a derived projection later.
package advertisingrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/advertising"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var (
	ErrInvalid  = errors.New("advertising repository: invalid request")
	ErrNotFound = errors.New("advertising repository: not found")
	ErrConflict = errors.New("advertising repository: conflicting fact")
)

// Filter controls all advertising reads and is deliberately narrower than
// SQL. The repository always adds the tenant predicate and a hard limit.
type Filter struct {
	From       time.Time
	To         time.Time
	Channel    string
	CampaignID string
	SKU        string
	Limit      int
}

// SyncRun is durable worker state, including counts and a bounded cursor.
type SyncRun struct {
	ID             string    `json:"id"`
	AccountID      string    `json:"account_id"`
	Channel        string    `json:"channel"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	Mode           string    `json:"mode"`
	Status         string    `json:"status"`
	NextCursor     string    `json:"next_cursor,omitempty"`
	WatermarkAt    time.Time `json:"watermark_at,omitempty"`
	FetchedCount   int       `json:"fetched_count"`
	AcceptedCount  int       `json:"accepted_count"`
	RejectedCount  int       `json:"rejected_count"`
	ErrorCode      string    `json:"error_code,omitempty"`
	EvidenceDigest string    `json:"evidence_digest,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
}

// Repository is a tenant-scoped advertising projection repository.
type Repository struct{ db *sql.DB }

// New constructs a repository over the application database.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() {
		return ErrInvalid
	}
	return nil
}

// UpsertCampaign stores the latest remote campaign observation. No credentials
// or raw provider payload are accepted by this boundary.
func (r *Repository) UpsertCampaign(ctx context.Context, scope tenancy.Scope, c core.Campaign) error {
	if err := r.validate(ctx, scope); err != nil || c.Validate() != nil {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO advertising_campaigns(organization_id,workspace_id,campaign_id,account_id,channel,remote_id,name,status,currency,daily_budget_minor,total_budget_minor,observed_at,effective_from,effective_to,version,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$12) ON CONFLICT(organization_id,workspace_id,campaign_id) DO UPDATE SET account_id=EXCLUDED.account_id,channel=EXCLUDED.channel,remote_id=EXCLUDED.remote_id,name=EXCLUDED.name,status=EXCLUDED.status,currency=EXCLUDED.currency,daily_budget_minor=EXCLUDED.daily_budget_minor,total_budget_minor=EXCLUDED.total_budget_minor,observed_at=EXCLUDED.observed_at,effective_from=EXCLUDED.effective_from,effective_to=EXCLUDED.effective_to,version=advertising_campaigns.version+1,updated_at=EXCLUDED.updated_at`, scope.OrganizationID(), scope.WorkspaceID(), c.ID, c.AccountID, c.Channel, c.RemoteID, c.Name, string(c.Status), c.Currency, c.DailyBudgetMinor, c.TotalBudgetMinor, c.ObservedAt, nullableTime(c.EffectiveFrom), nullableTime(c.EffectiveTo), c.Version)
		return err
	})
}

// AppendSpend stores an immutable normalized spend fact idempotently.
func (r *Repository) AppendSpend(ctx context.Context, scope tenancy.Scope, fact core.SpendFact) error {
	if err := r.validate(ctx, scope); err != nil || fact.Validate() != nil {
		return ErrInvalid
	}
	fingerprint := core.FingerprintSpend(fact)
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM advertising_spend_facts WHERE organization_id=$1 AND workspace_id=$2 AND account_id=$3 AND remote_fact_id=$4 AND period_start=$5 AND period_end=$6`, scope.OrganizationID(), scope.WorkspaceID(), fact.AccountID, fact.RemoteFactID, fact.PeriodStart, fact.PeriodEnd).Scan(&existing)
		if err == nil {
			if existing != fingerprint {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO advertising_spend_facts(organization_id,workspace_id,fact_id,account_id,channel,campaign_id,ad_id,sku,remote_fact_id,period_start,period_end,amount_minor,currency,source,observed_at,effective_at,quality,fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, scope.OrganizationID(), scope.WorkspaceID(), fact.ID, fact.AccountID, fact.Channel, fact.CampaignID, fact.AdID, fact.SKU, fact.RemoteFactID, fact.PeriodStart, fact.PeriodEnd, fact.AmountMinor, fact.Currency, fact.Source, fact.ObservedAt, fact.EffectiveAt, fact.Quality, fingerprint)
		return err
	})
}

// AppendPerformance stores an immutable normalized delivery fact idempotently.
func (r *Repository) AppendPerformance(ctx context.Context, scope tenancy.Scope, fact core.PerformanceFact) error {
	if err := r.validate(ctx, scope); err != nil || fact.Validate() != nil {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO advertising_performance_facts(organization_id,workspace_id,fact_id,account_id,channel,campaign_id,ad_id,sku,remote_fact_id,period_start,period_end,impressions,clicks,orders,revenue_minor,currency,source,observed_at,effective_at,quality) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) ON CONFLICT(organization_id,workspace_id,account_id,remote_fact_id,period_start,period_end) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), fact.ID, fact.AccountID, fact.Channel, fact.CampaignID, fact.AdID, fact.SKU, fact.RemoteFactID, fact.PeriodStart, fact.PeriodEnd, fact.Impressions, fact.Clicks, fact.Orders, fact.RevenueMinor, fact.Currency, fact.Source, fact.ObservedAt, fact.EffectiveAt, fact.Quality)
		return err
	})
}

// ListCampaigns returns bounded campaign projections.
func (r *Repository) ListCampaigns(ctx context.Context, scope tenancy.Scope, filter Filter) ([]core.Campaign, error) {
	if err := r.validateFilter(ctx, scope, filter); err != nil {
		return nil, err
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	items := make([]core.Campaign, 0, filter.Limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT campaign_id,account_id,channel,remote_id,name,status,currency,daily_budget_minor,total_budget_minor,observed_at,COALESCE(effective_from,'epoch'::timestamptz),COALESCE(effective_to,'epoch'::timestamptz),version FROM advertising_campaigns WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR channel=$3) AND ($4='' OR campaign_id=$4) ORDER BY updated_at DESC,campaign_id DESC LIMIT $5`, scope.OrganizationID(), scope.WorkspaceID(), filter.Channel, filter.CampaignID, filter.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c core.Campaign
			if err := rows.Scan(&c.ID, &c.AccountID, &c.Channel, &c.RemoteID, &c.Name, &c.Status, &c.Currency, &c.DailyBudgetMinor, &c.TotalBudgetMinor, &c.ObservedAt, &c.EffectiveFrom, &c.EffectiveTo, &c.Version); err != nil {
				return err
			}
			if c.EffectiveFrom.Unix() == 0 {
				c.EffectiveFrom = time.Time{}
			}
			if c.EffectiveTo.Unix() == 0 {
				c.EffectiveTo = time.Time{}
			}
			items = append(items, c)
		}
		return rows.Err()
	})
	return items, err
}

// ListSpend returns normalized spend facts for reporting and reconciliation.
func (r *Repository) ListSpend(ctx context.Context, scope tenancy.Scope, filter Filter) ([]core.SpendFact, error) {
	if err := r.validateFilter(ctx, scope, filter); err != nil {
		return nil, err
	}
	if filter.Limit == 0 {
		filter.Limit = 200
	}
	items := make([]core.SpendFact, 0, filter.Limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT fact_id,account_id,channel,campaign_id,ad_id,sku,remote_fact_id,period_start,period_end,amount_minor,currency,source,observed_at,effective_at,quality FROM advertising_spend_facts WHERE organization_id=$1 AND workspace_id=$2 AND ($3::timestamptz IS NULL OR period_start >= $3) AND ($4::timestamptz IS NULL OR period_end <= $4) AND ($5='' OR channel=$5) AND ($6='' OR campaign_id=$6) AND ($7='' OR sku=$7) ORDER BY period_start,fact_id LIMIT $8`, scope.OrganizationID(), scope.WorkspaceID(), nullableTime(filter.From), nullableTime(filter.To), filter.Channel, filter.CampaignID, filter.SKU, filter.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f core.SpendFact
			if err := rows.Scan(&f.ID, &f.AccountID, &f.Channel, &f.CampaignID, &f.AdID, &f.SKU, &f.RemoteFactID, &f.PeriodStart, &f.PeriodEnd, &f.AmountMinor, &f.Currency, &f.Source, &f.ObservedAt, &f.EffectiveAt, &f.Quality); err != nil {
				return err
			}
			items = append(items, f)
		}
		return rows.Err()
	})
	return items, err
}

// ListPerformance returns normalized delivery facts for reporting.
func (r *Repository) ListPerformance(ctx context.Context, scope tenancy.Scope, filter Filter) ([]core.PerformanceFact, error) {
	if err := r.validateFilter(ctx, scope, filter); err != nil {
		return nil, err
	}
	if filter.Limit == 0 {
		filter.Limit = 200
	}
	items := make([]core.PerformanceFact, 0, filter.Limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT fact_id,account_id,channel,campaign_id,ad_id,sku,remote_fact_id,period_start,period_end,impressions,clicks,orders,revenue_minor,currency,source,observed_at,effective_at,quality FROM advertising_performance_facts WHERE organization_id=$1 AND workspace_id=$2 AND ($3::timestamptz IS NULL OR period_start >= $3) AND ($4::timestamptz IS NULL OR period_end <= $4) AND ($5='' OR channel=$5) AND ($6='' OR campaign_id=$6) AND ($7='' OR sku=$7) ORDER BY period_start,fact_id LIMIT $8`, scope.OrganizationID(), scope.WorkspaceID(), nullableTime(filter.From), nullableTime(filter.To), filter.Channel, filter.CampaignID, filter.SKU, filter.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f core.PerformanceFact
			if err := rows.Scan(&f.ID, &f.AccountID, &f.Channel, &f.CampaignID, &f.AdID, &f.SKU, &f.RemoteFactID, &f.PeriodStart, &f.PeriodEnd, &f.Impressions, &f.Clicks, &f.Orders, &f.RevenueMinor, &f.Currency, &f.Source, &f.ObservedAt, &f.EffectiveAt, &f.Quality); err != nil {
				return err
			}
			items = append(items, f)
		}
		return rows.Err()
	})
	return items, err
}

// ListMetrics computes ROAS, ROMI and DRR from the authoritative normalized
// facts. It never substitutes missing values with a zero-quality success.
func (r *Repository) ListMetrics(ctx context.Context, scope tenancy.Scope, filter Filter) ([]core.Metric, error) {
	spends, err := r.ListSpend(ctx, scope, filter)
	if err != nil {
		return nil, err
	}
	performance, err := r.ListPerformance(ctx, scope, filter)
	if err != nil {
		return nil, err
	}
	return core.Aggregate(spends, performance)
}

// RecordFinding appends a reconciliation result.
func (r *Repository) RecordFinding(ctx context.Context, scope tenancy.Scope, finding core.Finding) error {
	if err := r.validate(ctx, scope); err != nil || !validFinding(finding) {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO advertising_reconciliation_findings(organization_id,workspace_id,finding_id,kind,campaign_id,remote_reference,expected_minor,actual_minor,severity,explanation,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(organization_id,workspace_id,finding_id) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), finding.ID, finding.Kind, finding.CampaignID, finding.RemoteReference, finding.ExpectedMinor, finding.ActualMinor, finding.Severity, finding.Explanation, finding.ObservedAt)
		return err
	})
}

// ListFindings returns bounded reconciliation evidence.
func (r *Repository) ListFindings(ctx context.Context, scope tenancy.Scope, limit int) ([]core.Finding, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	items := make([]core.Finding, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT finding_id,kind,campaign_id,remote_reference,expected_minor,actual_minor,severity,explanation,observed_at FROM advertising_reconciliation_findings WHERE organization_id=$1 AND workspace_id=$2 ORDER BY observed_at DESC,finding_id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f core.Finding
			if err := rows.Scan(&f.ID, &f.Kind, &f.CampaignID, &f.RemoteReference, &f.ExpectedMinor, &f.ActualMinor, &f.Severity, &f.Explanation, &f.ObservedAt); err != nil {
				return err
			}
			items = append(items, f)
		}
		return rows.Err()
	})
	return items, err
}

// StartSyncRun creates or returns the same period run, making worker retries
// safe. It is intentionally independent of the connector transport.
func (r *Repository) StartSyncRun(ctx context.Context, scope tenancy.Scope, run SyncRun) (SyncRun, error) {
	if err := r.validate(ctx, scope); err != nil || !validSyncRun(run) {
		return SyncRun{}, ErrInvalid
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.ID == "" {
		sum := sha256.Sum256([]byte(run.AccountID + "\x00" + run.Channel + "\x00" + run.From.Format(time.RFC3339) + "\x00" + run.To.Format(time.RFC3339) + "\x00" + run.Mode))
		run.ID = "adsrun-" + hex.EncodeToString(sum[:])[:24]
	}
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO advertising_sync_runs(organization_id,workspace_id,run_id,account_id,channel,from_at,to_at,mode,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'queued',$9) ON CONFLICT(organization_id,workspace_id,account_id,channel,from_at,to_at,mode) DO UPDATE SET run_id=advertising_sync_runs.run_id RETURNING run_id,created_at,status`, scope.OrganizationID(), scope.WorkspaceID(), run.ID, run.AccountID, run.Channel, run.From, run.To, run.Mode, run.CreatedAt).Scan(&run.ID, &run.CreatedAt, &run.Status)
	})
	return run, err
}

// UpdateSyncRun persists worker progress and never logs its cursor.
func (r *Repository) UpdateSyncRun(ctx context.Context, scope tenancy.Scope, run SyncRun) error {
	if err := r.validate(ctx, scope); err != nil || !validSyncRun(run) || run.ID == "" || len(run.NextCursor) > 4096 {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE advertising_sync_runs SET status=$4,next_cursor=$5,watermark_at=$6,fetched_count=$7,accepted_count=$8,rejected_count=$9,error_code=$10,evidence_digest=$11,completed_at=$12 WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), run.ID, run.Status, run.NextCursor, nullableTime(run.WatermarkAt), run.FetchedCount, run.AcceptedCount, run.RejectedCount, run.ErrorCode, nullableString(run.EvidenceDigest), nullableTime(run.CompletedAt))
		return err
	})
}

// ListSyncRuns returns bounded worker evidence for one tenant/workspace.
func (r *Repository) ListSyncRuns(ctx context.Context, scope tenancy.Scope, accountID string, limit int) ([]SyncRun, error) {
	if err := r.validate(ctx, scope); err != nil || len(accountID) > 192 || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	items := make([]SyncRun, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT run_id,account_id,channel,from_at,to_at,mode,status,next_cursor,COALESCE(watermark_at,'epoch'::timestamptz),fetched_count,accepted_count,rejected_count,error_code,COALESCE(evidence_digest,''),created_at,COALESCE(completed_at,'epoch'::timestamptz) FROM advertising_sync_runs WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR account_id=$3) ORDER BY created_at DESC,run_id DESC LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), accountID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run SyncRun
			if err := rows.Scan(&run.ID, &run.AccountID, &run.Channel, &run.From, &run.To, &run.Mode, &run.Status, &run.NextCursor, &run.WatermarkAt, &run.FetchedCount, &run.AcceptedCount, &run.RejectedCount, &run.ErrorCode, &run.EvidenceDigest, &run.CreatedAt, &run.CompletedAt); err != nil {
				return err
			}
			if run.WatermarkAt.Unix() == 0 {
				run.WatermarkAt = time.Time{}
			}
			if run.CompletedAt.Unix() == 0 {
				run.CompletedAt = time.Time{}
			}
			items = append(items, run)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) validateFilter(ctx context.Context, scope tenancy.Scope, f Filter) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if f.Limit < 0 || f.Limit > 200 || len(f.Channel) > 64 || len(f.CampaignID) > 192 || len(f.SKU) > 200 || (!f.From.IsZero() && f.From.Location() != time.UTC) || (!f.To.IsZero() && f.To.Location() != time.UTC) || (!f.From.IsZero() && !f.To.IsZero() && !f.To.After(f.From)) {
		return ErrInvalid
	}
	return nil
}
func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	opts := &sql.TxOptions{ReadOnly: readOnly}
	tx, err := r.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func validFinding(f core.Finding) bool {
	return f.ID != "" && f.Kind != "" && f.Severity != "" && len(f.Explanation) >= 1 && len(f.Explanation) <= 500 && !f.ObservedAt.IsZero() && f.ObservedAt.Location() == time.UTC
}
func validSyncRun(r SyncRun) bool {
	return r.AccountID != "" && r.Channel != "" && !r.From.IsZero() && !r.To.IsZero() && r.From.Location() == time.UTC && r.To.Location() == time.UTC && r.To.After(r.From) && (r.Mode == "daily" || r.Mode == "incremental" || r.Mode == "backfill") && (r.Status == "" || r.Status == "queued" || r.Status == "running" || r.Status == "completed" || r.Status == "partial" || r.Status == "failed" || r.Status == "dead_letter") && r.FetchedCount >= 0 && r.AcceptedCount >= 0 && r.RejectedCount >= 0
}
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
