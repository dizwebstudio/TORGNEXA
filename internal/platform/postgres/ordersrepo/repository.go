// Package ordersrepo implements tenant-scoped PostgreSQL Order persistence.
package ordersrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`
const orderSelect = `SELECT id,organization_id,workspace_id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at,version,created_at,updated_at FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

type Repository struct{ database *sql.DB }

var _ orders.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("orders repository: database is required")
	}
	return &Repository{database}, nil
}

func (r *Repository) Order(ctx context.Context, scope orders.Scope, id orders.OrderID) (orders.Order, error) {
	if err := validate(ctx, r, scope); err != nil {
		return orders.Order{}, err
	}
	if !id.Valid() {
		return orders.Order{}, orders.ErrInvalidRecord
	}
	var result orders.Order
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error { var err error; result, err = loadOrder(ctx, tx, scope, id, false); return err })
	return result, err
}

func (r *Repository) Create(ctx context.Context, scope orders.Scope, cmd orders.CreateOrder, mutation orders.Mutation) (orders.Order, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return orders.Order{}, err
	}
	if err := cmd.Validate(); err != nil {
		return orders.Order{}, err
	}
	model, err := orders.BuildCreate(cmd, scope, mutation.OccurredAt)
	if err != nil {
		return orders.Order{}, err
	}
	var result orders.Order
	err = r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		for _, item := range cmd.Items {
			var active bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='active')`, scope.OrganizationID(), scope.WorkspaceID(), item.OfferID.String()).Scan(&active); err != nil {
				return fmt.Errorf("orders repository: validate offer: %w", err)
			}
			if !active {
				return orders.ErrInvalidRecord
			}
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO orders(id,organization_id,workspace_id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at) VALUES($1,$2,$3,$4,'pending',$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING RETURNING id`, model.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), model.Number, model.Currency.String(), model.Subtotal.MinorUnits(), model.DiscountTotal.MinorUnits(), model.TaxTotal.MinorUnits(), model.ShippingTotal.MinorUnits(), model.GrandTotal.MinorUnits(), model.PlacedAt)
		var inserted string
		if err := row.Scan(&inserted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return orders.ErrConflict
			}
			return fmt.Errorf("orders repository: insert order: %w", err)
		}
		for _, item := range model.Items {
			_, err := tx.ExecContext(ctx, `INSERT INTO order_items(id,organization_id,workspace_id,order_id,position,offer_id,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, item.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), model.ID.String(), item.Position, item.OfferID.String(), item.SKU, item.Quantity.Value.Coefficient(), item.Quantity.Value.Scale(), item.Quantity.Unit.String(), item.UnitPrice.MinorUnits(), item.Subtotal.MinorUnits(), item.DiscountTotal.MinorUnits(), item.TaxTotal.MinorUnits(), item.LineTotal.MinorUnits(), item.Tax.Jurisdiction, item.Tax.Category, item.Tax.Rate.Coefficient(), item.Tax.Rate.Scale(), item.Tax.PriceIncludesTax)
			if err != nil {
				return fmt.Errorf("orders repository: insert item: %w", err)
			}
		}
		result, err = loadOrder(ctx, tx, scope, model.ID, false)
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, result, "orders.order.created", audit.RiskWriteSensitive, ""); err != nil {
			return err
		}
		return enqueueOrderEvent(ctx, tx, scope, mutation, result, "created", "")
	})
	return result, err
}

func (r *Repository) ChangeStatus(ctx context.Context, scope orders.Scope, cmd orders.ChangeStatus, mutation orders.Mutation) (orders.Order, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return orders.Order{}, err
	}
	if err := cmd.Validate(); err != nil {
		return orders.Order{}, err
	}
	var result orders.Order
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		current, err := loadOrder(ctx, tx, scope, cmd.ID, true)
		if err != nil {
			return err
		}
		if current.Version != cmd.ExpectedVersion {
			return orders.ErrConflict
		}
		if err := orders.ValidateTransition(current.Status, cmd.Status); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE orders SET status=$4,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5`, scope.OrganizationID(), scope.WorkspaceID(), cmd.ID.String(), string(cmd.Status), cmd.ExpectedVersion)
		if err != nil {
			return fmt.Errorf("orders repository: status update: %w", err)
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return orders.ErrConflict
		}
		result, err = loadOrder(ctx, tx, scope, cmd.ID, false)
		if err != nil {
			return err
		}
		old := string(current.Status)
		if err := appendAudit(ctx, tx, scope, mutation, result, "orders.order.status_changed", audit.RiskWriteSensitive, old); err != nil {
			return err
		}
		return enqueueOrderEvent(ctx, tx, scope, mutation, result, "status_changed", old)
	})
	return result, err
}

func (r *Repository) withTx(ctx context.Context, readOnly bool, scope orders.Scope, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("orders repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var o, w string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID(), scope.WorkspaceID()).Scan(&o, &w); err != nil {
		return fmt.Errorf("orders repository: scope: %w", err)
	}
	if o != scope.OrganizationID() || w != scope.WorkspaceID() {
		return orders.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("orders repository: commit: %w", err)
	}
	return nil
}
func validate(ctx context.Context, r *Repository, s orders.Scope) error {
	if ctx == nil {
		return errors.New("orders repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.database == nil {
		return errors.New("orders repository: uninitialized")
	}
	if !s.Valid() {
		return orders.ErrInvalidScope
	}
	return nil
}
func validateMutation(ctx context.Context, r *Repository, s orders.Scope, m orders.Mutation) error {
	if err := validate(ctx, r, s); err != nil {
		return err
	}
	return m.Validate()
}

type scanner interface{ Scan(...any) error }

func scanOrder(row scanner) (orders.Order, error) {
	var o orders.Order
	var id, org, ws, status, currency string
	var sub, disc, tax, ship, grand int64
	if err := row.Scan(&id, &org, &ws, &o.Number, &status, &currency, &sub, &disc, &tax, &ship, &grand, &o.PlacedAt, &o.Version, &o.CreatedAt, &o.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return orders.Order{}, orders.ErrNotFound
		}
		return orders.Order{}, fmt.Errorf("orders repository: scan order: %w", err)
	}
	c, err := orders.NewCurrency(currency)
	if err != nil {
		return orders.Order{}, orders.ErrInvalidRecord
	}
	o.ID = orders.OrderID(id)
	o.OrganizationID = org
	o.WorkspaceID = ws
	o.Status = orders.Status(status)
	o.Currency = c
	o.Subtotal, _ = orders.NewMoney(sub, c)
	o.DiscountTotal, _ = orders.NewMoney(disc, c)
	o.TaxTotal, _ = orders.NewMoney(tax, c)
	o.ShippingTotal, _ = orders.NewMoney(ship, c)
	o.GrandTotal, _ = orders.NewMoney(grand, c)
	o.PlacedAt = o.PlacedAt.UTC()
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	return o, nil
}
func loadOrder(ctx context.Context, tx *sql.Tx, s orders.Scope, id orders.OrderID, lock bool) (orders.Order, error) {
	stmt := orderSelect
	if lock {
		stmt += " FOR UPDATE"
	}
	o, err := scanOrder(tx.QueryRowContext(ctx, stmt, s.OrganizationID(), s.WorkspaceID(), id.String()))
	if err != nil {
		return orders.Order{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,order_id,offer_id,position,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax,created_at FROM order_items WHERE organization_id=$1 AND workspace_id=$2 AND order_id=$3 ORDER BY position,id`, s.OrganizationID(), s.WorkspaceID(), id.String())
	if err != nil {
		return orders.Order{}, fmt.Errorf("orders repository: items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows, o.Currency)
		if err != nil {
			return orders.Order{}, err
		}
		o.Items = append(o.Items, item)
	}
	if err := rows.Err(); err != nil {
		return orders.Order{}, err
	}
	if err := o.Validate(); err != nil {
		return orders.Order{}, err
	}
	return o, nil
}
func scanItem(row scanner, c orders.Currency) (orders.OrderItem, error) {
	var i orders.OrderItem
	var id, orderID, offerID, unit, jurisdiction, category string
	var qc, up, sub, disc, tax, line, rate int64
	var qs, rs uint8
	var included bool
	if err := row.Scan(&id, &orderID, &offerID, &i.Position, &i.SKU, &qc, &qs, &unit, &up, &sub, &disc, &tax, &line, &jurisdiction, &category, &rate, &rs, &included, &i.CreatedAt); err != nil {
		return orders.OrderItem{}, fmt.Errorf("orders repository: scan item: %w", err)
	}
	d, e := orders.NewDecimal(qc, qs)
	if e != nil {
		return orders.OrderItem{}, e
	}
	u, e := orders.NewUnitCode(unit)
	if e != nil {
		return orders.OrderItem{}, e
	}
	q, e := orders.NewQuantity(d, u)
	if e != nil {
		return orders.OrderItem{}, e
	}
	tr, e := orders.NewDecimal(rate, rs)
	if e != nil {
		return orders.OrderItem{}, e
	}
	i.ID = orders.OrderItemID(id)
	i.OrderID = orders.OrderID(orderID)
	i.OfferID = orders.OfferID(offerID)
	i.Quantity = q
	i.UnitPrice, _ = orders.NewMoney(up, c)
	i.Subtotal, _ = orders.NewMoney(sub, c)
	i.DiscountTotal, _ = orders.NewMoney(disc, c)
	i.TaxTotal, _ = orders.NewMoney(tax, c)
	i.LineTotal, _ = orders.NewMoney(line, c)
	i.Tax = orders.TaxSnapshot{Jurisdiction: jurisdiction, Category: category, Rate: tr, PriceIncludesTax: included}
	i.CreatedAt = i.CreatedAt.UTC()
	if err := i.Validate(); err != nil {
		return orders.OrderItem{}, err
	}
	return i, nil
}

func tenantScope(s orders.Scope) (tenancy.Scope, error) {
	return tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
}
func appendAudit(ctx context.Context, tx *sql.Tx, s orders.Scope, m orders.Mutation, o orders.Order, action string, risk audit.Risk, oldStatus string) error {
	ts, err := tenantScope(s)
	if err != nil {
		return err
	}
	summary := audit.Summary{"order_id": o.ID.String(), "order_number": o.Number, "status": string(o.Status), "currency": o.Currency.String(), "grand_minor_units": o.GrandTotal.MinorUnits(), "item_count": len(o.Items), "version": o.Version}
	if oldStatus != "" {
		summary["old_status"] = oldStatus
	}
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	rec := audit.Record{ID: m.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: action, ResourceType: "order", ResourceID: o.ID.String(), CorrelationID: m.CorrelationID, Risk: risk, Summary: safe, CreatedAt: m.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, ts, rec)
}

func wireMoney(m orders.Money) (domain.Money, error) {
	c, err := domain.NewCurrency(m.Currency().String())
	if err != nil {
		return domain.Money{}, err
	}
	return domain.NewMoney(m.MinorUnits(), c)
}
func wireQuantity(q orders.Quantity) (domain.Quantity, error) {
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
func wireTax(t orders.TaxSnapshot) (domain.TaxTreatment, error) {
	d, err := domain.NewDecimal(t.Rate.Coefficient(), t.Rate.Scale())
	if err != nil {
		return domain.TaxTreatment{}, err
	}
	v := domain.TaxTreatment{Jurisdiction: t.Jurisdiction, Category: domain.TaxCategory(t.Category), RateFraction: &d, PriceIncludes: t.PriceIncludesTax}
	if err := v.Validate(); err != nil {
		return domain.TaxTreatment{}, err
	}
	return v, nil
}

type eventItem struct {
	ItemID    string              `json:"item_id"`
	OfferID   string              `json:"offer_id"`
	SKU       string              `json:"sku"`
	Quantity  domain.Quantity     `json:"quantity"`
	UnitPrice domain.Money        `json:"unit_price"`
	LineTotal domain.Money        `json:"line_total"`
	Tax       domain.TaxTreatment `json:"tax"`
}

func enqueueOrderEvent(ctx context.Context, tx *sql.Tx, s orders.Scope, m orders.Mutation, o orders.Order, change, oldStatus string) error {
	total, err := wireMoney(o.GrandTotal)
	if err != nil {
		return err
	}
	items := make([]eventItem, 0, len(o.Items))
	for _, i := range o.Items {
		q, e := wireQuantity(i.Quantity)
		if e != nil {
			return e
		}
		up, e := wireMoney(i.UnitPrice)
		if e != nil {
			return e
		}
		lt, e := wireMoney(i.LineTotal)
		if e != nil {
			return e
		}
		tax, e := wireTax(i.Tax)
		if e != nil {
			return e
		}
		items = append(items, eventItem{i.ID.String(), i.OfferID.String(), i.SKU, q, up, lt, tax})
	}
	data, err := json.Marshal(struct {
		OrderID   string        `json:"order_id"`
		Number    string        `json:"number"`
		Status    orders.Status `json:"status"`
		OldStatus string        `json:"old_status,omitempty"`
		Total     domain.Money  `json:"total"`
		ItemCount int           `json:"item_count"`
		Items     []eventItem   `json:"items"`
		Version   int64         `json:"version"`
		Change    string        `json:"change"`
	}{o.ID.String(), o.Number, o.Status, oldStatus, total, len(items), items, o.Version, change})
	if err != nil {
		return err
	}
	typ, _ := eventbus.ParseEventType("commerce.orders.order_changed.v1")
	at, err := domain.NewUTCInstant(m.OccurredAt)
	if err != nil {
		return err
	}
	ev := eventbus.Event{ID: m.EventID, Type: typ, OccurredAt: at, OrganizationID: s.OrganizationID(), WorkspaceID: s.WorkspaceID(), EntityType: "order", EntityID: o.ID.String(), Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: data}
	if err := ev.Validate(); err != nil {
		return err
	}
	enq, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enq.Enqueue(ctx, ev)
}
