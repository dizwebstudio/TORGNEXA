// Package uniteconomicsrepo persists immutable calculation-run metadata. The
// analytical rows themselves may live in ClickHouse, while this metadata and
// its digest remain tenant-scoped PostgreSQL evidence.
package uniteconomicsrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	core "github.com/torgnexa/torgnexa/internal/core/uniteconomics"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var (
	ErrInvalid  = errors.New("unit economics repository: invalid request")
	ErrConflict = errors.New("unit economics repository: run key conflict")
)

// Repository is a tenant-scoped immutable run metadata store.
type Repository struct{ db *sql.DB }

// New creates a repository over an existing PostgreSQL pool.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// SaveRun inserts snapshot metadata exactly once. Replaying the same run key
// with the same digest is idempotent; a digest mismatch is a hard conflict.
func (r *Repository) SaveRun(ctx context.Context, scope tenancy.Scope, snapshot core.Snapshot, status string, watermarks map[string]string) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() || snapshot.OrganizationID != scope.OrganizationID().String() || snapshot.WorkspaceID != scope.WorkspaceID().String() || snapshot.ID == "" || snapshot.InputDigest == "" || status == "" {
		return ErrInvalid
	}
	if snapshot.Basis.Valid() == false || snapshot.To.Before(snapshot.From) {
		return ErrInvalid
	}
	if len(watermarks) > 32 {
		return ErrInvalid
	}
	encoded, err := json.Marshal(watermarks)
	if err != nil {
		return ErrInvalid
	}
	if len(encoded) > 65536 {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("unit economics repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("unit economics repository: scope: %w", err)
	}
	runKey := snapshot.InputDigest + ":" + string(snapshot.Basis) + ":" + snapshot.From.UTC().Format(time.RFC3339Nano) + ":" + snapshot.To.UTC().Format(time.RFC3339Nano)
	if len(runKey) > 192 {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO unit_economics_calculation_runs(organization_id,workspace_id,run_id,run_key,basis,from_at,to_at,reporting_currency,algorithm_version,metric_definition_version,allocation_policy_version,valuation_policy_version,attribution_policy_version,input_digest,status,quality_status,row_count,source_watermarks,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) ON CONFLICT(organization_id,workspace_id,run_key) DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), snapshot.ID, runKey, snapshot.Basis, snapshot.From.UTC(), snapshot.To.UTC(), snapshot.ReportingCurrency, snapshot.AlgorithmVersion, snapshot.MetricDefinitionVersion, snapshot.AllocationPolicyVersion, snapshot.ValuationPolicyVersion, snapshot.AttributionPolicyVersion, snapshot.InputDigest, status, snapshot.QualityStatus, len(snapshot.Rows), encoded, snapshot.GeneratedAt.UTC())
	if err != nil {
		return fmt.Errorf("unit economics repository: insert run: %w", err)
	}
	var existingDigest string
	if err := tx.QueryRowContext(ctx, `SELECT input_digest FROM unit_economics_calculation_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_key=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), runKey).Scan(&existingDigest); err != nil {
		return fmt.Errorf("unit economics repository: read run: %w", err)
	}
	if existingDigest != snapshot.InputDigest {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("unit economics repository: commit: %w", err)
	}
	return nil
}
