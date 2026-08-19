// Package pricingrepo implements tenant-scoped PostgreSQL Price persistence.
package pricingrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/pricing"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/lineagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`
const priceSelect = `SELECT id, organization_id, workspace_id, offer_id, kind, minor_units, currency, version, created_at, updated_at
FROM prices WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

type Repository struct{ database *sql.DB }

var _ pricing.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("pricing repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (r *Repository) Price(ctx context.Context, scope pricing.Scope, id pricing.PriceID) (pricing.Price, error) {
	if err := validate(ctx, r, scope); err != nil {
		return pricing.Price{}, err
	}
	if !id.Valid() {
		return pricing.Price{}, pricing.ErrInvalidRecord
	}
	var result pricing.Price
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		var err error
		result, err = scanPrice(tx.QueryRowContext(ctx, priceSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}
func (r *Repository) PricesByOffer(ctx context.Context, scope pricing.Scope, offerID pricing.OfferID, limit int) ([]pricing.Price, error) {
	if err := validate(ctx, r, scope); err != nil {
		return nil, err
	}
	if !offerID.Valid() || limit < 1 || limit > 1000 {
		return nil, pricing.ErrInvalidRecord
	}
	var result []pricing.Price
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, organization_id, workspace_id, offer_id, kind, minor_units, currency, version, created_at, updated_at FROM prices WHERE organization_id=$1 AND workspace_id=$2 AND offer_id=$3 ORDER BY kind,currency,id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), offerID.String(), limit)
		if err != nil {
			return fmt.Errorf("pricing repository: list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			p, e := scanPrice(rows)
			if e != nil {
				return e
			}
			result = append(result, p)
		}
		return rows.Err()
	})
	return result, err
}
func (r *Repository) Create(ctx context.Context, scope pricing.Scope, command pricing.CreatePrice, mutation pricing.Mutation) (pricing.Price, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return pricing.Price{}, err
	}
	if err := command.Validate(); err != nil {
		return pricing.Price{}, err
	}
	var result pricing.Price
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		var offerOK bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status <> 'archived')`, scope.OrganizationID(), scope.WorkspaceID(), command.OfferID.String()).Scan(&offerOK); err != nil {
			return fmt.Errorf("pricing repository: validate offer: %w", err)
		}
		if !offerOK {
			return pricing.ErrInvalidRecord
		}
		p, err := scanPrice(tx.QueryRowContext(ctx, `INSERT INTO prices(id,organization_id,workspace_id,offer_id,kind,minor_units,currency) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,offer_id,kind,minor_units,currency,version,created_at,updated_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.OfferID.String(), string(command.Kind), command.Amount.MinorUnits(), command.Amount.Currency().String()))
		if errors.Is(err, pricing.ErrNotFound) {
			return pricing.ErrConflict
		}
		if err != nil {
			return err
		}
		result = p
		if err := appendAudit(ctx, tx, scope, mutation, result, "pricing.price.created", audit.RiskWriteSensitive, nil); err != nil {
			return err
		}
		if err := enqueuePriceEvent(ctx, tx, scope, mutation, result, "created"); err != nil {
			return err
		}
		return appendPriceLineage(ctx, tx, scope, mutation, result, nil, "created")
	})
	return result, err
}
func (r *Repository) Update(ctx context.Context, scope pricing.Scope, command pricing.UpdatePrice, mutation pricing.Mutation) (pricing.Price, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return pricing.Price{}, err
	}
	if err := command.Validate(); err != nil {
		return pricing.Price{}, err
	}
	var result pricing.Price
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		current, err := scanPrice(tx.QueryRowContext(ctx, priceSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return pricing.ErrConflict
		}
		if current.Amount.Currency() != command.Amount.Currency() {
			return pricing.ErrInvalidRecord
		}
		result, err = scanPrice(tx.QueryRowContext(ctx, `UPDATE prices SET minor_units=$4,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 RETURNING id,organization_id,workspace_id,offer_id,kind,minor_units,currency,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), command.Amount.MinorUnits(), command.ExpectedVersion))
		if err != nil {
			return err
		}
		before := current.Amount.MinorUnits()
		if err := appendAudit(ctx, tx, scope, mutation, result, "pricing.price.updated", audit.RiskWriteSensitive, &before); err != nil {
			return err
		}
		if err := enqueuePriceEvent(ctx, tx, scope, mutation, result, "updated"); err != nil {
			return err
		}
		return appendPriceLineage(ctx, tx, scope, mutation, result, &current, "updated")
	})
	return result, err
}

func (r *Repository) withTx(ctx context.Context, readOnly bool, scope pricing.Scope, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("pricing repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var o, w string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID(), scope.WorkspaceID()).Scan(&o, &w); err != nil {
		return fmt.Errorf("pricing repository: scope: %w", err)
	}
	if o != scope.OrganizationID() || w != scope.WorkspaceID() {
		return pricing.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("pricing repository: commit: %w", err)
	}
	return nil
}
func validate(ctx context.Context, r *Repository, s pricing.Scope) error {
	if ctx == nil {
		return errors.New("pricing repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.database == nil {
		return errors.New("pricing repository: uninitialized")
	}
	if !s.Valid() {
		return pricing.ErrInvalidScope
	}
	return nil
}
func validateMutation(ctx context.Context, r *Repository, s pricing.Scope, m pricing.Mutation) error {
	if err := validate(ctx, r, s); err != nil {
		return err
	}
	return m.Validate()
}

type scanner interface{ Scan(...any) error }

func scanPrice(row scanner) (pricing.Price, error) {
	var p pricing.Price
	var id, org, ws, offer, kind, currency string
	var minor int64
	if err := row.Scan(&id, &org, &ws, &offer, &kind, &minor, &currency, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pricing.Price{}, pricing.ErrNotFound
		}
		return pricing.Price{}, fmt.Errorf("pricing repository: scan: %w", err)
	}
	c, err := pricing.NewCurrency(currency)
	if err != nil {
		return pricing.Price{}, pricing.ErrInvalidRecord
	}
	money, err := pricing.NewMoney(minor, c)
	if err != nil {
		return pricing.Price{}, pricing.ErrInvalidRecord
	}
	p.ID = pricing.PriceID(id)
	p.OrganizationID = org
	p.WorkspaceID = ws
	p.OfferID = pricing.OfferID(offer)
	p.Kind = pricing.Kind(kind)
	p.Amount = money
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if err := p.Validate(); err != nil {
		return pricing.Price{}, err
	}
	return p, nil
}
func tenantScope(s pricing.Scope) (tenancy.Scope, error) {
	return tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
}
func appendAudit(ctx context.Context, tx *sql.Tx, s pricing.Scope, m pricing.Mutation, p pricing.Price, action string, risk audit.Risk, before *int64) error {
	ts, err := tenantScope(s)
	if err != nil {
		return err
	}
	summary := audit.Summary{"offer_id": p.OfferID.String(), "price_id": p.ID.String(), "kind": string(p.Kind), "currency": p.Amount.Currency().String(), "new_minor_units": p.Amount.MinorUnits(), "version": p.Version}
	if before != nil {
		summary["old_minor_units"] = *before
	}
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	record := audit.Record{ID: m.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: action, ResourceType: "price", ResourceID: p.ID.String(), CorrelationID: m.CorrelationID, Risk: risk, Summary: safe, CreatedAt: m.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, ts, record)
}
func wireMoney(value pricing.Money) (domain.Money, error) {
	currency, err := domain.NewCurrency(value.Currency().String())
	if err != nil {
		return domain.Money{}, err
	}
	return domain.NewMoney(value.MinorUnits(), currency)
}

func enqueuePriceEvent(ctx context.Context, tx *sql.Tx, s pricing.Scope, m pricing.Mutation, p pricing.Price, change string) error {
	amount, err := wireMoney(p.Amount)
	if err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		PriceID string       `json:"price_id"`
		OfferID string       `json:"offer_id"`
		Kind    pricing.Kind `json:"kind"`
		Amount  domain.Money `json:"amount"`
		Version int64        `json:"version"`
		Change  string       `json:"change"`
	}{p.ID.String(), p.OfferID.String(), p.Kind, amount, p.Version, change})
	if err != nil {
		return err
	}
	typ, _ := eventbus.ParseEventType("commerce.pricing.price_changed.v1")
	at, err := domain.NewUTCInstant(m.OccurredAt)
	if err != nil {
		return err
	}
	ev := eventbus.Event{ID: m.EventID, Type: typ, OccurredAt: at, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), EntityType: "price", EntityID: p.ID.String(), Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: data}
	if err := ev.Validate(); err != nil {
		return err
	}
	enq, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enq.Enqueue(ctx, ev)
}

func appendPriceLineage(ctx context.Context, tx *sql.Tx, s pricing.Scope, m pricing.Mutation, p pricing.Price, previous *pricing.Price, change string) error {
	id, err := lineage.DeterministicID(m.EventID)
	if err != nil {
		return err
	}
	ls, err := lineage.NewScope(s.OrganizationID(), s.WorkspaceID())
	if err != nil {
		return err
	}
	inputs := []lineage.Input{{Role: "offer", Ref: lineage.Ref{System: "torgnexa", EntityType: "offer", EntityID: p.OfferID.String()}}}
	if previous != nil {
		at := previous.UpdatedAt.UTC()
		inputs = append(inputs, lineage.Input{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "price", EntityID: previous.ID.String(), Version: lineage.VersionNumber(previous.Version), Field: "amount", ObservedAt: &at}})
	}
	at := p.UpdatedAt.UTC()
	record := lineage.Record{
		ID: id, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), Source: m.Source, ActorID: m.ActorID,
		Operation: "pricing.price." + change,
		Output:    lineage.Ref{System: "torgnexa", EntityType: "price", EntityID: p.ID.String(), Version: lineage.VersionNumber(p.Version), Field: "amount", ObservedAt: &at},
		Inputs:    inputs, Transformation: lineage.Transformation{Kind: "domain_mutation", ID: "pricing." + change, Version: "1"},
		CorrelationID: m.CorrelationID, CausationID: m.CausationID, AuditID: m.AuditID, EventID: m.EventID, Result: lineage.ResultApplied, OccurredAt: m.OccurredAt,
	}
	return lineagerepo.AppendTransaction(ctx, tx, ls, record)
}
