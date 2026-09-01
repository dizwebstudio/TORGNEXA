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

// BankAccountPage is a bounded tenant-scoped account page.
type BankAccountPage struct {
	Items []core.BankAccount
}

// COGSBackfillPage is a bounded remediation-job page.
type COGSBackfillPage struct {
	Items   []core.COGSBackfillJob
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

// AppendBankAccount creates a redacted bank account binding idempotently.
func (r *Repository) AppendBankAccount(ctx context.Context, scope tenancy.Scope, account core.BankAccount) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || account.Validate() != nil {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		_, err := tx.ExecContext(ctx, `INSERT INTO financial_bank_accounts(organization_id,workspace_id,account_id,provider,masked_reference,currency,status,secret_reference,next_cursor,last_observed_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,COALESCE(NULLIF($11::timestamptz,'0001-01-01 00:00:00+00'),clock_timestamp()),COALESCE(NULLIF($12::timestamptz,'0001-01-01 00:00:00+00'),clock_timestamp())) ON CONFLICT(organization_id,workspace_id,account_id) DO NOTHING`, org, workspace, account.ID, account.Provider, account.MaskedReference, account.Currency, account.Status, account.SecretReference, account.NextCursor, nullableTime(account.LastObservedAt), nullableTime(account.CreatedAt), nullableTime(account.UpdatedAt))
		if err != nil {
			return fmt.Errorf("financial completeness repository: append bank account: %w", err)
		}
		var storedSystem, masked, currency, secret string
		if err := tx.QueryRowContext(ctx, `SELECT provider,masked_reference,currency,secret_reference FROM financial_bank_accounts WHERE organization_id=$1 AND workspace_id=$2 AND account_id=$3`, org, workspace, account.ID).Scan(&storedSystem, &masked, &currency, &secret); err != nil {
			return fmt.Errorf("financial completeness repository: verify bank account: %w", err)
		}
		wantedSystem, wantedMasked, wantedCurrency, wantedSecret := account.Provider, account.MaskedReference, account.Currency, account.SecretReference
		if storedSystem != wantedSystem || masked != wantedMasked || currency != wantedCurrency || secret != wantedSecret {
			return ErrConflict
		}
		return nil
	})
}

// ListBankAccounts returns metadata only; the API redacts SecretReference.
func (r *Repository) ListBankAccounts(ctx context.Context, scope tenancy.Scope, limit int) (BankAccountPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || limit < 1 || limit > 100 {
		return BankAccountPage{}, ErrInvalid
	}
	var page BankAccountPage
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		rows, err := tx.QueryContext(ctx, `SELECT account_id,provider,masked_reference,currency,status,secret_reference,next_cursor,last_observed_at,created_at,updated_at FROM financial_bank_accounts WHERE organization_id=$1 AND workspace_id=$2 ORDER BY account_id LIMIT $3`, org, workspace, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		page.Items = make([]core.BankAccount, 0, limit)
		for rows.Next() {
			var account core.BankAccount
			var last sql.NullTime
			if err := rows.Scan(&account.ID, &account.Provider, &account.MaskedReference, &account.Currency, &account.Status, &account.SecretReference, &account.NextCursor, &last, &account.CreatedAt, &account.UpdatedAt); err != nil {
				return err
			}
			if last.Valid {
				account.LastObservedAt = last.Time.UTC()
			}
			account.CreatedAt, account.UpdatedAt = account.CreatedAt.UTC(), account.UpdatedAt.UTC()
			page.Items = append(page.Items, account)
		}
		return rows.Err()
	})
	return page, err
}

// AppendStatement commits an immutable statement manifest. Source transactions
// may then reference its ID through SourceRecord.StatementID.
func (r *Repository) AppendStatement(ctx context.Context, scope tenancy.Scope, statement core.BankStatement) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || statement.Validate() != nil {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		_, err := tx.ExecContext(ctx, `INSERT INTO financial_bank_statements(organization_id,workspace_id,statement_id,account_id,period_from,period_to,source_reference,source_digest,state,transaction_count,imported_count,rejected_count,opening_balance_minor_units,closing_balance_minor_units,reconciliation_ref,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,COALESCE(NULLIF($16::timestamptz,'0001-01-01 00:00:00+00'),clock_timestamp())) ON CONFLICT(organization_id,workspace_id,statement_id) DO NOTHING`, org, workspace, statement.ID, statement.AccountID, statement.PeriodFrom, statement.PeriodTo, statement.SourceReference, statement.SourceDigest, statement.State, statement.TransactionCount, statement.ImportedCount, statement.RejectedCount, statement.OpeningBalanceMinor, statement.ClosingBalanceMinor, nullableString(statement.ReconciliationRef), nullableTime(statement.CreatedAt))
		if err != nil {
			return fmt.Errorf("financial completeness repository: append statement: %w", err)
		}
		var digest string
		if err := tx.QueryRowContext(ctx, `SELECT source_digest FROM financial_bank_statements WHERE organization_id=$1 AND workspace_id=$2 AND statement_id=$3`, org, workspace, statement.ID).Scan(&digest); err != nil {
			return fmt.Errorf("financial completeness repository: verify statement: %w", err)
		}
		if digest != statement.SourceDigest {
			return ErrConflict
		}
		return nil
	})
}

// AppendCOGSBackfillJob records a bounded preview or queued remediation job.
func (r *Repository) AppendCOGSBackfillJob(ctx context.Context, scope tenancy.Scope, job core.COGSBackfillJob) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || job.Validate() != nil {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		_, err := tx.ExecContext(ctx, `INSERT INTO financial_cogs_backfill_jobs(organization_id,workspace_id,job_id,from_at,to_at,sku,warehouse_id,channel_ref,preview_digest,status,total_rows,valued_rows,missing_rows,created_at,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,COALESCE(NULLIF($14::timestamptz,'0001-01-01 00:00:00+00'),clock_timestamp()),$15) ON CONFLICT(organization_id,workspace_id,job_id) DO NOTHING`, org, workspace, job.ID, job.From, job.To, nullableString(job.SKU), nullableString(job.WarehouseID), nullableString(job.ChannelRef), job.PreviewDigest, job.Status, job.TotalRows, job.ValuedRows, job.MissingRows, nullableTime(job.CreatedAt), nullableTime(job.CompletedAt))
		if err != nil {
			return fmt.Errorf("financial completeness repository: append cogs backfill: %w", err)
		}
		var digest string
		if err := tx.QueryRowContext(ctx, `SELECT preview_digest FROM financial_cogs_backfill_jobs WHERE organization_id=$1 AND workspace_id=$2 AND job_id=$3`, org, workspace, job.ID).Scan(&digest); err != nil {
			return fmt.Errorf("financial completeness repository: verify cogs backfill: %w", err)
		}
		if digest != job.PreviewDigest {
			return ErrConflict
		}
		return nil
	})
}

// ListCOGSBackfillJobs returns bounded tenant-scoped remediation jobs.
func (r *Repository) ListCOGSBackfillJobs(ctx context.Context, scope tenancy.Scope, afterID string, limit int) (COGSBackfillPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || limit < 1 || limit > 100 || len(afterID) > 192 {
		return COGSBackfillPage{}, ErrInvalid
	}
	var page COGSBackfillPage
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		rows, err := tx.QueryContext(ctx, `SELECT job_id,from_at,to_at,sku,warehouse_id,channel_ref,preview_digest,status,total_rows,valued_rows,missing_rows,created_at,completed_at FROM financial_cogs_backfill_jobs WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR job_id>$3) ORDER BY job_id LIMIT $4`, org, workspace, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		page.Items = make([]core.COGSBackfillJob, 0, limit)
		for rows.Next() {
			var job core.COGSBackfillJob
			var completed sql.NullTime
			if err := rows.Scan(&job.ID, &job.From, &job.To, &job.SKU, &job.WarehouseID, &job.ChannelRef, &job.PreviewDigest, &job.Status, &job.TotalRows, &job.ValuedRows, &job.MissingRows, &job.CreatedAt, &completed); err != nil {
				return err
			}
			job.From, job.To, job.CreatedAt = job.From.UTC(), job.To.UTC(), job.CreatedAt.UTC()
			if completed.Valid {
				job.CompletedAt = completed.Time.UTC()
			}
			page.Items = append(page.Items, job)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page.Items) > limit {
			page.HasMore = true
			page.Items = page.Items[:limit]
		}
		return nil
	})
	return page, err
}

// AppendSource inserts one immutable, redacted source fact idempotently.
func (r *Repository) AppendSource(ctx context.Context, scope tenancy.Scope, record core.SourceRecord) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() {
		return ErrInvalid
	}
	if err := record.Validate(); err != nil {
		return err
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		_, err := tx.ExecContext(ctx, `INSERT INTO financial_source_records(organization_id,workspace_id,record_id,kind,source_system,account_ref,source_ref,statement_id,order_id,payout_id,sku,campaign_id,attribution_status,amount_minor_units,currency,state,quality,occurred_at,posted_at,source_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,COALESCE(NULLIF($21::timestamptz,'0001-01-01 00:00:00+00'),clock_timestamp())) ON CONFLICT(organization_id,workspace_id,kind,source_system,account_ref,source_ref) DO NOTHING`, org, workspace, record.ID, record.Kind, record.SourceSystem, record.AccountRef, record.SourceRef, nullableString(record.StatementID), nullableString(record.OrderID), nullableString(record.PayoutID), nullableString(record.SKU), nullableString(record.CampaignID), nullableString(record.AttributionStatus), record.AmountMinor, record.Currency, record.State, record.Quality, record.OccurredAt, nullableTime(record.PostedAt), record.SourceDigest, nullableTime(record.CreatedAt))
		if err != nil {
			return fmt.Errorf("financial completeness repository: append source: %w", err)
		}
		var amount int64
		var currency, digest string
		if err := tx.QueryRowContext(ctx, `SELECT amount_minor_units,currency,source_digest FROM financial_source_records WHERE organization_id=$1 AND workspace_id=$2 AND kind=$3 AND source_system=$4 AND account_ref=$5 AND source_ref=$6`, org, workspace, record.Kind, record.SourceSystem, record.AccountRef, record.SourceRef).Scan(&amount, &currency, &digest); err != nil {
			return fmt.Errorf("financial completeness repository: verify source: %w", err)
		}
		if amount != record.AmountMinor || currency != record.Currency || digest != record.SourceDigest {
			return ErrConflict
		}
		return nil
	})
}

// ListSources returns redacted evidence in stable record-id order.
func (r *Repository) ListSources(ctx context.Context, scope tenancy.Scope, filter Filter) (SourcePage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || filter.Limit < 1 || filter.Limit > 200 || filter.AfterID != "" && len(filter.AfterID) > 192 || !validTimeRange(filter.From, filter.To) {
		return SourcePage{}, ErrInvalid
	}
	var page SourcePage
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		query := `SELECT record_id,kind,source_system,account_ref,source_ref,statement_id,order_id,payout_id,sku,campaign_id,attribution_status,amount_minor_units,currency,state,quality,occurred_at,posted_at,source_digest,created_at FROM financial_source_records WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR kind=$3) AND ($4='' OR quality=$4) AND ($5='' OR record_id>$5) AND ($6::timestamptz IS NULL OR occurred_at >= $6) AND ($7::timestamptz IS NULL OR occurred_at < $7) ORDER BY record_id LIMIT $8`
		rows, err := tx.QueryContext(ctx, query, org, workspace, filter.Kind, filter.Quality, filter.AfterID, nullableTime(filter.From), nullableTime(filter.To), filter.Limit+1)
		if err != nil {
			return fmt.Errorf("financial completeness repository: list sources: %w", err)
		}
		defer rows.Close()
		page = SourcePage{Items: make([]core.SourceRecord, 0, filter.Limit)}
		for rows.Next() {
			var item core.SourceRecord
			var kind, quality string
			var posted, created sql.NullTime
			var statementID, orderID, payoutID, sku, campaignID, attributionStatus sql.NullString
			if err := rows.Scan(&item.ID, &kind, &item.SourceSystem, &item.AccountRef, &item.SourceRef, &statementID, &orderID, &payoutID, &sku, &campaignID, &attributionStatus, &item.AmountMinor, &item.Currency, &item.State, &quality, &item.OccurredAt, &posted, &item.SourceDigest, &created); err != nil {
				return fmt.Errorf("financial completeness repository: scan source: %w", err)
			}
			item.StatementID, item.OrderID, item.PayoutID = statementID.String, orderID.String, payoutID.String
			item.SKU, item.CampaignID, item.AttributionStatus = sku.String, campaignID.String, attributionStatus.String
			item.Kind, item.Quality = core.SourceKind(kind), core.Quality(quality)
			if posted.Valid {
				item.PostedAt = posted.Time.UTC()
			}
			if created.Valid {
				item.CreatedAt = created.Time.UTC()
			}
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page.Items) > filter.Limit {
			page.HasMore = true
			page.Items = page.Items[:filter.Limit]
		}
		return nil
	})
	return page, err
}

// ListFindings returns the tenant's reconciliation queue.
func (r *Repository) ListFindings(ctx context.Context, scope tenancy.Scope, afterID string, limit int) (FindingPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || limit < 1 || limit > 200 || len(afterID) > 192 {
		return FindingPage{}, ErrInvalid
	}
	var page FindingPage
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		rows, err := tx.QueryContext(ctx, `SELECT finding_id,kind,subject_ref,expected_minor_units,observed_minor_units,currency,severity,status,explanation,owner_ref,detected_at,resolved_at FROM financial_completeness_findings WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR finding_id>$3) ORDER BY finding_id LIMIT $4`, org, workspace, afterID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		page = FindingPage{Items: make([]core.Finding, 0, limit)}
		for rows.Next() {
			var item core.Finding
			var resolved sql.NullTime
			if err := rows.Scan(&item.ID, &item.Kind, &item.SubjectRef, &item.ExpectedMinor, &item.ObservedMinor, &item.Currency, &item.Severity, &item.Status, &item.Explanation, &item.OwnerRef, &item.DetectedAt, &resolved); err != nil {
				return err
			}
			if resolved.Valid {
				item.ResolvedAt = resolved.Time.UTC()
			}
			item.DetectedAt = item.DetectedAt.UTC()
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page.Items) > limit {
			page.HasMore = true
			page.Items = page.Items[:limit]
		}
		return nil
	})
	return page, err
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
	var findings int
	if err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(occurred_at),'0001-01-01'::timestamptz) FROM financial_source_records WHERE organization_id=$1 AND workspace_id=$2 AND occurred_at >= $3 AND occurred_at < $4`, org, workspace, from, to).Scan(&open, &last); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM financial_completeness_findings WHERE organization_id=$1 AND workspace_id=$2 AND status<>'resolved'`, org, workspace).Scan(&findings)
	}); err != nil {
		return Summary{}, err
	}
	return Summary{Matrix: core.Matrix(), Evaluation: evaluation, SourceCount: open, OpenFindingCount: findings, LastObservedAt: last.UTC()}, nil
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func validTimeRange(from, to time.Time) bool {
	return (from.IsZero() || from.Location() == time.UTC) && (to.IsZero() || to.Location() == time.UTC) && (from.IsZero() || to.IsZero() || to.After(from))
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
