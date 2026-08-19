// Package compliancerepo persists product-compliance evidence and policies in PostgreSQL.
package compliancerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/compliance"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/lineagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, compliance.ErrInvalid
	}
	return &Repository{db: db}, nil
}

// ListDocuments returns a bounded tenant-scoped document page ordered by freshness.
func (r *Repository) ListDocuments(ctx context.Context, s compliance.Scope, limit int) ([]compliance.ComplianceDocument, error) {
	if limit < 1 || limit > 100 {
		return nil, compliance.ErrInvalid
	}
	out := make([]compliance.ComplianceDocument, 0)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at FROM compliance_documents WHERE organization_id=$1 AND workspace_id=$2 AND (number<>'DEMO-EAEU-RU-001' OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)) ORDER BY updated_at DESC,id DESC LIMIT $3`, s.OrganizationID(), s.WorkspaceID(), limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			d, e := scanDocument(rows)
			if e != nil {
				return e
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) CreateDocument(ctx context.Context, s compliance.Scope, v compliance.ComplianceDocument, m compliance.Mutation) (compliance.ComplianceDocument, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != compliance.StatusDraft || m.Validate() != nil {
		return compliance.ComplianceDocument{}, compliance.ErrInvalid
	}
	var out compliance.ComplianceDocument
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanDocument(tx.QueryRowContext(ctx, `INSERT INTO compliance_documents(id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft',$10,$11,$12,$13,$14,'',NULL,1,$15,$15) RETURNING id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, string(v.Type), v.Number, v.Jurisdiction, v.Issuer, v.RegistrySource, v.RegistryReference, v.IssuedAt, nullTime(v.ExpiresAt), v.HolderPartyType, v.HolderPartyID, v.EvidenceObjectID, m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "document", out.ID.String(), out.Version, "created", nil)
	})
	return out, err
}
func (r *Repository) UpdateDocument(ctx context.Context, s compliance.Scope, v compliance.ComplianceDocument, m compliance.Mutation) (compliance.ComplianceDocument, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version < 2 || m.Validate() != nil {
		return compliance.ComplianceDocument{}, compliance.ErrInvalid
	}
	var out compliance.ComplianceDocument
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanDocument(tx.QueryRowContext(ctx, `UPDATE compliance_documents SET issuer=$4,registry_reference=$5,status=$6,expires_at=$7,holder_party_type=$8,holder_party_id=$9,evidence_object_id=$10,verification_source=$11,verified_at=$12,version=$13,updated_at=$14 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$15 RETURNING id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), v.ID.String(), v.Issuer, v.RegistryReference, string(v.Status), nullTime(v.ExpiresAt), v.HolderPartyType, v.HolderPartyID, v.EvidenceObjectID, v.VerificationSource, nullTime(v.VerifiedAt), v.Version, m.OccurredAt, v.Version-1))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "document", out.ID.String(), out.Version, "updated", []lineage.Input{{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "compliance_document", EntityID: out.ID.String(), Version: strconv.FormatInt(out.Version-1, 10)}}})
	})
	return out, err
}
func (r *Repository) CreateBinding(ctx context.Context, s compliance.Scope, v compliance.Binding, m compliance.Mutation) (compliance.Binding, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || m.Validate() != nil {
		return compliance.Binding{}, compliance.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$8)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.DocumentID.String(), string(v.SubjectType), v.SubjectID, v.Active, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "binding", v.ID.String(), 1, "created", []lineage.Input{{Role: "document", Ref: lineage.Ref{System: "torgnexa", EntityType: "compliance_document", EntityID: v.DocumentID.String()}}, {Role: "subject", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.SubjectType), EntityID: v.SubjectID}}})
	})
	return v, err
}
func (r *Repository) CreatePolicy(ctx context.Context, s compliance.Scope, v compliance.Policy, m compliance.Mutation) (compliance.Policy, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || m.Validate() != nil {
		return compliance.Policy{}, compliance.ErrInvalid
	}
	raw, _ := json.Marshal(v.Requirements)
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO compliance_policies(id,organization_id,workspace_id,code,jurisdiction,operation,connector_family,seller_role,category_id,requirements,effective_from,effective_until,active,version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, v.Jurisdiction, string(v.Operation), v.ChannelFamily, v.SellerRole, nullString(v.CategoryID.String()), raw, v.EffectiveFrom, nullTime(v.EffectiveUntil), v.Active, v.Version, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "policy", v.ID.String(), v.Version, "created", nil)
	})
	v.CreatedAt = m.OccurredAt
	v.UpdatedAt = m.OccurredAt
	return v, err
}

func (r *Repository) Evaluate(ctx context.Context, s compliance.Scope, c compliance.EvaluationContext) (compliance.Evaluation, error) {
	if c.Validate() != nil {
		return compliance.Evaluation{}, compliance.ErrInvalid
	}
	var policies []compliance.Policy
	var docs []compliance.ComplianceDocument
	var bindings []compliance.Binding
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,code,jurisdiction,operation,connector_family,seller_role,COALESCE(category_id,''),requirements,effective_from,effective_until,active,version,created_at FROM compliance_policies WHERE organization_id=$1 AND workspace_id=$2 AND active AND jurisdiction=$3 AND operation=$4 AND effective_from<=$5 AND (effective_until IS NULL OR effective_until>$5) ORDER BY code,version LIMIT 128`, s.OrganizationID(), s.WorkspaceID(), c.Jurisdiction, string(c.Operation), c.At)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var p compliance.Policy
			var id, op, cat string
			var raw []byte
			var until sql.NullTime
			if e = rows.Scan(&id, &p.Code, &p.Jurisdiction, &op, &p.ChannelFamily, &p.SellerRole, &cat, &raw, &p.EffectiveFrom, &until, &p.Active, &p.Version, &p.CreatedAt); e != nil {
				return e
			}
			p.ID = compliance.ID(id)
			p.OrganizationID = s.OrganizationID()
			p.WorkspaceID = s.WorkspaceID()
			p.Operation = compliance.Operation(op)
			p.CategoryID = compliance.ID(cat)
			p.UpdatedAt = p.CreatedAt
			if until.Valid {
				p.EffectiveUntil = until.Time.UTC()
			}
			if e = json.Unmarshal(raw, &p.Requirements); e != nil {
				return compliance.ErrInvalid
			}
			policies = append(policies, p)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		bRows, e := tx.QueryContext(ctx, `SELECT b.id,b.document_id,b.subject_type,b.subject_id,b.active,b.version,b.created_at,b.updated_at FROM compliance_bindings b WHERE b.organization_id=$1 AND b.workspace_id=$2 AND b.active AND ((b.subject_type='product' AND b.subject_id=$3) OR (b.subject_type='offer' AND b.subject_id=$4) OR (b.subject_type='category' AND b.subject_id=$5) OR (b.subject_type='gtin' AND b.subject_id=$6) OR (b.subject_type='sku' AND b.subject_id=$7)) ORDER BY b.id LIMIT 256`, s.OrganizationID(), s.WorkspaceID(), c.ProductID.String(), c.OfferID.String(), c.CategoryID.String(), c.GTIN, c.SKU)
		if e != nil {
			return e
		}
		defer bRows.Close()
		docIDs := []string{}
		for bRows.Next() {
			var b compliance.Binding
			var id, did, st string
			if e = bRows.Scan(&id, &did, &st, &b.SubjectID, &b.Active, &b.Version, &b.CreatedAt, &b.UpdatedAt); e != nil {
				return e
			}
			b.ID = compliance.ID(id)
			b.DocumentID = compliance.ID(did)
			b.SubjectType = compliance.SubjectType(st)
			b.OrganizationID = s.OrganizationID()
			b.WorkspaceID = s.WorkspaceID()
			b.CreatedAt = b.CreatedAt.UTC()
			b.UpdatedAt = b.UpdatedAt.UTC()
			bindings = append(bindings, b)
			docIDs = append(docIDs, did)
		}
		if e = bRows.Err(); e != nil {
			return e
		}
		if len(docIDs) == 0 {
			return nil
		}
		dRows, e := tx.QueryContext(ctx, `SELECT DISTINCT d.id,d.organization_id,d.workspace_id,d.document_type,d.number,d.jurisdiction,d.issuer,d.registry_source,d.registry_reference,d.status,d.issued_at,d.expires_at,d.holder_party_type,d.holder_party_id,d.evidence_object_id,d.verification_source,d.verified_at,d.version,d.created_at,d.updated_at FROM compliance_documents d JOIN compliance_bindings b ON b.organization_id=d.organization_id AND b.workspace_id=d.workspace_id AND b.document_id=d.id WHERE d.organization_id=$1 AND d.workspace_id=$2 AND b.active AND ((b.subject_type='product' AND b.subject_id=$3) OR (b.subject_type='offer' AND b.subject_id=$4) OR (b.subject_type='category' AND b.subject_id=$5) OR (b.subject_type='gtin' AND b.subject_id=$6) OR (b.subject_type='sku' AND b.subject_id=$7)) ORDER BY d.id LIMIT 256`, s.OrganizationID(), s.WorkspaceID(), c.ProductID.String(), c.OfferID.String(), c.CategoryID.String(), c.GTIN, c.SKU)
		if e != nil {
			return e
		}
		defer dRows.Close()
		for dRows.Next() {
			d, e := scanDocument(dRows)
			if e != nil {
				return e
			}
			docs = append(docs, d)
		}
		return dRows.Err()
	})
	if err != nil {
		return compliance.Evaluation{}, err
	}
	return compliance.Evaluate(c, policies, docs, bindings)
}

func (r *Repository) Verify(ctx context.Context, s compliance.Scope, id compliance.ID, verifier compliance.RegistryVerifier, m compliance.Mutation) (compliance.ComplianceDocument, error) {
	if !id.Valid() || verifier == nil || m.Validate() != nil {
		return compliance.ComplianceDocument{}, compliance.ErrInvalid
	}
	var out compliance.ComplianceDocument
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		current, e := scanDocument(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at FROM compliance_documents WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		if e != nil {
			return mapRead(e)
		}
		res, e := verifier.Verify(ctx, compliance.VerificationRequest{Document: current, At: m.OccurredAt})
		if e != nil {
			return e
		}
		if !res.Status.Valid() || res.Status == compliance.StatusDraft || res.Source == "" || res.VerifiedAt.Location() != time.UTC {
			return compliance.ErrInvalid
		}
		var scanErr error
		out, scanErr = scanDocument(tx.QueryRowContext(ctx, `UPDATE compliance_documents SET status=$4,registry_reference=CASE WHEN $5='' THEN registry_reference ELSE $5 END,verification_source=$6,verified_at=$7,version=version+1,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 RETURNING id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), id.String(), string(res.Status), res.RegistryReference, res.Source, res.VerifiedAt))
		if scanErr != nil {
			return mapWrite(scanErr)
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO compliance_verifications(id,organization_id,workspace_id,document_id,source,status,registry_reference,checked_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, m.EventID, s.OrganizationID(), s.WorkspaceID(), id.String(), res.Source, string(res.Status), res.RegistryReference, res.VerifiedAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "document", id.String(), out.Version, "verified", []lineage.Input{{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "compliance_document", EntityID: id.String(), Version: strconv.FormatInt(current.Version, 10)}}})
	})
	return out, err
}
func (r *Repository) ExpiryDue(ctx context.Context, s compliance.Scope, at time.Time, leadHours, limit int) ([]compliance.ComplianceDocument, error) {
	if at.Location() != time.UTC || leadHours < 1 || limit < 1 || limit > 500 {
		return nil, compliance.ErrInvalid
	}
	out := []compliance.ComplianceDocument{}
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,holder_party_type,holder_party_id,evidence_object_id,verification_source,verified_at,version,created_at,updated_at FROM compliance_documents WHERE organization_id=$1 AND workspace_id=$2 AND status='valid' AND expires_at IS NOT NULL AND expires_at>$3 AND expires_at<=$3+($4::text||' hours')::interval ORDER BY expires_at,id LIMIT $5`, s.OrganizationID(), s.WorkspaceID(), at, leadHours, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			d, e := scanDocument(rows)
			if e != nil {
				return e
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) evidence(ctx context.Context, tx *sql.Tx, s compliance.Scope, m compliance.Mutation, entityType, entityID string, version int64, change string, inputs []lineage.Input) error {
	ts, e := tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
	if e != nil {
		return e
	}
	summary, e := audit.SanitizeSummary(audit.Summary{"entity_type": entityType, "entity_id": entityID, "version": version, "change": change})
	if e != nil {
		return e
	}
	ar := audit.Record{ID: m.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: "compliance." + entityType + "." + change, ResourceType: entityType, ResourceID: entityID, CorrelationID: m.CorrelationID, Risk: audit.RiskLegallySignificant, Summary: summary, CreatedAt: m.OccurredAt}
	if e = auditrepo.AppendTransaction(ctx, tx, ts, ar); e != nil {
		return e
	}
	payload, _ := json.Marshal(struct {
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Version    int64  `json:"version"`
		Change     string `json:"change"`
	}{entityType, entityID, version, change})
	et, e := eventbus.ParseEventType("compliance.product.record_changed.v1")
	if e != nil {
		return e
	}
	occ, e := domain.NewUTCInstant(m.OccurredAt)
	if e != nil {
		return e
	}
	ev := eventbus.Event{ID: m.EventID, Type: et, OccurredAt: occ, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: payload}
	if e = ev.Validate(); e != nil {
		return e
	}
	enq, e := outboxrepo.NewTransactionEnqueuer(tx)
	if e != nil {
		return e
	}
	if e = enq.Enqueue(ctx, ev); e != nil {
		return e
	}
	lid, e := lineage.DeterministicID(m.EventID)
	if e != nil {
		return e
	}
	ls, e := lineage.NewScope(s.OrganizationID(), s.WorkspaceID())
	if e != nil {
		return e
	}
	lr := lineage.Record{ID: lid, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), Source: m.Source, ActorID: m.ActorID, Operation: "compliance." + entityType + "." + change, Output: lineage.Ref{System: "torgnexa", EntityType: "compliance_" + entityType, EntityID: entityID, Version: strconv.FormatInt(version, 10)}, Inputs: inputs, Transformation: lineage.Transformation{Kind: "compliance_mutation", ID: "compliance." + change, Version: "1"}, CorrelationID: m.CorrelationID, CausationID: m.CausationID, AuditID: m.AuditID, EventID: m.EventID, Result: lineage.ResultApplied, OccurredAt: m.OccurredAt}
	return lineagerepo.AppendTransaction(ctx, tx, ls, lr)
}
func (r *Repository) withTx(ctx context.Context, ro bool, s compliance.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || r == nil || r.db == nil || !s.Valid() {
		return compliance.ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	tx, e := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: ro})
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var o, w string
	if e = tx.QueryRowContext(ctx, applyScope, s.OrganizationID(), s.WorkspaceID()).Scan(&o, &w); e != nil {
		return e
	}
	if o != s.OrganizationID() || w != s.WorkspaceID() {
		return compliance.ErrInvalid
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanDocument(row scanner) (compliance.ComplianceDocument, error) {
	var d compliance.ComplianceDocument
	var id, typ, status string
	var exp, verified sql.NullTime
	if e := row.Scan(&id, &d.OrganizationID, &d.WorkspaceID, &typ, &d.Number, &d.Jurisdiction, &d.Issuer, &d.RegistrySource, &d.RegistryReference, &status, &d.IssuedAt, &exp, &d.HolderPartyType, &d.HolderPartyID, &d.EvidenceObjectID, &d.VerificationSource, &verified, &d.Version, &d.CreatedAt, &d.UpdatedAt); e != nil {
		return compliance.ComplianceDocument{}, mapRead(e)
	}
	d.ID = compliance.ID(id)
	d.Type = compliance.DocumentType(typ)
	d.Status = compliance.DocumentStatus(status)
	d.IssuedAt = d.IssuedAt.UTC()
	d.CreatedAt = d.CreatedAt.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()
	if exp.Valid {
		d.ExpiresAt = exp.Time.UTC()
	}
	if verified.Valid {
		d.VerifiedAt = verified.Time.UTC()
	}
	if d.Validate() != nil {
		return compliance.ComplianceDocument{}, compliance.ErrInvalid
	}
	return d, nil
}
func mapRead(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return compliance.ErrNotFound
	}
	return e
}
func mapWrite(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return compliance.ErrConflict
	}
	return e
}
func nullTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return v
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
