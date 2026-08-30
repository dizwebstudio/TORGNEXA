// Package markingrepo persists the provider-neutral marking execution state.
package markingrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/marking"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository is the PostgreSQL adapter for marking execution.
type Repository struct{ db *sql.DB }

// BatchView is the safe operator projection. It contains no raw code values.
type BatchView struct {
	Batch      marking.CodeBatch `json:"batch"`
	CodeCount  int64             `json:"code_count"`
	OpenPrints int64             `json:"open_print_jobs"`
	OpenDrifts int64             `json:"open_drifts"`
}

// Overview is the bounded operator read model for the marking workspace.
type Overview struct {
	Batches        []BatchView `json:"batches"`
	OpenOperations int64       `json:"open_operations"`
	OpenDrifts     int64       `json:"open_drifts"`
}

// New constructs a tenant-scoped marking repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("marking repository: database is required")
	}
	return &Repository{db: db}, nil
}

// Overview returns a bounded read projection and never performs remote I/O.
func (r *Repository) Overview(ctx context.Context, scope tenancy.Scope, limit int) (Overview, error) {
	if r == nil || r.db == nil || !scope.Valid() || limit < 1 || limit > 100 {
		return Overview{}, marking.ErrInvalid
	}
	var result Overview
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT b.batch_id,b.organization_id,b.workspace_id,b.product_group,b.gtin,b.sku,b.requested_quantity,b.received_quantity,b.reserved_quantity,b.status,b.raw_artifact_ref,b.raw_artifact_expires_at,b.version,b.created_at,b.updated_at,(SELECT count(*) FROM marking_codes c WHERE c.organization_id=b.organization_id AND c.workspace_id=b.workspace_id AND c.batch_id=b.batch_id),(SELECT count(*) FROM marking_print_jobs p WHERE p.organization_id=b.organization_id AND p.workspace_id=b.workspace_id AND p.state IN ('queued','running','unknown')),(SELECT count(*) FROM marking_drifts d WHERE d.organization_id=b.organization_id AND d.workspace_id=b.workspace_id AND d.entity_ref=b.batch_id AND NOT d.resolved) FROM marking_code_batches b WHERE b.organization_id=$1 AND b.workspace_id=$2 ORDER BY b.updated_at DESC,b.batch_id DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return fmt.Errorf("marking repository: list batches: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var batch marking.CodeBatch
			var expires sql.NullTime
			var artifact string
			var codeCount, prints, drifts int64
			if err := rows.Scan(&batch.ID, &batch.OrganizationID, &batch.WorkspaceID, &batch.ProductGroup, &batch.GTIN, &batch.SKU, &batch.Requested, &batch.Received, &batch.Reserved, &batch.Status, &artifact, &expires, &batch.Version, &batch.CreatedAt, &batch.UpdatedAt, &codeCount, &prints, &drifts); err != nil {
				return fmt.Errorf("marking repository: scan batch: %w", err)
			}
			batch.RawArtifactRef = artifact
			if expires.Valid {
				batch.ExpiresAt = expires.Time.UTC()
			}
			if err := batch.Validate(); err != nil {
				return err
			}
			result.Batches = append(result.Batches, BatchView{Batch: batch, CodeCount: codeCount, OpenPrints: prints, OpenDrifts: drifts})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM marking_operations WHERE organization_id=$1 AND workspace_id=$2 AND state IN ('queued','running','unknown')`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&result.OpenOperations)
	})
	if err != nil {
		return Overview{}, err
	}
	for _, batch := range result.Batches {
		result.OpenDrifts += batch.OpenDrifts
	}
	return result, nil
}

// RecordScan fingerprints were created before this method is called. The
// method classifies unknown, mismatched, duplicate and overflow scans without
// exposing or storing the scanned plaintext.
func (r *Repository) RecordScan(ctx context.Context, scope tenancy.Scope, scan marking.Scan, expectedQuantity int64) (marking.Scan, error) {
	if r == nil || r.db == nil || !scope.Valid() || scan.Validate() != nil || expectedQuantity < 1 {
		return marking.Scan{}, marking.ErrInvalid
	}
	var result = scan
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var codeGTIN, codeSKU, status string
		err := tx.QueryRowContext(ctx, `SELECT gtin,sku,status FROM marking_codes WHERE organization_id=$1 AND workspace_id=$2 AND fingerprint=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), scan.Fingerprint).Scan(&codeGTIN, &codeSKU, &status)
		if errors.Is(err, sql.ErrNoRows) {
			result.Result, result.ReasonCode = marking.ScanRejected, "unknown_code"
		} else if err != nil {
			return fmt.Errorf("marking repository: find code: %w", err)
		} else if codeGTIN != scan.GTIN || codeSKU != scan.SKU {
			result.Result, result.ReasonCode = marking.ScanRejected, "gtin_or_sku_mismatch"
		} else {
			var alreadyScanned bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM marking_scans WHERE organization_id=$1 AND workspace_id=$2 AND fingerprint=$3 AND result='accepted')`, scope.OrganizationID().String(), scope.WorkspaceID().String(), scan.Fingerprint).Scan(&alreadyScanned); err != nil {
				return fmt.Errorf("marking repository: duplicate check: %w", err)
			}
			var accepted int64
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM marking_scans WHERE organization_id=$1 AND workspace_id=$2 AND wms_action=$3 AND result='accepted'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), scan.WMSAction).Scan(&accepted); err != nil {
				return fmt.Errorf("marking repository: quantity check: %w", err)
			}
			switch {
			case alreadyScanned:
				result.Result, result.ReasonCode = marking.ScanDuplicate, "code_already_scanned"
			case accepted >= expectedQuantity:
				result.Result, result.ReasonCode = marking.ScanOverflow, "quantity_overflow"
			default:
				result.Result, result.ReasonCode = marking.ScanAccepted, ""
			}
			if result.Result == marking.ScanAccepted {
				_, err = tx.ExecContext(ctx, `UPDATE marking_codes SET status=CASE WHEN status IN ('printed','available','reserved') THEN 'applied' ELSE status END,updated_at=$4,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND fingerprint=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), scan.Fingerprint, scan.OccurredAt)
				if err != nil {
					return fmt.Errorf("marking repository: apply code: %w", err)
				}
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marking_scans(organization_id,workspace_id,scan_id,fingerprint,gtin,sku,wms_action,result,reason_code,actor_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), result.ID, result.Fingerprint, result.GTIN, result.SKU, result.WMSAction, result.Result, result.ReasonCode, result.ActorID, result.OccurredAt)
		if err != nil {
			return fmt.Errorf("marking repository: record scan: %w", err)
		}
		return nil
	})
	return result, err
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	options := &sql.TxOptions{}
	if readOnly {
		options.ReadOnly = true
	}
	tx, err := r.db.BeginTx(ctx, options)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
