// Package publicationqualityrepo persists tenant-scoped publication quality
// evidence. It deliberately stores derived facts and never performs connector
// calls or writes canonical catalog data.
package publicationqualityrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/publicationquality"
)

var (
	ErrNotFound = errors.New("publication quality repository: not found")
	ErrConflict = errors.New("publication quality repository: conflict")
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository is a tenant-scoped PostgreSQL adapter for quality evidence.
type Repository struct{ db *sql.DB }

// RunSummary is the bounded list projection used by the operator API.
type RunSummary struct {
	ID                 string
	ProductID          string
	OfferID            string
	ConnectorAccountID string
	ConnectorID        string
	ChannelFamily      string
	Decision           publicationquality.Decision
	ScoreBPS           int64
	EvaluatedAt        time.Time
	ValidUntil         time.Time
	Version            int64
}

// CurrentReceipt returns the newest unexpired receipt for a concrete product
// publication target. The caller still performs the final in-memory exact
// snapshot check when it has assembled the current source facts.
func (r *Repository) CurrentReceipt(ctx context.Context, scope tenancy.Scope, productID, accountID, connectorID string, productVersion int64, now time.Time) (publicationquality.PublicationGateReceipt, error) {
	a, b, c := productID, accountID, connectorID
	if err := validate(ctx, r, scope); err != nil || a == "" || b == "" || c == "" || productVersion < 1 || now.IsZero() {
		return publicationquality.PublicationGateReceipt{}, publicationquality.ErrInvalid
	}
	var receipt publicationquality.PublicationGateReceipt
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var candidateCount int64
		err := tx.QueryRowContext(ctx, `SELECT receipt_id,run_id,product_id,offer_id,connector_account_id,connector_id,channel_family,locale,jurisdiction,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version,snapshot_digest,profile_digest,compliance_fingerprint,decision,issued_at,valid_until,version,count(*) OVER() FROM publication_quality_gate_receipts WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 AND connector_account_id=$4 AND connector_id=$5 AND product_version=$6 AND valid_until>$7 ORDER BY valid_until DESC,receipt_id DESC LIMIT 1`, scope.OrganizationID(), scope.WorkspaceID(), productID, accountID, connectorID, productVersion, now.UTC()).Scan(&receipt.ID, &receipt.RunID, &receipt.Target.ProductID, &receipt.Target.OfferID, &receipt.Target.ConnectorAccountID, &receipt.Target.ConnectorID, &receipt.Target.ChannelFamily, &receipt.Target.Locale, &receipt.Target.Jurisdiction, &receipt.ProductVersion, &receipt.OfferVersion, &receipt.PriceVersion, &receipt.InventoryVersion, &receipt.MediaVersion, &receipt.MappingVersion, &receipt.CapabilityVersion, &receipt.SnapshotDigest, &receipt.ProfileDigest, &receipt.ComplianceFingerprint, &receipt.Decision, &receipt.IssuedAt, &receipt.ValidUntil, &receipt.Version, &candidateCount)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if candidateCount != 1 {
			return ErrConflict
		}
		receipt.Target.OrganizationID = scope.OrganizationID().String()
		receipt.Target.WorkspaceID = scope.WorkspaceID().String()
		receipt.IssuedAt = receipt.IssuedAt.UTC()
		receipt.ValidUntil = receipt.ValidUntil.UTC()
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return publicationquality.PublicationGateReceipt{}, publicationquality.ErrNotFound
	}
	if err != nil {
		return publicationquality.PublicationGateReceipt{}, err
	}
	return receipt, nil
}

// New constructs a repository backed by db.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("publication quality repository: database is required")
	}
	return &Repository{db: db}, nil
}

// SaveEvaluation atomically stores a completed run, its bounded issues and an
// optional successful gate receipt. A run ID is idempotent only when all
// immutable digests and target versions are identical.
func (r *Repository) SaveEvaluation(ctx context.Context, scope tenancy.Scope, run publicationquality.QualityRun, receipt publicationquality.PublicationGateReceipt) error {
	if err := validate(ctx, r, scope); err != nil {
		return err
	}
	if err := run.Validate(); err != nil {
		return publicationquality.ErrInvalid
	}
	if receipt.ID != "" {
		if err := receipt.Validate(); err != nil {
			return publicationquality.ErrInvalid
		}
		if receipt.Target.OrganizationID != scope.OrganizationID().String() || receipt.Target.WorkspaceID != scope.WorkspaceID().String() || receipt.RunID != run.ID || !sameTarget(receipt.Target, run.Target) || receipt.ProductVersion != run.ProductVersion || receipt.OfferVersion != run.OfferVersion || receipt.PriceVersion != run.PriceVersion || receipt.InventoryVersion != run.InventoryVersion || receipt.MediaVersion != run.MediaVersion || receipt.MappingVersion != run.MappingVersion || receipt.CapabilityVersion != run.CapabilityVersion || receipt.SnapshotDigest != run.SnapshotDigest || receipt.ProfileDigest != run.ProfileDigest || receipt.ComplianceFingerprint != run.ComplianceFingerprint || receipt.Decision != run.Decision {
			return publicationquality.ErrInvalid
		}
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var existingDigest, existingProfile, existingCompliance string
		var existingVersions [7]int64
		var existingProductID, existingOfferID, existingAccountID, existingTargetID, existingFamily, existingLocale, existingJurisdiction string
		err := tx.QueryRowContext(ctx, `SELECT snapshot_digest,profile_digest,compliance_fingerprint,product_id,offer_id,connector_account_id,connector_id,channel_family,locale,jurisdiction,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version FROM publication_quality_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), run.ID).Scan(&existingDigest, &existingProfile, &existingCompliance, &existingProductID, &existingOfferID, &existingAccountID, &existingTargetID, &existingFamily, &existingLocale, &existingJurisdiction, &existingVersions[0], &existingVersions[1], &existingVersions[2], &existingVersions[3], &existingVersions[4], &existingVersions[5], &existingVersions[6])
		existingTarget := publicationquality.Target{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ProductID: existingProductID, OfferID: existingOfferID, ConnectorAccountID: existingAccountID, ConnectorID: existingTargetID, ChannelFamily: existingFamily, Locale: existingLocale, Jurisdiction: existingJurisdiction}
		targetMatches := targetKey(existingTarget) == targetKey(run.Target)
		switch {
		case err == nil:
			if existingDigest != run.SnapshotDigest || existingProfile != run.ProfileDigest || existingCompliance != run.ComplianceFingerprint || !targetMatches || existingVersions != [7]int64{run.ProductVersion, run.OfferVersion, run.PriceVersion, run.InventoryVersion, run.MediaVersion, run.MappingVersion, run.CapabilityVersion} {
				return ErrConflict
			}
		case errors.Is(err, sql.ErrNoRows):
			if err := insertRun(ctx, tx, scope, run); err != nil {
				return err
			}
			if err := insertIssues(ctx, tx, scope, run); err != nil {
				return err
			}
		default:
			return fmt.Errorf("publication quality repository: lookup run: %w", err)
		}
		if receipt.ID != "" {
			if err := insertReceipt(ctx, tx, scope, receipt); err != nil {
				return err
			}
		}
		return nil
	})
}

func sameTarget(left, right publicationquality.Target) bool {
	return targetKey(left) == targetKey(right)
}

func targetKey(target publicationquality.Target) string {
	return strings.Join([]string{target.OrganizationID, target.WorkspaceID, target.ProductID, target.OfferID, target.ConnectorAccountID, target.ConnectorID, target.ChannelFamily, target.Locale, target.Jurisdiction}, "\x00")
}

// Run returns a run and its immutable issue evidence in deterministic order.
func (r *Repository) Run(ctx context.Context, scope tenancy.Scope, id string) (publicationquality.QualityRun, error) {
	if err := validate(ctx, r, scope); err != nil || id == "" {
		return publicationquality.QualityRun{}, publicationquality.ErrInvalid
	}
	var run publicationquality.QualityRun
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var categoryJSON []byte
		var evaluatedAt, validUntil sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT run_id,product_id,offer_id,connector_account_id,connector_id,channel_family,locale,jurisdiction,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version,snapshot_digest,profile_digest,compliance_fingerprint,status,decision,score_bps,category_scores,evaluated_at,valid_until,version FROM publication_quality_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&run.ID, &run.Target.ProductID, &run.Target.OfferID, &run.Target.ConnectorAccountID, &run.Target.ConnectorID, &run.Target.ChannelFamily, &run.Target.Locale, &run.Target.Jurisdiction, &run.ProductVersion, &run.OfferVersion, &run.PriceVersion, &run.InventoryVersion, &run.MappingVersion, &run.CapabilityVersion, &run.SnapshotDigest, &run.ProfileDigest, &run.ComplianceFingerprint, &run.Status, &run.Decision, &run.ScoreBPS, &categoryJSON, &evaluatedAt, &validUntil, &run.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("publication quality repository: scan run: %w", err)
		}
		run.Target.OrganizationID = scope.OrganizationID().String()
		run.Target.WorkspaceID = scope.WorkspaceID().String()
		if evaluatedAt.Valid {
			run.EvaluatedAt = evaluatedAt.Time.UTC()
		}
		if validUntil.Valid {
			run.ValidUntil = validUntil.Time.UTC()
		}
		if err := json.Unmarshal(categoryJSON, &run.CategoryScoresBPS); err != nil {
			return fmt.Errorf("publication quality repository: decode category scores: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT code,category,severity,field_path,message,expected,observed,remediation,source_ref FROM publication_quality_issues WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 ORDER BY issue_id`, scope.OrganizationID(), scope.WorkspaceID(), id)
		if err != nil {
			return fmt.Errorf("publication quality repository: list issues: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var issue publicationquality.Issue
			if err := rows.Scan(&issue.Code, &issue.Category, &issue.Severity, &issue.FieldPath, &issue.Message, &issue.Expected, &issue.Observed, &issue.Remediation, &issue.SourceRef); err != nil {
				return fmt.Errorf("publication quality repository: scan issue: %w", err)
			}
			run.Issues = append(run.Issues, issue)
		}
		return rows.Err()
	})
	if errors.Is(err, ErrNotFound) {
		return publicationquality.QualityRun{}, publicationquality.ErrNotFound
	}
	return run, err
}

// ListRuns returns the newest bounded quality decisions for one tenant.
func (r *Repository) ListRuns(ctx context.Context, scope tenancy.Scope, productID string, limit int) ([]RunSummary, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 100 || len(productID) > 192 {
		return nil, publicationquality.ErrInvalid
	}
	items := make([]RunSummary, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT run_id,product_id,offer_id,connector_account_id,connector_id,channel_family,decision,score_bps,COALESCE(evaluated_at,created_at),COALESCE(valid_until,created_at),version FROM publication_quality_runs WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR product_id=$3) ORDER BY updated_at DESC,run_id DESC LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), productID, limit)
		if err != nil {
			return fmt.Errorf("publication quality repository: list runs: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item RunSummary
			if err := rows.Scan(&item.ID, &item.ProductID, &item.OfferID, &item.ConnectorAccountID, &item.ConnectorID, &item.ChannelFamily, &item.Decision, &item.ScoreBPS, &item.EvaluatedAt, &item.ValidUntil, &item.Version); err != nil {
				return fmt.Errorf("publication quality repository: scan run summary: %w", err)
			}
			item.EvaluatedAt = item.EvaluatedAt.UTC()
			item.ValidUntil = item.ValidUntil.UTC()
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// Receipt returns a tenant-scoped gate receipt by ID.
func (r *Repository) Receipt(ctx context.Context, scope tenancy.Scope, id string) (publicationquality.PublicationGateReceipt, error) {
	if err := validate(ctx, r, scope); err != nil || id == "" {
		return publicationquality.PublicationGateReceipt{}, publicationquality.ErrInvalid
	}
	var receipt publicationquality.PublicationGateReceipt
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT receipt_id,run_id,product_id,offer_id,connector_account_id,connector_id,channel_family,locale,jurisdiction,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version,snapshot_digest,profile_digest,compliance_fingerprint,decision,issued_at,valid_until,version FROM publication_quality_gate_receipts WHERE organization_id=$1 AND workspace_id=$2 AND receipt_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&receipt.ID, &receipt.RunID, &receipt.Target.ProductID, &receipt.Target.OfferID, &receipt.Target.ConnectorAccountID, &receipt.Target.ConnectorID, &receipt.Target.ChannelFamily, &receipt.Target.Locale, &receipt.Target.Jurisdiction, &receipt.ProductVersion, &receipt.OfferVersion, &receipt.PriceVersion, &receipt.InventoryVersion, &receipt.MediaVersion, &receipt.MappingVersion, &receipt.CapabilityVersion, &receipt.SnapshotDigest, &receipt.ProfileDigest, &receipt.ComplianceFingerprint, &receipt.Decision, &receipt.IssuedAt, &receipt.ValidUntil, &receipt.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		receipt.Target.OrganizationID = scope.OrganizationID().String()
		receipt.Target.WorkspaceID = scope.WorkspaceID().String()
		receipt.IssuedAt = receipt.IssuedAt.UTC()
		receipt.ValidUntil = receipt.ValidUntil.UTC()
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return publicationquality.PublicationGateReceipt{}, publicationquality.ErrNotFound
	}
	return receipt, err
}

// SaveRemediation records a bounded remediation proposal. It is idempotent by
// remediation ID and deliberately has no method that applies catalog changes.
func (r *Repository) SaveRemediation(ctx context.Context, scope tenancy.Scope, action publicationquality.RemediationAction) error {
	if err := validate(ctx, r, scope); err != nil || action.Validate() != nil {
		return publicationquality.ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var existingExpected, existingDiff, existingStatus string
		err := tx.QueryRowContext(ctx, `SELECT expected_snapshot_digest,proposed_diff_digest,status FROM publication_quality_remediations WHERE organization_id=$1 AND workspace_id=$2 AND remediation_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), action.ID).Scan(&existingExpected, &existingDiff, &existingStatus)
		switch {
		case err == nil:
			if existingExpected != action.ExpectedSnapshotDigest || existingDiff != action.ProposedDiffDigest || existingStatus != action.Status {
				return ErrConflict
			}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("publication quality repository: lookup remediation: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO publication_quality_remediations(organization_id,workspace_id,remediation_id,run_id,issue_id,action_code,status,expected_snapshot_digest,proposed_diff_digest,approval_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, scope.OrganizationID(), scope.WorkspaceID(), action.ID, action.RunID, action.IssueCode, action.ActionCode, action.Status, action.ExpectedSnapshotDigest, action.ProposedDiffDigest, action.ApprovalID, action.CreatedAt)
		if err != nil {
			return fmt.Errorf("publication quality repository: insert remediation: %w", err)
		}
		return nil
	})
}

func insertRun(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, run publicationquality.QualityRun) error {
	categoryJSON, err := json.Marshal(run.CategoryScoresBPS)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO publication_quality_runs(organization_id,workspace_id,run_id,product_id,offer_id,connector_account_id,connector_id,channel_family,locale,jurisdiction,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version,snapshot_digest,profile_digest,compliance_fingerprint,status,decision,score_bps,category_scores,evaluated_at,valid_until,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`, scope.OrganizationID(), scope.WorkspaceID(), run.ID, run.Target.ProductID, run.Target.OfferID, run.Target.ConnectorAccountID, run.Target.ConnectorID, run.Target.ChannelFamily, run.Target.Locale, run.Target.Jurisdiction, run.ProductVersion, run.OfferVersion, run.PriceVersion, run.InventoryVersion, run.MediaVersion, run.MappingVersion, run.CapabilityVersion, run.SnapshotDigest, run.ProfileDigest, run.ComplianceFingerprint, run.Status, run.Decision, run.ScoreBPS, categoryJSON, run.EvaluatedAt, run.ValidUntil, run.Version)
	if err != nil {
		return fmt.Errorf("publication quality repository: insert run: %w", err)
	}
	return nil
}

func insertIssues(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, run publicationquality.QualityRun) error {
	for index, issue := range run.Issues {
		issueID := fmt.Sprintf("%04d-%s", index+1, issue.Code)
		if _, err := tx.ExecContext(ctx, `INSERT INTO publication_quality_issues(organization_id,workspace_id,run_id,issue_id,code,category,severity,field_path,message,expected,observed,remediation,source_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, scope.OrganizationID(), scope.WorkspaceID(), run.ID, issueID, issue.Code, issue.Category, issue.Severity, issue.FieldPath, issue.Message, issue.Expected, issue.Observed, issue.Remediation, issue.SourceRef); err != nil {
			return fmt.Errorf("publication quality repository: insert issue: %w", err)
		}
	}
	return nil
}

func insertReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, receipt publicationquality.PublicationGateReceipt) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO publication_quality_gate_receipts(organization_id,workspace_id,receipt_id,run_id,product_id,offer_id,connector_account_id,connector_id,channel_family,locale,jurisdiction,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version,snapshot_digest,profile_digest,compliance_fingerprint,decision,issued_at,valid_until,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25) ON CONFLICT (organization_id,workspace_id,receipt_id) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), receipt.ID, receipt.RunID, receipt.Target.ProductID, receipt.Target.OfferID, receipt.Target.ConnectorAccountID, receipt.Target.ConnectorID, receipt.Target.ChannelFamily, receipt.Target.Locale, receipt.Target.Jurisdiction, receipt.ProductVersion, receipt.OfferVersion, receipt.PriceVersion, receipt.InventoryVersion, receipt.MediaVersion, receipt.MappingVersion, receipt.CapabilityVersion, receipt.SnapshotDigest, receipt.ProfileDigest, receipt.ComplianceFingerprint, receipt.Decision, receipt.IssuedAt, receipt.ValidUntil, receipt.Version)
	if err != nil {
		return fmt.Errorf("publication quality repository: insert receipt: %w", err)
	}
	return nil
}

func validate(ctx context.Context, r *Repository, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return publicationquality.ErrInvalid
	}
	return nil
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("publication quality repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()); err != nil {
		return fmt.Errorf("publication quality repository: apply tenant scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("publication quality repository: commit: %w", err)
	}
	return nil
}
