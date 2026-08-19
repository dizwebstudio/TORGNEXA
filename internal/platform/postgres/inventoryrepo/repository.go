// Package inventoryrepo implements tenant-scoped PostgreSQL inventory persistence.
package inventoryrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/lineagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`
const warehouseSelect = `SELECT id,organization_id,workspace_id,code,name,status,version,created_at,updated_at FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
const positionSelect = `SELECT id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

type Repository struct{ database *sql.DB }

// PositionView joins canonical stock with its offer, product and warehouse labels.
type PositionView struct {
	ID, OfferID, WarehouseID, SKU, ProductTitle, WarehouseCode, WarehouseName string
	OnHand, Reserved, Available, Unit                                         string
	Version                                                                   int64
	UpdatedAt                                                                 string
}

// MovementView is an immutable stock-change projection sourced from audit evidence.
type MovementView struct {
	ID, Action, Change, Reason, OldOnHand, OnHand, OldReserved, Reserved, Unit, ActorID, OccurredAt string
}

// ListPositionViews returns a bounded tenant-scoped stock projection.
func (r *Repository) ListPositionViews(ctx context.Context, s inventory.Scope, limit int) ([]PositionView, error) {
	if e := validate(ctx, r, s); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 100 {
		return nil, inventory.ErrInvalidRecord
	}
	result := make([]PositionView, 0)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT i.id,i.offer_id,i.warehouse_id,o.sku,p.title,w.code,w.name,i.on_hand_coefficient,i.on_hand_scale,i.reserved_coefficient,i.reserved_scale,i.unit,i.version,i.updated_at FROM inventory_positions i JOIN offers o ON o.organization_id=i.organization_id AND o.workspace_id=i.workspace_id AND o.id=i.offer_id JOIN products p ON p.organization_id=o.organization_id AND p.workspace_id=o.workspace_id AND p.id=o.product_id JOIN warehouses w ON w.organization_id=i.organization_id AND w.workspace_id=i.workspace_id AND w.id=i.warehouse_id WHERE i.organization_id=$1 AND i.workspace_id=$2 AND (o.sku<>'DEMO-SKU' OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)) ORDER BY i.updated_at DESC,i.id DESC LIMIT $3`, s.OrganizationID(), s.WorkspaceID(), limit)
		if e != nil {
			return fmt.Errorf("inventory repository: list positions: %w", e)
		}
		defer rows.Close()
		for rows.Next() {
			var v PositionView
			var on, res int64
			var os, rs uint8
			var updated time.Time
			if e := rows.Scan(&v.ID, &v.OfferID, &v.WarehouseID, &v.SKU, &v.ProductTitle, &v.WarehouseCode, &v.WarehouseName, &on, &os, &res, &rs, &v.Unit, &v.Version, &updated); e != nil {
				return fmt.Errorf("inventory repository: scan position view: %w", e)
			}
			od, e := inventory.NewDecimal(on, os)
			if e != nil {
				return e
			}
			rd, e := inventory.NewDecimal(res, rs)
			if e != nil {
				return e
			}
			available, e := od.Sub(rd)
			if e != nil {
				return e
			}
			v.OnHand, v.Reserved, v.Available, v.UpdatedAt = od.String(), rd.String(), available.String(), updated.UTC().Format(time.RFC3339Nano)
			result = append(result, v)
		}
		return rows.Err()
	})
	return result, err
}

// ListWarehouses returns canonical tenant warehouses.
func (r *Repository) ListWarehouses(ctx context.Context, s inventory.Scope, limit int) ([]inventory.Warehouse, error) {
	if e := validate(ctx, r, s); e != nil || limit < 1 || limit > 500 {
		return nil, inventory.ErrInvalidRecord
	}
	var out []inventory.Warehouse
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,code,name,status,version,created_at,updated_at FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 ORDER BY name,id LIMIT $3`, s.OrganizationID(), s.WorkspaceID(), limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanWarehouse(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// PositionViewByID returns the labelled stock card projection.
func (r *Repository) PositionViewByID(ctx context.Context, s inventory.Scope, id string) (PositionView, error) {
	items, e := r.ListPositionViews(ctx, s, 100)
	if e != nil {
		return PositionView{}, e
	}
	for _, v := range items {
		if v.ID == id {
			return v, nil
		}
	}
	return PositionView{}, inventory.ErrNotFound
}

// ResolveImportParents resolves canonical offer and warehouse identities without trusting tenant ids from import rows.
func (r *Repository) ResolveImportParents(ctx context.Context, s inventory.Scope, sku, warehouseCode string) (inventory.OfferID, inventory.WarehouseID, error) {
	var offer, warehouse string
	e := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT id FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND sku=$3 AND status<>'archived'`, s.OrganizationID(), s.WorkspaceID(), sku).Scan(&offer); e != nil {
			return e
		}
		return tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND code=$3 AND status='active'`, s.OrganizationID(), s.WorkspaceID(), warehouseCode).Scan(&warehouse)
	})
	if e != nil {
		return "", "", e
	}
	return inventory.OfferID(offer), inventory.WarehouseID(warehouse), nil
}

// PositionByParents finds an existing canonical position for import upsert.
func (r *Repository) PositionByParents(ctx context.Context, s inventory.Scope, offer inventory.OfferID, warehouse inventory.WarehouseID) (inventory.Position, error) {
	var out inventory.Position
	e := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanPosition(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND offer_id=$3 AND warehouse_id=$4`, s.OrganizationID(), s.WorkspaceID(), offer.String(), warehouse.String()))
		return e
	})
	return out, e
}

// MovementHistory returns append-only audit evidence for a position.
func (r *Repository) MovementHistory(ctx context.Context, s inventory.Scope, positionID string, limit int) ([]MovementView, error) {
	if limit < 1 || limit > 500 {
		return nil, inventory.ErrInvalidRecord
	}
	var out []MovementView
	e := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,action,COALESCE(summary->>'change',''),COALESCE(summary->>'reason',''),COALESCE(summary->>'old_on_hand',''),COALESCE(summary->>'on_hand',''),COALESCE(summary->>'old_reserved',''),COALESCE(summary->>'reserved',''),COALESCE(summary->>'unit',''),COALESCE(actor_id,''),created_at FROM audit_records WHERE organization_id=$1 AND workspace_id=$2 AND resource_type='inventory_position' AND resource_id=$3 ORDER BY created_at DESC,id DESC LIMIT $4`, s.OrganizationID(), s.WorkspaceID(), positionID, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v MovementView
			var at time.Time
			if e := rows.Scan(&v.ID, &v.Action, &v.Change, &v.Reason, &v.OldOnHand, &v.OnHand, &v.OldReserved, &v.Reserved, &v.Unit, &v.ActorID, &at); e != nil {
				return e
			}
			v.OccurredAt = at.UTC().Format(time.RFC3339Nano)
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}

var _ inventory.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("inventory repository: database is required")
	}
	return &Repository{db}, nil
}
func (r *Repository) Warehouse(ctx context.Context, s inventory.Scope, id inventory.WarehouseID) (inventory.Warehouse, error) {
	if e := validate(ctx, r, s); e != nil {
		return inventory.Warehouse{}, e
	}
	var out inventory.Warehouse
	e := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanWarehouse(tx.QueryRowContext(ctx, warehouseSelect, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, e
}
func (r *Repository) Position(ctx context.Context, s inventory.Scope, id inventory.PositionID) (inventory.Position, error) {
	if e := validate(ctx, r, s); e != nil {
		return inventory.Position{}, e
	}
	var out inventory.Position
	e := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var e error
		out, e = scanPosition(tx.QueryRowContext(ctx, positionSelect, s.OrganizationID(), s.WorkspaceID(), id.String()))
		return e
	})
	return out, e
}
func (r *Repository) CreateWarehouse(ctx context.Context, s inventory.Scope, c inventory.CreateWarehouse, m inventory.Mutation) (inventory.Warehouse, error) {
	if e := validateMutation(ctx, r, s, m); e != nil {
		return inventory.Warehouse{}, e
	}
	if e := c.Validate(); e != nil {
		return inventory.Warehouse{}, e
	}
	var out inventory.Warehouse
	e := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		w, e := scanWarehouse(tx.QueryRowContext(ctx, `INSERT INTO warehouses(id,organization_id,workspace_id,code,name) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,code,name,status,version,created_at,updated_at`, c.ID.String(), s.OrganizationID(), s.WorkspaceID(), c.Code, c.Name))
		if errors.Is(e, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if e != nil {
			return e
		}
		out = w
		if e := appendAudit(ctx, tx, s, m, "inventory.warehouse.created", "warehouse", out.ID.String(), audit.RiskWriteSafe, audit.Summary{"warehouse_id": out.ID.String(), "code": out.Code, "status": string(out.Status), "version": out.Version}); e != nil {
			return e
		}
		return enqueueWarehouseEvent(ctx, tx, s, m, out, "created")
	})
	return out, e
}
func (r *Repository) ChangeWarehouseStatus(ctx context.Context, s inventory.Scope, c inventory.ChangeWarehouseStatus, m inventory.Mutation) (inventory.Warehouse, error) {
	if e := validateMutation(ctx, r, s, m); e != nil {
		return inventory.Warehouse{}, e
	}
	if e := c.Validate(); e != nil {
		return inventory.Warehouse{}, e
	}
	var out inventory.Warehouse
	e := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		cur, e := scanWarehouse(tx.QueryRowContext(ctx, warehouseSelect+` FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), c.ID.String()))
		if e != nil {
			return e
		}
		if cur.Version != c.ExpectedVersion {
			return inventory.ErrConflict
		}
		if cur.Status == c.Status {
			return inventory.ErrInvalidRecord
		}
		out, e = scanWarehouse(tx.QueryRowContext(ctx, `UPDATE warehouses SET status=$4,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 RETURNING id,organization_id,workspace_id,code,name,status,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), c.ID.String(), string(c.Status), c.ExpectedVersion))
		if e != nil {
			return e
		}
		if e := appendAudit(ctx, tx, s, m, "inventory.warehouse.status_changed", "warehouse", out.ID.String(), audit.RiskWriteSafe, audit.Summary{"old_status": string(cur.Status), "new_status": string(out.Status), "version": out.Version}); e != nil {
			return e
		}
		return enqueueWarehouseEvent(ctx, tx, s, m, out, "status_changed")
	})
	return out, e
}
func (r *Repository) CreatePosition(ctx context.Context, s inventory.Scope, c inventory.CreatePosition, m inventory.Mutation) (inventory.Position, error) {
	if e := validateMutation(ctx, r, s, m); e != nil {
		return inventory.Position{}, e
	}
	if e := c.Validate(); e != nil {
		return inventory.Position{}, e
	}
	var out inventory.Position
	e := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var ok bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status <> 'archived')`, s.OrganizationID(), s.WorkspaceID(), c.OfferID.String()).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return inventory.ErrInvalidRecord
		}
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='active')`, s.OrganizationID(), s.WorkspaceID(), c.WarehouseID.String()).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return inventory.ErrWarehouseInactive
		}
		p, e := scanPosition(tx.QueryRowContext(ctx, `INSERT INTO inventory_positions(id,organization_id,workspace_id,offer_id,warehouse_id,unit) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at`, c.ID.String(), s.OrganizationID(), s.WorkspaceID(), c.OfferID.String(), c.WarehouseID.String(), c.Unit.String()))
		if errors.Is(e, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if e != nil {
			return e
		}
		out = p
		if e := appendPositionAudit(ctx, tx, s, m, out, "inventory.position.created", nil, "created", ""); e != nil {
			return e
		}
		if e := enqueuePositionEvent(ctx, tx, s, m, out, "created", ""); e != nil {
			return e
		}
		return appendPositionLineage(ctx, tx, s, m, out, nil, "created")
	})
	return out, e
}
func (r *Repository) SetOnHand(ctx context.Context, s inventory.Scope, c inventory.ChangeQuantity, m inventory.Mutation) (inventory.Position, error) {
	return r.change(ctx, s, c, m, "on_hand_set", func(p inventory.Position) (inventory.Quantity, inventory.Quantity, error) {
		if c.Quantity.Unit != p.OnHand.Unit {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInvalidRecord
		}
		cmp, _ := c.Quantity.Value.Cmp(p.Reserved.Value)
		if cmp < 0 {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInsufficientAvailable
		}
		return c.Quantity, p.Reserved, nil
	})
}
func (r *Repository) Reserve(ctx context.Context, s inventory.Scope, c inventory.ChangeQuantity, m inventory.Mutation) (inventory.Position, error) {
	return r.change(ctx, s, c, m, "reserved", func(p inventory.Position) (inventory.Quantity, inventory.Quantity, error) {
		if c.Quantity.Unit != p.OnHand.Unit {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInvalidRecord
		}
		nr, e := p.Reserved.Value.Add(c.Quantity.Value)
		if e != nil {
			return inventory.Quantity{}, inventory.Quantity{}, e
		}
		cmp, _ := nr.Cmp(p.OnHand.Value)
		if cmp > 0 {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInsufficientAvailable
		}
		q, _ := inventory.NewQuantity(nr, p.OnHand.Unit)
		return p.OnHand, q, nil
	})
}
func (r *Repository) Release(ctx context.Context, s inventory.Scope, c inventory.ChangeQuantity, m inventory.Mutation) (inventory.Position, error) {
	return r.change(ctx, s, c, m, "released", func(p inventory.Position) (inventory.Quantity, inventory.Quantity, error) {
		if c.Quantity.Unit != p.OnHand.Unit {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInvalidRecord
		}
		cmp, _ := c.Quantity.Value.Cmp(p.Reserved.Value)
		if cmp > 0 {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInsufficientReserved
		}
		nr, e := p.Reserved.Value.Sub(c.Quantity.Value)
		if e != nil {
			return inventory.Quantity{}, inventory.Quantity{}, e
		}
		q, _ := inventory.NewQuantity(nr, p.OnHand.Unit)
		return p.OnHand, q, nil
	})
}
func (r *Repository) ConsumeReserved(ctx context.Context, s inventory.Scope, c inventory.ChangeQuantity, m inventory.Mutation) (inventory.Position, error) {
	return r.change(ctx, s, c, m, "consumed_reserved", func(p inventory.Position) (inventory.Quantity, inventory.Quantity, error) {
		if c.Quantity.Unit != p.OnHand.Unit {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInvalidRecord
		}
		cmp, _ := c.Quantity.Value.Cmp(p.Reserved.Value)
		if cmp > 0 {
			return inventory.Quantity{}, inventory.Quantity{}, inventory.ErrInsufficientReserved
		}
		no, e := p.OnHand.Value.Sub(c.Quantity.Value)
		if e != nil {
			return inventory.Quantity{}, inventory.Quantity{}, e
		}
		nr, e := p.Reserved.Value.Sub(c.Quantity.Value)
		if e != nil {
			return inventory.Quantity{}, inventory.Quantity{}, e
		}
		oq, _ := inventory.NewQuantity(no, p.OnHand.Unit)
		rq, _ := inventory.NewQuantity(nr, p.OnHand.Unit)
		return oq, rq, nil
	})
}

type changeFn func(inventory.Position) (inventory.Quantity, inventory.Quantity, error)

func (r *Repository) change(ctx context.Context, s inventory.Scope, c inventory.ChangeQuantity, m inventory.Mutation, change string, fn changeFn) (inventory.Position, error) {
	if e := validateMutation(ctx, r, s, m); e != nil {
		return inventory.Position{}, e
	}
	if e := c.Validate(); e != nil {
		return inventory.Position{}, e
	}
	var out inventory.Position
	e := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		cur, e := scanPosition(tx.QueryRowContext(ctx, positionSelect+` FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), c.ID.String()))
		if e != nil {
			return e
		}
		if cur.Version != c.ExpectedVersion {
			return inventory.ErrConflict
		}
		if change != "released" {
			var active bool
			if e := tx.QueryRowContext(ctx, `SELECT status='active' FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID(), s.WorkspaceID(), cur.WarehouseID.String()).Scan(&active); e != nil {
				return e
			}
			if !active {
				return inventory.ErrWarehouseInactive
			}
		}
		on, res, e := fn(cur)
		if e != nil {
			return e
		}
		out, e = scanPosition(tx.QueryRowContext(ctx, `UPDATE inventory_positions SET on_hand_coefficient=$4,on_hand_scale=$5,reserved_coefficient=$6,reserved_scale=$7,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$8 RETURNING id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), c.ID.String(), on.Value.Coefficient(), on.Value.Scale(), res.Value.Coefficient(), res.Value.Scale(), c.ExpectedVersion))
		if e != nil {
			return e
		}
		if e := appendPositionAudit(ctx, tx, s, m, out, "inventory.position."+change, &cur, change, c.Reason); e != nil {
			return e
		}
		if e := enqueuePositionEvent(ctx, tx, s, m, out, change, c.Reason); e != nil {
			return e
		}
		return appendPositionLineage(ctx, tx, s, m, out, &cur, change)
	})
	return out, e
}
func (r *Repository) withTx(ctx context.Context, ro bool, s inventory.Scope, fn func(*sql.Tx) error) error {
	tx, e := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: ro})
	if e != nil {
		return fmt.Errorf("inventory repository: begin: %w", e)
	}
	defer func() { _ = tx.Rollback() }()
	var o, w string
	if e := tx.QueryRowContext(ctx, applyScopeStatement, s.OrganizationID(), s.WorkspaceID()).Scan(&o, &w); e != nil {
		return e
	}
	if o != s.OrganizationID() || w != s.WorkspaceID() {
		return inventory.ErrInvalidScope
	}
	if e := fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func validate(ctx context.Context, r *Repository, s inventory.Scope) error {
	if ctx == nil {
		return errors.New("inventory repository: context required")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if r == nil || r.database == nil {
		return errors.New("inventory repository: uninitialized")
	}
	if !s.Valid() {
		return inventory.ErrInvalidScope
	}
	return nil
}
func validateMutation(ctx context.Context, r *Repository, s inventory.Scope, m inventory.Mutation) error {
	if e := validate(ctx, r, s); e != nil {
		return e
	}
	return m.Validate()
}

type scanner interface{ Scan(...any) error }

func scanWarehouse(row scanner) (inventory.Warehouse, error) {
	var w inventory.Warehouse
	var id, org, ws, status string
	if e := row.Scan(&id, &org, &ws, &w.Code, &w.Name, &status, &w.Version, &w.CreatedAt, &w.UpdatedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return inventory.Warehouse{}, inventory.ErrNotFound
		}
		return inventory.Warehouse{}, e
	}
	w.ID = inventory.WarehouseID(id)
	w.OrganizationID = org
	w.WorkspaceID = ws
	w.Status = inventory.WarehouseStatus(status)
	w.CreatedAt = w.CreatedAt.UTC()
	w.UpdatedAt = w.UpdatedAt.UTC()
	if e := w.Validate(); e != nil {
		return inventory.Warehouse{}, e
	}
	return w, nil
}
func scanPosition(row scanner) (inventory.Position, error) {
	var p inventory.Position
	var id, org, ws, offer, warehouse, unit string
	var oc, rc int64
	var os, rs uint8
	if e := row.Scan(&id, &org, &ws, &offer, &warehouse, &oc, &os, &rc, &rs, &unit, &p.Version, &p.CreatedAt, &p.UpdatedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return inventory.Position{}, inventory.ErrNotFound
		}
		return inventory.Position{}, e
	}
	od, e := inventory.NewDecimal(oc, os)
	if e != nil {
		return inventory.Position{}, inventory.ErrInvalidRecord
	}
	rd, e := inventory.NewDecimal(rc, rs)
	if e != nil {
		return inventory.Position{}, inventory.ErrInvalidRecord
	}
	u, e := inventory.NewUnitCode(unit)
	if e != nil {
		return inventory.Position{}, inventory.ErrInvalidRecord
	}
	p.OnHand, _ = inventory.NewQuantity(od, u)
	p.Reserved, _ = inventory.NewQuantity(rd, u)
	p.ID = inventory.PositionID(id)
	p.OrganizationID = org
	p.WorkspaceID = ws
	p.OfferID = inventory.OfferID(offer)
	p.WarehouseID = inventory.WarehouseID(warehouse)
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if e := p.Validate(); e != nil {
		return inventory.Position{}, e
	}
	return p, nil
}
func tenantScope(s inventory.Scope) (tenancy.Scope, error) {
	return tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
}
func appendAudit(ctx context.Context, tx *sql.Tx, s inventory.Scope, m inventory.Mutation, action, resourceType, resourceID string, risk audit.Risk, summary audit.Summary) error {
	ts, e := tenantScope(s)
	if e != nil {
		return e
	}
	safe, e := audit.SanitizeSummary(summary)
	if e != nil {
		return e
	}
	record := audit.Record{ID: m.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: m.CorrelationID, Risk: risk, Summary: safe, CreatedAt: m.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, ts, record)
}
func appendPositionAudit(ctx context.Context, tx *sql.Tx, s inventory.Scope, m inventory.Mutation, p inventory.Position, action string, before *inventory.Position, change, reason string) error {
	a, _ := p.Available()
	summary := audit.Summary{"offer_id": p.OfferID.String(), "warehouse_id": p.WarehouseID.String(), "position_id": p.ID.String(), "on_hand": p.OnHand.Value.String(), "reserved": p.Reserved.Value.String(), "available": a.Value.String(), "unit": p.OnHand.Unit.String(), "version": p.Version, "change": change}
	if reason != "" {
		summary["reason"] = reason
	}
	if before != nil {
		ba, _ := before.Available()
		summary["old_on_hand"] = before.OnHand.Value.String()
		summary["old_reserved"] = before.Reserved.Value.String()
		summary["old_available"] = ba.Value.String()
	}
	return appendAudit(ctx, tx, s, m, action, "inventory_position", p.ID.String(), audit.RiskWriteSafe, summary)
}
func eventBase(s inventory.Scope, m inventory.Mutation, typeValue, entityType, entityID string, data json.RawMessage) (eventbus.Event, error) {
	typ, e := eventbus.ParseEventType(typeValue)
	if e != nil {
		return eventbus.Event{}, e
	}
	at, e := domain.NewUTCInstant(m.OccurredAt)
	if e != nil {
		return eventbus.Event{}, e
	}
	ev := eventbus.Event{ID: m.EventID, Type: typ, OccurredAt: at, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: data}
	return ev, ev.Validate()
}
func enqueue(ctx context.Context, tx *sql.Tx, ev eventbus.Event) error {
	e, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return e.Enqueue(ctx, ev)
}
func enqueueWarehouseEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, m inventory.Mutation, w inventory.Warehouse, change string) error {
	data, _ := json.Marshal(struct {
		WarehouseID string                    `json:"warehouse_id"`
		Status      inventory.WarehouseStatus `json:"status"`
		Version     int64                     `json:"version"`
		Change      string                    `json:"change"`
	}{w.ID.String(), w.Status, w.Version, change})
	ev, e := eventBase(s, m, "commerce.inventory.warehouse_changed.v1", "warehouse", w.ID.String(), data)
	if e != nil {
		return e
	}
	return enqueue(ctx, tx, ev)
}
func wireQuantity(q inventory.Quantity) (domain.Quantity, error) {
	d, err := domain.NewDecimal(q.Value.Coefficient(), q.Value.Scale())
	if err != nil {
		return domain.Quantity{}, err
	}
	u, err := domain.NewUnitCode(q.Unit.String())
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.NewQuantity(d, u)
}

func enqueuePositionEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, m inventory.Mutation, p inventory.Position, change, reason string) error {
	a, err := p.Available()
	if err != nil {
		return err
	}
	onHand, err := wireQuantity(p.OnHand)
	if err != nil {
		return err
	}
	reserved, err := wireQuantity(p.Reserved)
	if err != nil {
		return err
	}
	available, err := wireQuantity(a)
	if err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		PositionID  string          `json:"position_id"`
		OfferID     string          `json:"offer_id"`
		WarehouseID string          `json:"warehouse_id"`
		OnHand      domain.Quantity `json:"on_hand"`
		Reserved    domain.Quantity `json:"reserved"`
		Available   domain.Quantity `json:"available"`
		Version     int64           `json:"version"`
		Change      string          `json:"change"`
		Reason      string          `json:"reason,omitempty"`
	}{p.ID.String(), p.OfferID.String(), p.WarehouseID.String(), onHand, reserved, available, p.Version, change, reason})
	if err != nil {
		return err
	}
	ev, err := eventBase(s, m, "commerce.inventory.position_changed.v1", "inventory_position", p.ID.String(), data)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, ev)
}

func appendPositionLineage(ctx context.Context, tx *sql.Tx, s inventory.Scope, m inventory.Mutation, p inventory.Position, previous *inventory.Position, change string) error {
	id, err := lineage.DeterministicID(m.EventID)
	if err != nil {
		return err
	}
	ls, err := lineage.NewScope(s.OrganizationID(), s.WorkspaceID())
	if err != nil {
		return err
	}
	inputs := []lineage.Input{
		{Role: "offer", Ref: lineage.Ref{System: "torgnexa", EntityType: "offer", EntityID: p.OfferID.String()}},
		{Role: "warehouse", Ref: lineage.Ref{System: "torgnexa", EntityType: "warehouse", EntityID: p.WarehouseID.String()}},
	}
	if previous != nil {
		at := previous.UpdatedAt.UTC()
		inputs = append(inputs, lineage.Input{Role: "previous", Ref: lineage.Ref{System: "torgnexa", EntityType: "inventory_position", EntityID: previous.ID.String(), Version: lineage.VersionNumber(previous.Version), Field: "stock", ObservedAt: &at}})
	}
	at := p.UpdatedAt.UTC()
	record := lineage.Record{
		ID: id, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), Source: m.Source, ActorID: m.ActorID,
		Operation: "inventory.position." + change,
		Output:    lineage.Ref{System: "torgnexa", EntityType: "inventory_position", EntityID: p.ID.String(), Version: lineage.VersionNumber(p.Version), Field: "stock", ObservedAt: &at},
		Inputs:    inputs, Transformation: lineage.Transformation{Kind: "domain_mutation", ID: "inventory." + change, Version: "1"},
		CorrelationID: m.CorrelationID, CausationID: m.CausationID, AuditID: m.AuditID, EventID: m.EventID, Result: lineage.ResultApplied, OccurredAt: m.OccurredAt,
	}
	return lineagerepo.AppendTransaction(ctx, tx, ls, record)
}
