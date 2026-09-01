// Package legalpartyrepo implements tenant-scoped PostgreSQL persistence for legal-party masters.
package legalpartyrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
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

var _ legalparty.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("legal-party repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) LegalEntity(ctx context.Context, s legalparty.Scope, id legalparty.ID) (legalparty.LegalEntity, error) {
	var out legalparty.LegalEntity
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanLegalEntity(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,code,legal_name,short_name,country_code,inn,kpp,ogrn,status,version,created_at,updated_at FROM legal_entities WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}
func (r *Repository) IndividualEntrepreneur(ctx context.Context, s legalparty.Scope, id legalparty.ID) (legalparty.IndividualEntrepreneur, error) {
	var out legalparty.IndividualEntrepreneur
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanIE(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,code,full_name,country_code,inn,ogrnip,status,version,created_at,updated_at FROM individual_entrepreneurs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}

// Branch returns a tenant-scoped canonical branch master.
func (r *Repository) Branch(ctx context.Context, s legalparty.Scope, id legalparty.ID) (legalparty.Branch, error) {
	var out legalparty.Branch
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanBranch(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,legal_entity_id,code,name,country_code,kpp,status,version,created_at,updated_at FROM legal_branches WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}
func (r *Repository) Counterparty(ctx context.Context, s legalparty.Scope, id legalparty.ID) (legalparty.Counterparty, error) {
	var out legalparty.Counterparty
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanCounterparty(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,code,party_type,party_id,role,status,version,created_at,updated_at FROM counterparties WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, err
}

// ListCounterparties returns bounded canonical counterparty-role records for
// the authenticated organization/workspace, excluding unrelated party masters.
func (r *Repository) ListCounterparties(ctx context.Context, s legalparty.Scope, limit int) ([]legalparty.Counterparty, error) {
	if limit < 1 || limit > 100 {
		return nil, legalparty.ErrInvalid
	}
	items := make([]legalparty.Counterparty, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,code,party_type,party_id,role,status,version,created_at,updated_at FROM counterparties WHERE organization_id=$1 AND workspace_id=$2 ORDER BY id LIMIT $3`, s.OrganizationID(), s.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanCounterparty(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) Search(ctx context.Context, s legalparty.Scope, q legalparty.SearchQuery) (legalparty.SearchPage, error) {
	if q.Validate() != nil {
		return legalparty.SearchPage{}, legalparty.ErrInvalid
	}
	page := legalparty.SearchPage{Items: []legalparty.SearchResult{}}
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		textLike := "%" + strings.ToLower(q.Text) + "%"
		typ := string(q.PartyType)
		rows, err := tx.QueryContext(ctx, `SELECT party_type,party_id,code,display_name,inn,registration_id,status FROM (
      SELECT 'legal_entity'::text party_type,id party_id,code,COALESCE(NULLIF(short_name,''),legal_name) display_name,inn,ogrn registration_id,status FROM legal_entities WHERE organization_id=$1 AND workspace_id=$2
      UNION ALL
      SELECT 'individual_entrepreneur',id,code,full_name,inn,ogrnip,status FROM individual_entrepreneurs WHERE organization_id=$1 AND workspace_id=$2
      UNION ALL
      SELECT 'branch',b.id,b.code,b.name,e.inn,e.ogrn,b.status FROM legal_branches b JOIN legal_entities e ON e.organization_id=b.organization_id AND e.workspace_id=b.workspace_id AND e.id=b.legal_entity_id WHERE b.organization_id=$1 AND b.workspace_id=$2
    ) p WHERE ($3='' OR party_type=$3) AND ($4='' OR lower(display_name) LIKE $4) AND ($5='' OR inn=$5) AND ($6='' OR registration_id=$6) ORDER BY display_name,party_id LIMIT $7`, s.OrganizationID(), s.WorkspaceID(), typ, textLike, q.INN, q.RegistrationID, q.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x legalparty.SearchResult
			var pt, status string
			if err := rows.Scan(&pt, &x.PartyID, &x.Code, &x.DisplayName, &x.INN, &x.RegistrationID, &status); err != nil {
				return err
			}
			x.PartyType = legalparty.PartyType(pt)
			x.Status = legalparty.Status(status)
			page.Items = append(page.Items, x)
		}
		return rows.Err()
	})
	return page, err
}

func (r *Repository) CreateLegalEntity(ctx context.Context, s legalparty.Scope, v legalparty.LegalEntity, m legalparty.Mutation) (legalparty.LegalEntity, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.StatusDraft || m.Validate() != nil {
		return legalparty.LegalEntity{}, legalparty.ErrInvalid
	}
	var out legalparty.LegalEntity
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanLegalEntity(tx.QueryRowContext(ctx, `INSERT INTO legal_entities(id,organization_id,workspace_id,code,legal_name,short_name,country_code,inn,kpp,ogrn,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'draft',1,$11,$11) RETURNING id,organization_id,workspace_id,code,legal_name,short_name,country_code,inn,kpp,ogrn,status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, v.LegalName, v.ShortName, v.CountryCode, v.INN, v.KPP, v.OGRN, m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "legal_entity", out.ID.String(), out.Version, "created", nil)
	})
	return out, err
}
func (r *Repository) UpdateLegalEntity(ctx context.Context, s legalparty.Scope, v legalparty.LegalEntity, m legalparty.Mutation) (legalparty.LegalEntity, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version < 2 || m.Validate() != nil {
		return legalparty.LegalEntity{}, legalparty.ErrInvalid
	}
	var out legalparty.LegalEntity
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanLegalEntity(tx.QueryRowContext(ctx, `UPDATE legal_entities SET legal_name=$4,short_name=$5,country_code=$6,inn=$7,kpp=$8,ogrn=$9,status=$10,version=$11,updated_at=$12 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$13 RETURNING id,organization_id,workspace_id,code,legal_name,short_name,country_code,inn,kpp,ogrn,status,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), v.ID.String(), v.LegalName, v.ShortName, v.CountryCode, v.INN, v.KPP, v.OGRN, string(v.Status), v.Version, m.OccurredAt, v.Version-1))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "legal_entity", out.ID.String(), out.Version, "updated", []lineage.Input{{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "legal_entity", EntityID: out.ID.String(), Version: strconv.FormatInt(out.Version-1, 10)}}})
	})
	return out, err
}
func (r *Repository) CreateIndividualEntrepreneur(ctx context.Context, s legalparty.Scope, v legalparty.IndividualEntrepreneur, m legalparty.Mutation) (legalparty.IndividualEntrepreneur, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.StatusDraft || m.Validate() != nil {
		return legalparty.IndividualEntrepreneur{}, legalparty.ErrInvalid
	}
	var out legalparty.IndividualEntrepreneur
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanIE(tx.QueryRowContext(ctx, `INSERT INTO individual_entrepreneurs(id,organization_id,workspace_id,code,full_name,country_code,inn,ogrnip,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'draft',1,$9,$9) RETURNING id,organization_id,workspace_id,code,full_name,country_code,inn,ogrnip,status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, v.FullName, v.CountryCode, v.INN, v.OGRNIP, m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "individual_entrepreneur", out.ID.String(), out.Version, "created", nil)
	})
	return out, err
}
func (r *Repository) UpdateIndividualEntrepreneur(ctx context.Context, s legalparty.Scope, v legalparty.IndividualEntrepreneur, m legalparty.Mutation) (legalparty.IndividualEntrepreneur, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version < 2 || m.Validate() != nil {
		return legalparty.IndividualEntrepreneur{}, legalparty.ErrInvalid
	}
	var out legalparty.IndividualEntrepreneur
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanIE(tx.QueryRowContext(ctx, `UPDATE individual_entrepreneurs SET full_name=$4,country_code=$5,inn=$6,ogrnip=$7,status=$8,version=$9,updated_at=$10 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$11 RETURNING id,organization_id,workspace_id,code,full_name,country_code,inn,ogrnip,status,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), v.ID.String(), v.FullName, v.CountryCode, v.INN, v.OGRNIP, string(v.Status), v.Version, m.OccurredAt, v.Version-1))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "individual_entrepreneur", out.ID.String(), out.Version, "updated", []lineage.Input{{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "individual_entrepreneur", EntityID: out.ID.String(), Version: strconv.FormatInt(out.Version-1, 10)}}})
	})
	return out, err
}
func (r *Repository) CreateBranch(ctx context.Context, s legalparty.Scope, v legalparty.Branch, m legalparty.Mutation) (legalparty.Branch, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.StatusDraft || m.Validate() != nil {
		return legalparty.Branch{}, legalparty.ErrInvalid
	}
	var out legalparty.Branch
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanBranch(tx.QueryRowContext(ctx, `INSERT INTO legal_branches(id,organization_id,workspace_id,legal_entity_id,code,name,country_code,kpp,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'draft',1,$9,$9) RETURNING id,organization_id,workspace_id,legal_entity_id,code,name,country_code,kpp,status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.LegalEntityID.String(), v.Code, v.Name, v.CountryCode, v.KPP, m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "branch", out.ID.String(), out.Version, "created", []lineage.Input{{Role: "parent", Ref: lineage.Ref{System: "torgnexa", EntityType: "legal_entity", EntityID: v.LegalEntityID.String()}}})
	})
	return out, err
}
func (r *Repository) CreateCounterparty(ctx context.Context, s legalparty.Scope, v legalparty.Counterparty, m legalparty.Mutation) (legalparty.Counterparty, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.StatusDraft || m.Validate() != nil {
		return legalparty.Counterparty{}, legalparty.ErrInvalid
	}
	var out legalparty.Counterparty
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanCounterparty(tx.QueryRowContext(ctx, `INSERT INTO counterparties(id,organization_id,workspace_id,code,party_type,party_id,role,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'draft',1,$8,$8) RETURNING id,organization_id,workspace_id,code,party_type,party_id,role,status,version,created_at,updated_at`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.Code, string(v.PartyType), v.PartyID.String(), string(v.Role), m.OccurredAt))
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "counterparty", out.ID.String(), out.Version, "created", []lineage.Input{{Role: "party", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.PartyType), EntityID: v.PartyID.String()}}})
	})
	return out, err
}
func (r *Repository) CreateAddress(ctx context.Context, s legalparty.Scope, v legalparty.Address, m legalparty.Mutation) (legalparty.Address, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || m.Validate() != nil {
		return legalparty.Address{}, legalparty.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO legal_addresses(id,organization_id,workspace_id,party_type,party_id,kind,country_code,postal_code,region,city,line1,line2,is_primary,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1,$15,$15)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, string(v.PartyType), v.PartyID.String(), string(v.Kind), v.CountryCode, v.PostalCode, v.Region, v.City, v.Line1, v.Line2, v.IsPrimary, v.Active, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "address", v.ID.String(), 1, "created", []lineage.Input{{Role: "party", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.PartyType), EntityID: v.PartyID.String()}}})
	})
	return v, err
}
func (r *Repository) CreateBankAccount(ctx context.Context, s legalparty.Scope, v legalparty.BankAccount, m legalparty.Mutation) (legalparty.BankAccount, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.StatusDraft || m.Validate() != nil {
		return legalparty.BankAccount{}, legalparty.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO counterparty_bank_accounts(id,organization_id,workspace_id,counterparty_id,currency,account_number,bank_name,bank_country_code,bic,correspondent_account,is_primary,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'draft',1,$12,$12)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.CounterpartyID.String(), v.Currency, v.AccountNumber, v.BankName, v.BankCountryCode, v.BIC, v.CorrespondentAccount, v.IsPrimary, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "bank_account", v.ID.String(), 1, "created", []lineage.Input{{Role: "counterparty", Ref: lineage.Ref{System: "torgnexa", EntityType: "counterparty", EntityID: v.CounterpartyID.String()}}})
	})
	return v, err
}
func (r *Repository) CreateContract(ctx context.Context, s legalparty.Scope, v legalparty.Contract, m legalparty.Mutation) (legalparty.Contract, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.ContractDraft || m.Validate() != nil {
		return legalparty.Contract{}, legalparty.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO counterparty_contracts(id,organization_id,workspace_id,counterparty_id,number,contract_type,signed_on,valid_from,valid_until,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft',1,$10,$10)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.CounterpartyID.String(), v.Number, v.ContractType, v.SignedOn, v.ValidFrom, v.ValidUntil, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "contract", v.ID.String(), 1, "created", []lineage.Input{{Role: "counterparty", Ref: lineage.Ref{System: "torgnexa", EntityType: "counterparty", EntityID: v.CounterpartyID.String()}}})
	})
	return v, err
}
func (r *Repository) CreateAuthorityReference(ctx context.Context, s legalparty.Scope, v legalparty.AuthorityReference, m legalparty.Mutation) (legalparty.AuthorityReference, error) {
	if v.Validate() != nil || v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.Version != 1 || v.Status != legalparty.StatusDraft || m.Validate() != nil {
		return legalparty.AuthorityReference{}, legalparty.ErrInvalid
	}
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO counterparty_authorities(id,organization_id,workspace_id,counterparty_id,authority_type,reference_number,issuer,issued_at,expires_at,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft',1,$10,$10)`, v.ID.String(), v.OrganizationID, v.WorkspaceID, v.CounterpartyID.String(), string(v.Type), v.ReferenceNumber, v.Issuer, v.IssuedAt, v.ExpiresAt, m.OccurredAt)
		if e != nil {
			return mapWrite(e)
		}
		return r.evidence(ctx, tx, s, m, "authority_reference", v.ID.String(), 1, "created", []lineage.Input{{Role: "counterparty", Ref: lineage.Ref{System: "torgnexa", EntityType: "counterparty", EntityID: v.CounterpartyID.String()}}})
	})
	return v, err
}
func (r *Repository) StoreMergePreview(ctx context.Context, s legalparty.Scope, v legalparty.MergePreview, m legalparty.Mutation) error {
	if v.OrganizationID != s.OrganizationID() || v.WorkspaceID != s.WorkspaceID() || v.ID != "party-merge."+v.FingerprintSHA256 || len(v.Fields) < 1 || m.Validate() != nil {
		return legalparty.ErrInvalid
	}
	fields, _ := json.Marshal(v.Fields)
	return r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		res, e := tx.ExecContext(ctx, `INSERT INTO legal_party_merge_previews(id,organization_id,workspace_id,party_type,target_id,source_id,target_version,source_version,fields,has_conflicts,fingerprint_sha256,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO NOTHING`, v.ID, v.OrganizationID, v.WorkspaceID, string(v.PartyType), v.TargetID.String(), v.SourceID.String(), v.TargetVersion, v.SourceVersion, fields, v.HasConflicts, v.FingerprintSHA256, v.CreatedAt)
		if e != nil {
			return mapWrite(e)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			var stored string
			if e := tx.QueryRowContext(ctx, `SELECT fingerprint_sha256 FROM legal_party_merge_previews WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), v.ID).Scan(&stored); e != nil || stored != v.FingerprintSHA256 {
				return legalparty.ErrConflict
			}
		}
		inputs := []lineage.Input{{Role: "target", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.PartyType), EntityID: v.TargetID.String(), Version: strconv.FormatInt(v.TargetVersion, 10)}}, {Role: "source", Ref: lineage.Ref{System: "torgnexa", EntityType: string(v.PartyType), EntityID: v.SourceID.String(), Version: strconv.FormatInt(v.SourceVersion, 10)}}}
		return r.evidence(ctx, tx, s, m, "merge_preview", v.ID, 1, "created", inputs)
	})
}

func (r *Repository) evidence(ctx context.Context, tx *sql.Tx, s legalparty.Scope, m legalparty.Mutation, entityType, entityID string, version int64, change string, inputs []lineage.Input) error {
	ts, err := tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
	if err != nil {
		return err
	}
	summary, err := audit.SanitizeSummary(audit.Summary{"entity_type": entityType, "entity_id": entityID, "version": version, "change": change})
	if err != nil {
		return err
	}
	ar := audit.Record{ID: m.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: "legal_party." + entityType + "." + change, ResourceType: entityType, ResourceID: entityID, CorrelationID: m.CorrelationID, Risk: audit.RiskWriteSensitive, Summary: summary, CreatedAt: m.OccurredAt}
	if err := auditrepo.AppendTransaction(ctx, tx, ts, ar); err != nil {
		return err
	}
	payload, _ := json.Marshal(struct {
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Version    int64  `json:"version"`
		Change     string `json:"change"`
	}{entityType, entityID, version, change})
	et, err := eventbus.ParseEventType("enterprise.legal_party.record_changed.v1")
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
	lr := lineage.Record{ID: lid, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), Source: m.Source, ActorID: m.ActorID, Operation: "legal_party." + entityType + "." + change, Output: lineage.Ref{System: "torgnexa", EntityType: entityType, EntityID: entityID, Version: strconv.FormatInt(version, 10)}, Inputs: inputs, Transformation: lineage.Transformation{Kind: "master_data_mutation", ID: "legal_party." + change, Version: "1"}, CorrelationID: m.CorrelationID, CausationID: m.CausationID, AuditID: m.AuditID, EventID: m.EventID, Result: lineage.ResultApplied, OccurredAt: m.OccurredAt}
	return lineagerepo.AppendTransaction(ctx, tx, ls, lr)
}
func (r *Repository) withTx(ctx context.Context, ro bool, s legalparty.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || r == nil || r.db == nil || !s.Valid() {
		return legalparty.ErrInvalid
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
		return legalparty.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanLegalEntity(row scanner) (legalparty.LegalEntity, error) {
	var v legalparty.LegalEntity
	var id, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &v.Code, &v.LegalName, &v.ShortName, &v.CountryCode, &v.INN, &v.KPP, &v.OGRN, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return legalparty.LegalEntity{}, mapRead(e)
	}
	v.ID = legalparty.ID(id)
	v.Status = legalparty.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return legalparty.LegalEntity{}, legalparty.ErrInvalid
	}
	return v, nil
}
func scanIE(row scanner) (legalparty.IndividualEntrepreneur, error) {
	var v legalparty.IndividualEntrepreneur
	var id, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &v.Code, &v.FullName, &v.CountryCode, &v.INN, &v.OGRNIP, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return legalparty.IndividualEntrepreneur{}, mapRead(e)
	}
	v.ID = legalparty.ID(id)
	v.Status = legalparty.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return legalparty.IndividualEntrepreneur{}, legalparty.ErrInvalid
	}
	return v, nil
}
func scanBranch(row scanner) (legalparty.Branch, error) {
	var v legalparty.Branch
	var id, parent, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &parent, &v.Code, &v.Name, &v.CountryCode, &v.KPP, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return legalparty.Branch{}, mapRead(e)
	}
	v.ID = legalparty.ID(id)
	v.LegalEntityID = legalparty.ID(parent)
	v.Status = legalparty.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return legalparty.Branch{}, legalparty.ErrInvalid
	}
	return v, nil
}
func scanCounterparty(row scanner) (legalparty.Counterparty, error) {
	var v legalparty.Counterparty
	var id, pt, partyID, role, status string
	if e := row.Scan(&id, &v.OrganizationID, &v.WorkspaceID, &v.Code, &pt, &partyID, &role, &status, &v.Version, &v.CreatedAt, &v.UpdatedAt); e != nil {
		return legalparty.Counterparty{}, mapRead(e)
	}
	v.ID = legalparty.ID(id)
	v.PartyID = legalparty.ID(partyID)
	v.PartyType = legalparty.PartyType(pt)
	v.Role = legalparty.CounterpartyRole(role)
	v.Status = legalparty.Status(status)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return legalparty.Counterparty{}, legalparty.ErrInvalid
	}
	return v, nil
}
func mapRead(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return legalparty.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("legal-party repository: read: %w", err)
	}
	return nil
}
func mapWrite(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if strings.Contains(s, "duplicate key") || strings.Contains(s, "version") || strings.Contains(s, "immutable") {
		return legalparty.ErrConflict
	}
	return fmt.Errorf("legal-party repository: write: %w", err)
}
