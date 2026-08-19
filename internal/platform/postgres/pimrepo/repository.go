// Package pimrepo implements tenant-scoped PostgreSQL persistence for canonical PIM/MDM.
package pimrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/torgnexa/torgnexa/internal/core/pim"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/lineagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

var _ pim.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("pim repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Brand(ctx context.Context, s pim.Scope, id pim.ID) (pim.Brand, error) {
	var out pim.Brand
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanBrand(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,code,name,status,version,created_at,updated_at FROM pim_brands WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}
func (r *Repository) Category(ctx context.Context, s pim.Scope, id pim.ID) (pim.Category, error) {
	var out pim.Category
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanCategory(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,code,name,COALESCE(parent_id,''),status,version,created_at,updated_at FROM pim_categories WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}
func (r *Repository) Attribute(ctx context.Context, s pim.Scope, id pim.ID) (pim.Attribute, error) {
	var out pim.Attribute
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanAttribute(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,code,name,value_type,multi_value,status,version,created_at,updated_at FROM pim_attributes WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}

func (r *Repository) CreateBrand(ctx context.Context, s pim.Scope, v pim.Brand, m pim.Mutation) (pim.Brand, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil || v.Version != 1 || v.Status != pim.StatusDraft {
		return pim.Brand{}, pim.ErrInvalid
	}
	var out pim.Brand
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanBrand(tx.QueryRowContext(ctx, `INSERT INTO pim_brands(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,1,$7,$7) RETURNING id,organization_id,workspace_id,code,name,status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, v.Name, string(v.Status), m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "brand", out.ID.String(), out.Version, "created", nil)
	})
	return out, err
}
func (r *Repository) UpdateBrand(ctx context.Context, s pim.Scope, v pim.Brand, m pim.Mutation) (pim.Brand, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil || v.Version < 2 {
		return pim.Brand{}, pim.ErrInvalid
	}
	var out pim.Brand
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanBrand(tx.QueryRowContext(ctx, `UPDATE pim_brands SET name=$4,status=$5,version=$6,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$8 RETURNING id,organization_id,workspace_id,code,name,status,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), v.ID.String(), v.Name, string(v.Status), v.Version, m.OccurredAt, v.Version-1))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "brand", out.ID.String(), out.Version, "updated", []lineage.Input{{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "brand", EntityID: out.ID.String(), Version: strconv.FormatInt(out.Version-1, 10)}}})
	})
	return out, err
}
func (r *Repository) CreateCategory(ctx context.Context, s pim.Scope, v pim.Category, m pim.Mutation) (pim.Category, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil || v.Version != 1 || v.Status != pim.StatusDraft {
		return pim.Category{}, pim.ErrInvalid
	}
	var out pim.Category
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var parent any
		if v.ParentID != "" {
			parent = v.ParentID.String()
		}
		var e error
		out, e = scanCategory(tx.QueryRowContext(ctx, `INSERT INTO pim_categories(id,organization_id,workspace_id,code,name,parent_id,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$8) RETURNING id,organization_id,workspace_id,code,name,COALESCE(parent_id,''),status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, v.Name, parent, string(v.Status), m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "category", out.ID.String(), out.Version, "created", nil)
	})
	return out, err
}
func (r *Repository) UpdateCategory(ctx context.Context, s pim.Scope, v pim.Category, m pim.Mutation) (pim.Category, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil || v.Version < 2 {
		return pim.Category{}, pim.ErrInvalid
	}
	var out pim.Category
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanCategory(tx.QueryRowContext(ctx, `UPDATE pim_categories SET name=$4,status=$5,version=$6,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$8 RETURNING id,organization_id,workspace_id,code,name,COALESCE(parent_id,''),status,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), v.ID.String(), v.Name, string(v.Status), v.Version, m.OccurredAt, v.Version-1))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "category", out.ID.String(), out.Version, "updated", nil)
	})
	return out, err
}
func (r *Repository) CreateAttribute(ctx context.Context, s pim.Scope, v pim.Attribute, m pim.Mutation) (pim.Attribute, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil || v.Version != 1 || v.Status != pim.StatusDraft {
		return pim.Attribute{}, pim.ErrInvalid
	}
	var out pim.Attribute
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanAttribute(tx.QueryRowContext(ctx, `INSERT INTO pim_attributes(id,organization_id,workspace_id,code,name,value_type,multi_value,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9) RETURNING id,organization_id,workspace_id,code,name,value_type,multi_value,status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, v.Name, string(v.ValueType), v.MultiValue, string(v.Status), m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "attribute", out.ID.String(), out.Version, "created", nil)
	})
	return out, err
}
func (r *Repository) UpdateAttribute(ctx context.Context, s pim.Scope, v pim.Attribute, m pim.Mutation) (pim.Attribute, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil || v.Version < 2 {
		return pim.Attribute{}, pim.ErrInvalid
	}
	var out pim.Attribute
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanAttribute(tx.QueryRowContext(ctx, `UPDATE pim_attributes SET name=$4,status=$5,version=$6,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$8 RETURNING id,organization_id,workspace_id,code,name,value_type,multi_value,status,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), v.ID.String(), v.Name, string(v.Status), v.Version, m.OccurredAt, v.Version-1))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "attribute", out.ID.String(), out.Version, "updated", nil)
	})
	return out, err
}

func (r *Repository) SetProductBrand(ctx context.Context, s pim.Scope, v pim.ProductBrand, m pim.Mutation) (pim.ProductBrand, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil {
		return pim.ProductBrand{}, pim.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if v.Version == 1 {
			_, e := tx.ExecContext(ctx, `INSERT INTO pim_product_brands(organization_id,workspace_id,product_id,brand_id,source,version,active,created_at,updated_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$7)`, v.OrganizationID, v.WorkspaceID, v.ProductID.String(), v.BrandID.String(), v.Source, v.Active, m.OccurredAt)
			if e != nil {
				return mapWrite(e)
			}
		} else {
			res, e := tx.ExecContext(ctx, `UPDATE pim_product_brands SET brand_id=$4,source=$5,version=$6,active=$7,updated_at=$8 WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 AND version=$9`, v.OrganizationID, v.WorkspaceID, v.ProductID.String(), v.BrandID.String(), v.Source, v.Version, v.Active, m.OccurredAt, v.Version-1)
			if e != nil {
				return mapWrite(e)
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return pim.ErrConflict
			}
		}
		return r.evidence(ctx, tx, s, m, "product_brand", v.ProductID.String(), v.Version, "set", nil)
	})
	return v, err
}
func (r *Repository) SetProductCategory(ctx context.Context, s pim.Scope, v pim.ProductCategory, m pim.Mutation) (pim.ProductCategory, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil {
		return pim.ProductCategory{}, pim.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if v.Version == 1 {
			_, e := tx.ExecContext(ctx, `INSERT INTO pim_product_categories(organization_id,workspace_id,product_id,category_id,is_primary,source,version,active,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,$8)`, v.OrganizationID, v.WorkspaceID, v.ProductID.String(), v.CategoryID.String(), v.IsPrimary, v.Source, v.Active, m.OccurredAt)
			if e != nil {
				return mapWrite(e)
			}
		} else {
			res, e := tx.ExecContext(ctx, `UPDATE pim_product_categories SET is_primary=$5,source=$6,version=$7,active=$8,updated_at=$9 WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 AND category_id=$4 AND version=$10`, v.OrganizationID, v.WorkspaceID, v.ProductID.String(), v.CategoryID.String(), v.IsPrimary, v.Source, v.Version, v.Active, m.OccurredAt, v.Version-1)
			if e != nil {
				return mapWrite(e)
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return pim.ErrConflict
			}
		}
		return r.evidence(ctx, tx, s, m, "product_category", v.ProductID.String()+":"+v.CategoryID.String(), v.Version, "set", nil)
	})
	return v, err
}
func (r *Repository) SetProductAttributeValue(ctx context.Context, s pim.Scope, v pim.AttributeValue, m pim.Mutation) (pim.AttributeValue, error) {
	if v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil {
		return pim.AttributeValue{}, pim.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var kind string
		var multi bool
		if e := tx.QueryRowContext(ctx, `SELECT value_type,multi_value FROM pim_attributes WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status<>'archived'`, s.OrganizationID(), s.WorkspaceID(), v.AttributeID.String()).Scan(&kind, &multi); e != nil {
			return mapWrite(e)
		}
		if v.Validate(pim.ValueType(kind), multi) != nil {
			return pim.ErrInvalid
		}
		if v.Version == 1 {
			_, e := tx.ExecContext(ctx, `INSERT INTO pim_product_attribute_values(organization_id,workspace_id,product_id,attribute_id,ordinal,value,source,version,active,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$9)`, v.OrganizationID, v.WorkspaceID, v.ProductID.String(), v.AttributeID.String(), v.Ordinal, string(v.Value), v.Source, v.Active, m.OccurredAt)
			if e != nil {
				return mapWrite(e)
			}
		} else {
			res, e := tx.ExecContext(ctx, `UPDATE pim_product_attribute_values SET value=$6,source=$7,version=$8,active=$9,updated_at=$10 WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 AND attribute_id=$4 AND ordinal=$5 AND version=$11`, v.OrganizationID, v.WorkspaceID, v.ProductID.String(), v.AttributeID.String(), v.Ordinal, string(v.Value), v.Source, v.Version, v.Active, m.OccurredAt, v.Version-1)
			if e != nil {
				return mapWrite(e)
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return pim.ErrConflict
			}
		}
		return r.evidence(ctx, tx, s, m, "product_attribute", v.ProductID.String()+":"+v.AttributeID.String(), v.Version, "set", nil)
	})
	return v, err
}
func (r *Repository) SetFieldAuthority(ctx context.Context, s pim.Scope, v pim.FieldAuthority, m pim.Mutation) (pim.FieldAuthority, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil {
		return pim.FieldAuthority{}, pim.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if v.Version == 1 {
			_, e := tx.ExecContext(ctx, `INSERT INTO pim_field_authorities(id,organization_id,workspace_id,entity_type,field_path,source,priority,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, string(v.EntityType), v.FieldPath, v.Source, v.Priority, v.Active, m.OccurredAt)
			if e != nil {
				return mapWrite(e)
			}
		} else {
			res, e := tx.ExecContext(ctx, `UPDATE pim_field_authorities SET priority=$4,active=$5,version=$6,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$8`, v.OrganizationID, v.WorkspaceID, v.ID.String(), v.Priority, v.Active, v.Version, m.OccurredAt, v.Version-1)
			if e != nil {
				return mapWrite(e)
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return pim.ErrConflict
			}
		}
		return r.evidence(ctx, tx, s, m, "field_authority", v.ID.String(), v.Version, "set", nil)
	})
	return v, err
}
func (r *Repository) FlagDuplicate(ctx context.Context, s pim.Scope, v pim.DuplicateCandidate, m pim.Mutation) (pim.DuplicateCandidate, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil {
		return pim.DuplicateCandidate{}, pim.ErrInvalid
	}
	signals, _ := json.Marshal(v.Signals)
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if v.Version == 1 {
			_, e := tx.ExecContext(ctx, `INSERT INTO pim_duplicate_candidates(id,organization_id,workspace_id,entity_type,left_id,right_id,score_bps,signals,state,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'open',1,$9,$9)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, string(v.EntityType), v.LeftID.String(), v.RightID.String(), v.ScoreBPS, signals, m.OccurredAt)
			if e != nil {
				return mapWrite(e)
			}
		} else {
			res, e := tx.ExecContext(ctx, `UPDATE pim_duplicate_candidates SET state=$4,version=$5,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7`, v.OrganizationID, v.WorkspaceID, v.ID.String(), string(v.State), v.Version, m.OccurredAt, v.Version-1)
			if e != nil {
				return mapWrite(e)
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return pim.ErrConflict
			}
		}
		inputs := []lineage.Input{{Role: "left", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.EntityType), EntityID: v.LeftID.String()}}, {Role: "right", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.EntityType), EntityID: v.RightID.String()}}}
		return r.evidence(ctx, tx, s, m, "duplicate_candidate", v.ID.String(), v.Version, "flagged", inputs)
	})
	return v, err
}
func (r *Repository) StoreMergePreview(ctx context.Context, s pim.Scope, v pim.MergePreview, m pim.Mutation) error {
	if m.Validate() != nil || v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() {
		return pim.ErrInvalid
	}
	fields, _ := json.Marshal(v.Fields)
	return r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		res, e := tx.ExecContext(ctx, `INSERT INTO pim_merge_previews(id,organization_id,workspace_id,entity_type,target_id,source_id,target_version,source_version,fields,has_conflicts,fingerprint_sha256,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING`, v.ID, s.OrganizationID(), s.WorkspaceID(), string(v.EntityType), v.TargetID.String(), v.SourceID.String(), v.TargetVersion, v.SourceVersion, fields, v.HasConflicts, v.FingerprintSHA256, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			var stored string
			if e := tx.QueryRowContext(ctx, `SELECT fingerprint_sha256 FROM pim_merge_previews WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), v.ID).Scan(&stored); e != nil || stored != v.FingerprintSHA256 {
				return pim.ErrConflict
			}
		}
		inputs := []lineage.Input{{Role: "target", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.EntityType), EntityID: v.TargetID.String(), Version: v.TargetVersion}}, {Role: "source", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.EntityType), EntityID: v.SourceID.String(), Version: v.SourceVersion}}}
		return r.evidence(ctx, tx, s, m, "merge_preview", v.ID, 1, "created", inputs)
	})
}

func (r *Repository) evidence(ctx context.Context, tx *sql.Tx, s pim.Scope, m pim.Mutation, entityType, entityID string, version int64, change string, inputs []lineage.Input) error {
	ts, err := tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
	if err != nil {
		return err
	}
	summary, err := audit.SanitizeSummary(audit.Summary{"entity_type": entityType, "entity_id": entityID, "version": version, "change": change})
	if err != nil {
		return err
	}
	ar := audit.Record{ID: m.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: "pim." + entityType + "." + change, ResourceType: entityType, ResourceID: entityID, CorrelationID: m.CorrelationID, Risk: audit.RiskWriteSensitive, Summary: summary, CreatedAt: m.OccurredAt}
	if err := auditrepo.AppendTransaction(ctx, tx, ts, ar); err != nil {
		return err
	}
	payload, _ := json.Marshal(struct {
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Version    int64  `json:"version"`
		Change     string `json:"change"`
	}{entityType, entityID, version, change})
	et, err := eventbus.ParseEventType("commerce.pim.record_changed.v1")
	if err != nil {
		return err
	}
	occ, err := domain.NewUTCInstant(m.OccurredAt)
	if err != nil {
		return err
	}
	ev := eventbus.Event{ID: m.EventID, Type: et, OccurredAt: occ, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: payload}
	if err := ev.Validate(); err != nil {
		return err
	}
	enq, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	if err := enq.Enqueue(ctx, ev); err != nil {
		return err
	}
	lid, err := lineage.DeterministicID(m.EventID)
	if err != nil {
		return err
	}
	ls, err := lineage.NewScope(s.OrganizationID(), s.WorkspaceID())
	if err != nil {
		return err
	}
	lr := lineage.Record{ID: lid, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), Source: m.Source, ActorID: m.ActorID, Operation: "pim." + entityType + "." + change, Output: lineage.Ref{System: "torgnexa", EntityType: entityType, EntityID: entityID, Version: strconv.FormatInt(version, 10)}, Inputs: inputs, Transformation: lineage.Transformation{Kind: "mdm_mutation", ID: "pim." + change, Version: "1"}, CorrelationID: m.CorrelationID, CausationID: m.CausationID, AuditID: m.AuditID, EventID: m.EventID, Result: lineage.ResultApplied, OccurredAt: m.OccurredAt}
	return lineagerepo.AppendTransaction(ctx, tx, ls, lr)
}
func (r *Repository) withTx(ctx context.Context, ro bool, s pim.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || r == nil || r.db == nil || !s.Valid() {
		return pim.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: ro})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var o, w string
	if err := tx.QueryRowContext(ctx, applyScope, s.OrganizationID(), s.WorkspaceID()).Scan(&o, &w); err != nil {
		return err
	}
	if o != s.OrganizationID() || w != s.WorkspaceID() {
		return pim.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanBrand(row scanner) (pim.Brand, error) {
	var v pim.Brand
	var id, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &v.Code, &v.Name, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return pim.Brand{}, mapRead(e)
	}
	v.ID = pim.ID(id)
	v.Status = pim.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return pim.Brand{}, pim.ErrInvalid
	}
	return v, nil
}
func scanCategory(row scanner) (pim.Category, error) {
	var v pim.Category
	var id, parent, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &v.Code, &v.Name, &parent, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return pim.Category{}, mapRead(e)
	}
	v.ID = pim.ID(id)
	v.ParentID = pim.ID(parent)
	v.Status = pim.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return pim.Category{}, pim.ErrInvalid
	}
	return v, nil
}
func scanAttribute(row scanner) (pim.Attribute, error) {
	var v pim.Attribute
	var id, kind, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &v.Code, &v.Name, &kind, &v.MultiValue, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return pim.Attribute{}, mapRead(e)
	}
	v.ID = pim.ID(id)
	v.ValueType = pim.ValueType(kind)
	v.Status = pim.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return pim.Attribute{}, pim.ErrInvalid
	}
	return v, nil
}
func mapRead(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return pim.ErrNotFound
	}
	return fmt.Errorf("pim repository: %w", err)
}
func mapWrite(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return pim.ErrConflict
	}
	return fmt.Errorf("pim repository: %w", err)
}
