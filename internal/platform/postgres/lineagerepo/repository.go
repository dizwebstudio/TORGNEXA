// Package lineagerepo implements immutable tenant-scoped PostgreSQL lineage storage.
package lineagerepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/lineage"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ database *sql.DB }

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("lineage repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (r *Repository) Append(ctx context.Context, scope lineage.Scope, record lineage.Record) error {
	if err := validate(ctx, r, scope, record); err != nil {
		return err
	}
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("lineage repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := applyTenant(ctx, tx, scope); err != nil {
		return err
	}
	if err := AppendTransaction(ctx, tx, scope, record); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("lineage repository: commit: %w", err)
	}
	return nil
}

// AppendTransaction appends lineage inside an existing domain transaction.
func AppendTransaction(ctx context.Context, tx *sql.Tx, scope lineage.Scope, record lineage.Record) error {
	if ctx == nil || tx == nil || !scope.Valid() || record.Validate() != nil || record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() {
		return lineage.ErrInvalid
	}
	var o, w string
	if err := tx.QueryRowContext(ctx, `SELECT current_setting('app.organization_id',true), current_setting('app.workspace_id',true)`).Scan(&o, &w); err != nil {
		return err
	}
	if o != scope.OrganizationID() || w != scope.WorkspaceID() {
		return lineage.ErrInvalid
	}
	fingerprint, err := lineage.FingerprintSHA256(record)
	if err != nil {
		return err
	}
	observed := any(nil)
	if record.Output.ObservedAt != nil {
		observed = *record.Output.ObservedAt
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO lineage_records(
		id,organization_id,workspace_id,source,actor_id,operation,
		output_system,output_entity_type,output_entity_id,output_entity_version,output_field,output_observed_at,
		transform_kind,transform_id,transform_version,mapping_id,rule_id,
		correlation_id,causation_id,audit_id,event_id,result,fingerprint_sha256,occurred_at)
	VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14,$15,NULLIF($16,''),NULLIF($17,''),$18,NULLIF($19,''),$20,$21,$22,$23,$24)
	ON CONFLICT (id) DO NOTHING`, record.ID, record.OrganizationID, record.WorkspaceID, record.Source, record.ActorID, record.Operation,
		record.Output.System, record.Output.EntityType, record.Output.EntityID, record.Output.Version, record.Output.Field, observed,
		record.Transformation.Kind, record.Transformation.ID, record.Transformation.Version, record.Transformation.MappingID, record.Transformation.RuleID,
		record.CorrelationID, record.CausationID, record.AuditID, record.EventID, string(record.Result), fingerprint, record.OccurredAt)
	if err != nil {
		return fmt.Errorf("lineage repository: insert record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var stored string
		err = tx.QueryRowContext(ctx, `SELECT fingerprint_sha256 FROM lineage_records WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), record.ID).Scan(&stored)
		if err != nil || stored != fingerprint {
			return fmt.Errorf("lineage repository: id collision")
		}
		return nil
	}
	for i, input := range record.Inputs {
		var inputObserved any
		if input.Ref.ObservedAt != nil {
			inputObserved = *input.Ref.ObservedAt
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO lineage_inputs(organization_id,workspace_id,record_id,position,role,source_system,source_entity_type,source_entity_id,source_entity_version,source_field,source_observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11)`, scope.OrganizationID(), scope.WorkspaceID(), record.ID, i+1, input.Role, input.Ref.System, input.Ref.EntityType, input.Ref.EntityID, input.Ref.Version, input.Ref.Field, inputObserved); err != nil {
			return fmt.Errorf("lineage repository: insert input: %w", err)
		}
	}
	return nil
}

func (r *Repository) Timeline(ctx context.Context, scope lineage.Scope, query lineage.TimelineQuery) (lineage.TimelinePage, error) {
	if ctx == nil || r == nil || r.database == nil || !scope.Valid() || query.Validate() != nil {
		return lineage.TimelinePage{}, lineage.ErrInvalid
	}
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return lineage.TimelinePage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = applyTenant(ctx, tx, scope); err != nil {
		return lineage.TimelinePage{}, err
	}
	args := []any{scope.OrganizationID(), scope.WorkspaceID(), query.System, query.EntityType, query.EntityID, query.Field, query.Limit + 1}
	cursorClause := ""
	if query.BeforeAt != nil {
		cursorClause = " AND (occurred_at,id) < ($8,$9)"
		args = append(args, *query.BeforeAt, query.BeforeID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,source,COALESCE(actor_id,''),operation,output_system,output_entity_type,output_entity_id,COALESCE(output_entity_version,''),COALESCE(output_field,''),output_observed_at,transform_kind,transform_id,transform_version,COALESCE(mapping_id,''),COALESCE(rule_id,''),correlation_id,COALESCE(causation_id,''),audit_id,event_id,result,occurred_at FROM lineage_records WHERE organization_id=$1 AND workspace_id=$2 AND output_system=$3 AND output_entity_type=$4 AND output_entity_id=$5 AND ($6='' OR output_field=$6)`+cursorClause+` ORDER BY occurred_at DESC,id DESC LIMIT $7`, args...)
	if err != nil {
		return lineage.TimelinePage{}, err
	}
	defer rows.Close()
	items := make([]lineage.Record, 0, query.Limit+1)
	for rows.Next() {
		rec, e := scanRecord(rows)
		if e != nil {
			return lineage.TimelinePage{}, e
		}
		inputs, e := loadInputs(ctx, tx, scope, rec.ID)
		if e != nil {
			return lineage.TimelinePage{}, e
		}
		rec.Inputs = inputs
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return lineage.TimelinePage{}, err
	}
	page := lineage.TimelinePage{}
	if len(items) > query.Limit {
		last := items[query.Limit-1]
		page.NextCursor = &lineage.Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
		items = items[:query.Limit]
	}
	page.Items = items
	if err := tx.Commit(); err != nil {
		return lineage.TimelinePage{}, err
	}
	return page, nil
}

func applyTenant(ctx context.Context, tx *sql.Tx, scope lineage.Scope) error {
	var o, w string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()).Scan(&o, &w); err != nil {
		return err
	}
	if o != scope.OrganizationID() || w != scope.WorkspaceID() {
		return lineage.ErrInvalid
	}
	return nil
}
func validate(ctx context.Context, r *Repository, s lineage.Scope, rec lineage.Record) error {
	if ctx == nil || r == nil || r.database == nil || !s.Valid() || rec.Validate() != nil || rec.OrganizationID != s.OrganizationID() || rec.WorkspaceID != s.WorkspaceID() {
		return lineage.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (lineage.Record, error) {
	var r lineage.Record
	var outputObserved sql.NullTime
	var result string
	if err := row.Scan(&r.ID, &r.OrganizationID, &r.WorkspaceID, &r.Source, &r.ActorID, &r.Operation, &r.Output.System, &r.Output.EntityType, &r.Output.EntityID, &r.Output.Version, &r.Output.Field, &outputObserved, &r.Transformation.Kind, &r.Transformation.ID, &r.Transformation.Version, &r.Transformation.MappingID, &r.Transformation.RuleID, &r.CorrelationID, &r.CausationID, &r.AuditID, &r.EventID, &result, &r.OccurredAt); err != nil {
		return lineage.Record{}, err
	}
	if outputObserved.Valid {
		v := outputObserved.Time.UTC()
		r.Output.ObservedAt = &v
	}
	r.Result = lineage.Result(result)
	r.OccurredAt = r.OccurredAt.UTC()
	if err := r.Validate(); err != nil {
		return lineage.Record{}, err
	}
	return r, nil
}
func loadInputs(ctx context.Context, tx *sql.Tx, s lineage.Scope, id string) ([]lineage.Input, error) {
	rows, err := tx.QueryContext(ctx, `SELECT role,source_system,source_entity_type,source_entity_id,COALESCE(source_entity_version,''),COALESCE(source_field,''),source_observed_at FROM lineage_inputs WHERE organization_id=$1 AND workspace_id=$2 AND record_id=$3 ORDER BY position`, s.OrganizationID(), s.WorkspaceID(), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lineage.Input
	for rows.Next() {
		var i lineage.Input
		var observed sql.NullTime
		if err := rows.Scan(&i.Role, &i.Ref.System, &i.Ref.EntityType, &i.Ref.EntityID, &i.Ref.Version, &i.Ref.Field, &observed); err != nil {
			return nil, err
		}
		if observed.Valid {
			v := observed.Time.UTC()
			i.Ref.ObservedAt = &v
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

var _ lineage.Reader = (*Repository)(nil)
var _ lineage.Appender = (*Repository)(nil)
var _ = time.UTC
