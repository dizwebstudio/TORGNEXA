// Package searchrepo implements the PostgreSQL MVP adapter for TORGNEXA search.
package searchrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/search"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

const productSearchSQL = `
WITH search_input AS (
  SELECT CASE WHEN $3='' THEN NULL::tsquery ELSE websearch_to_tsquery('simple'::regconfig,$3) END AS tsq
), ranked AS (
  SELECT p.id,p.code,p.title,p.status,p.updated_at,
    CASE
      WHEN $3='' THEN 2
      WHEN lower(p.code)=lower($3) OR EXISTS (
        SELECT 1 FROM offers e
        WHERE e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
          AND (lower(e.sku)=lower($3) OR lower(COALESCE(e.gtin,''))=lower($3))
      ) THEN 0
      WHEN lower(p.code) LIKE lower($9) ESCAPE E'\\' OR lower(p.title) LIKE lower($9) ESCAPE E'\\' OR EXISTS (
        SELECT 1 FROM offers e
        WHERE e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
          AND (lower(e.sku) LIKE lower($9) ESCAPE E'\\' OR lower(COALESCE(e.gtin,'')) LIKE lower($9) ESCAPE E'\\')
      ) THEN 1
      ELSE 2
    END AS priority
  FROM products p CROSS JOIN search_input s
  WHERE p.organization_id=$1 AND p.workspace_id=$2
    AND (p.code<>'DEMO-PRODUCT' OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2))
    AND ($4='' OR p.status=$4)
    AND (
      $3='' OR lower(p.code)=lower($3)
      OR lower(p.code) LIKE lower($9) ESCAPE E'\\'
      OR lower(p.title) LIKE lower($9) ESCAPE E'\\'
      OR search_product_vector(p.code,p.title,p.description) @@ s.tsq
      OR EXISTS (
        SELECT 1 FROM offers e
        WHERE e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
          AND (
            lower(e.sku)=lower($3)
            OR lower(COALESCE(e.gtin,''))=lower($3)
            OR lower(e.sku) LIKE lower($9) ESCAPE E'\\'
            OR lower(COALESCE(e.gtin,'')) LIKE lower($9) ESCAPE E'\\'
            OR search_offer_vector(e.sku,e.gtin) @@ s.tsq
          )
      )
    )
)
SELECT id,code,title,status,updated_at,priority
FROM ranked
WHERE $5='' OR priority>$6 OR (priority=$6 AND (updated_at<$7 OR (updated_at=$7 AND id<$5)))
ORDER BY priority ASC,updated_at DESC,id DESC
LIMIT $8`

const orderSearchSQL = `
WITH search_input AS (
  SELECT CASE WHEN $3='' THEN NULL::tsquery ELSE websearch_to_tsquery('simple'::regconfig,$3) END AS tsq
), ranked AS (
  SELECT o.id,o.order_number,o.status,o.currency,o.grand_minor_units,o.placed_at,o.updated_at,
    CASE
      WHEN $3='' THEN 2
      WHEN lower(o.order_number)=lower($3) OR EXISTS (
        SELECT 1 FROM order_items i
        WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
          AND lower(i.sku_snapshot)=lower($3)
      ) THEN 0
      WHEN lower(o.order_number) LIKE lower($11) ESCAPE E'\\' OR EXISTS (
        SELECT 1 FROM order_items i
        WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
          AND lower(i.sku_snapshot) LIKE lower($11) ESCAPE E'\\'
      ) THEN 1
      ELSE 2
    END AS priority
  FROM orders o CROSS JOIN search_input s
  WHERE o.organization_id=$1 AND o.workspace_id=$2
    AND (o.order_number NOT IN ('DEMO-001','DEMO-002','DEMO-003','DEMO-004','DEMO-005') OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2))
    AND ($4='' OR o.status=$4)
    AND ($8::timestamptz IS NULL OR o.placed_at >= $8)
    AND ($9::timestamptz IS NULL OR o.placed_at < $9)
    AND (
      $3='' OR lower(o.order_number)=lower($3)
      OR lower(o.order_number) LIKE lower($11) ESCAPE E'\\'
      OR search_order_vector(o.order_number) @@ s.tsq
      OR EXISTS (
        SELECT 1 FROM order_items i
        WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
          AND (
            lower(i.sku_snapshot)=lower($3)
            OR lower(i.sku_snapshot) LIKE lower($11) ESCAPE E'\\'
            OR search_order_item_vector(i.sku_snapshot) @@ s.tsq
          )
      )
    )
)
SELECT id,order_number,status,currency,grand_minor_units,placed_at,updated_at,priority
FROM ranked
WHERE $5='' OR priority>$6 OR (priority=$6 AND (updated_at<$7 OR (updated_at=$7 AND id<$5)))
ORDER BY priority ASC,updated_at DESC,id DESC
LIMIT $10`

type Repository struct{ db *sql.DB }

type OrderDetail struct {
	ID                 string            `json:"id"`
	OrderNumber        string            `json:"order_number"`
	Status             string            `json:"status"`
	Currency           string            `json:"currency"`
	SubtotalMinorUnits int64             `json:"subtotal_minor_units"`
	DiscountMinorUnits int64             `json:"discount_minor_units"`
	TaxMinorUnits      int64             `json:"tax_minor_units"`
	ShippingMinorUnits int64             `json:"shipping_minor_units"`
	GrandMinorUnits    int64             `json:"grand_minor_units"`
	PlacedAt           time.Time         `json:"placed_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	Items              []OrderDetailItem `json:"items"`
	Sources            []OrderSource     `json:"sources"`
}
type OrderDetailItem struct {
	SKU                 string `json:"sku"`
	QuantityCoefficient int64  `json:"quantity_coefficient"`
	QuantityScale       int16  `json:"quantity_scale"`
	Unit                string `json:"unit"`
	UnitPriceMinorUnits int64  `json:"unit_price_minor_units"`
	LineTotalMinorUnits int64  `json:"line_total_minor_units"`
}
type OrderSource struct {
	Provider string `json:"provider"`
	RemoteID string `json:"remote_id"`
}

func (r *Repository) OrderDetail(ctx context.Context, scope tenancy.Scope, id string) (OrderDetail, error) {
	if id == "" {
		return OrderDetail{}, search.ErrInvalid
	}
	var out OrderDetail
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at,updated_at FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND (order_number NOT IN ('DEMO-001','DEMO-002','DEMO-003','DEMO-004','DEMO-005') OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2))`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id).Scan(&out.ID, &out.OrderNumber, &out.Status, &out.Currency, &out.SubtotalMinorUnits, &out.DiscountMinorUnits, &out.TaxMinorUnits, &out.ShippingMinorUnits, &out.GrandMinorUnits, &out.PlacedAt, &out.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,line_total_minor_units FROM order_items WHERE organization_id=$1 AND workspace_id=$2 AND order_id=$3 ORDER BY position`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		if err != nil {
			return err
		}
		defer rows.Close()
		out.Items = []OrderDetailItem{}
		for rows.Next() {
			var item OrderDetailItem
			if err := rows.Scan(&item.SKU, &item.QuantityCoefficient, &item.QuantityScale, &item.Unit, &item.UnitPriceMinorUnits, &item.LineTotalMinorUnits); err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		sources, err := tx.QueryContext(ctx, `SELECT a.provider,m.remote_id FROM connector_entity_mappings m JOIN connector_accounts a ON a.organization_id=m.organization_id AND a.workspace_id=m.workspace_id AND a.id=m.connector_account_id WHERE m.organization_id=$1 AND m.workspace_id=$2 AND m.entity_type='order' AND m.local_entity_id=$3 ORDER BY a.provider`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		if err != nil {
			return err
		}
		defer sources.Close()
		out.Sources = []OrderSource{}
		for sources.Next() {
			var source OrderSource
			if err := sources.Scan(&source.Provider, &source.RemoteID); err != nil {
				return err
			}
			out.Sources = append(out.Sources, source)
		}
		return sources.Err()
	})
	out.PlacedAt = out.PlacedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, err
}

// SeedDemoOrders atomically creates five tenant-scoped synthetic orders and is idempotent.
func (r *Repository) SeedDemoOrders(ctx context.Context, scope tenancy.Scope, recipientID string) (int, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || recipientID == "" || len(recipientID) > 128 {
		return 0, search.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("search repository: begin demo seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	var appliedOrg, appliedWS string
	if err := tx.QueryRowContext(ctx, applyScope, org, ws).Scan(&appliedOrg, &appliedWS); err != nil {
		return 0, fmt.Errorf("search repository: scope demo seed: %w", err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND order_number LIKE 'DEMO-%')`, org, ws).Scan(&exists); err != nil {
		return 0, fmt.Errorf("search repository: check demo seed: %w", err)
	}
	if exists {
		var productID, offerID string
		if err := tx.QueryRowContext(ctx, `SELECT p.id,o.id FROM products p JOIN offers o ON o.organization_id=p.organization_id AND o.workspace_id=p.workspace_id AND o.product_id=p.id WHERE p.organization_id=$1 AND p.workspace_id=$2 AND p.code='DEMO-PRODUCT' AND o.sku='DEMO-SKU'`, org, ws).Scan(&productID, &offerID); err != nil {
			return 0, fmt.Errorf("search repository: find demo catalog: %w", err)
		}
		if err := seedDemoInventory(ctx, tx, org, ws, offerID, time.Now().UTC()); err != nil {
			return 0, err
		}
		if err := seedDemoCompliance(ctx, tx, org, ws, productID, time.Now().UTC()); err != nil {
			return 0, err
		}
		if err := seedDemoNotifications(ctx, tx, org, ws, recipientID, time.Now().UTC()); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM demo_dataset_tombstones WHERE organization_id=$1 AND workspace_id=$2`, org, ws); err != nil {
			return 0, fmt.Errorf("search repository: restore demo visibility: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return 0, fmt.Errorf("search repository: commit demo restore: %w", err)
		}
		return 0, nil
	}
	productID, offerID := randomUUIDv7(), randomUUIDv7()
	if productID == "" || offerID == "" {
		return 0, errors.New("search repository: random identifier failed")
	}
	stamp := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO products(id,organization_id,workspace_id,code,title,description,status,created_at,updated_at) VALUES($1,$2,$3,'DEMO-PRODUCT','Демонстрационный товар','Синтетические данные TORGNEXA','draft',$4,$4)`, productID, org, ws, stamp); err != nil {
		return 0, fmt.Errorf("search repository: insert demo product: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE products SET status='active',version=2,updated_at=$4 WHERE id=$1 AND organization_id=$2 AND workspace_id=$3`, productID, org, ws, stamp); err != nil {
		return 0, fmt.Errorf("search repository: activate demo product: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO offers(id,organization_id,workspace_id,product_id,sku,status,created_at,updated_at) VALUES($1,$2,$3,$4,'DEMO-SKU','draft',$5,$5)`, offerID, org, ws, productID, stamp); err != nil {
		return 0, fmt.Errorf("search repository: insert demo offer: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE offers SET status='active',version=2,updated_at=$4 WHERE id=$1 AND organization_id=$2 AND workspace_id=$3`, offerID, org, ws, stamp); err != nil {
		return 0, fmt.Errorf("search repository: activate demo offer: %w", err)
	}
	if err = seedDemoInventory(ctx, tx, org, ws, offerID, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoCompliance(ctx, tx, org, ws, productID, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoNotifications(ctx, tx, org, ws, recipientID, stamp); err != nil {
		return 0, err
	}
	amounts := []int64{129900, 459000, 79900, 219900, 349000}
	for index, amount := range amounts {
		orderID, itemID := randomUUIDv7(), randomUUIDv7()
		if orderID == "" || itemID == "" {
			return 0, errors.New("search repository: random identifier failed")
		}
		placed, number := stamp.Add(-time.Duration(index)*6*time.Hour), fmt.Sprintf("DEMO-%03d", index+1)
		if _, err = tx.ExecContext(ctx, `INSERT INTO orders(id,organization_id,workspace_id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at,created_at,updated_at) VALUES($1,$2,$3,$4,'pending','RUB',$5,0,0,0,$5,$6,$6,$6)`, orderID, org, ws, number, amount, placed); err != nil {
			return 0, fmt.Errorf("search repository: insert demo order: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_items(id,organization_id,workspace_id,order_id,position,offer_id,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax,created_at) VALUES($1,$2,$3,$4,1,$5,'DEMO-SKU',1,0,'PCS',$6,$6,0,0,$6,'RU','zero',0,0,true,$7)`, itemID, org, ws, orderID, offerID, amount, placed); err != nil {
			return 0, fmt.Errorf("search repository: insert demo item: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("search repository: commit demo seed: %w", err)
	}
	return len(amounts), nil
}

func seedDemoInventory(ctx context.Context, tx *sql.Tx, org, ws, offerID string, stamp time.Time) error {
	warehouseID := randomUUIDv7()
	if warehouseID == "" {
		return errors.New("search repository: random demo warehouse identifier failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouses(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,'DEMO-WAREHOUSE','Демонстрационный склад','active',1,$4,$4) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, warehouseID, org, ws, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo warehouse: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND code='DEMO-WAREHOUSE'`, org, ws).Scan(&warehouseID); err != nil {
		return fmt.Errorf("search repository: find demo warehouse: %w", err)
	}
	positionID := randomUUIDv7()
	if positionID == "" {
		return errors.New("search repository: random demo inventory identifier failed")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO inventory_positions(id,organization_id,workspace_id,offer_id,warehouse_id,unit,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'PCS',$6,$6) ON CONFLICT(organization_id,workspace_id,offer_id,warehouse_id) DO NOTHING`, positionID, org, ws, offerID, warehouseID, stamp)
	if err != nil {
		return fmt.Errorf("search repository: insert demo inventory: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("search repository: inspect demo inventory insert: %w", err)
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_positions SET on_hand_coefficient=48,reserved_coefficient=7,version=2,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=1`, org, ws, positionID, stamp); err != nil {
			return fmt.Errorf("search repository: stock demo inventory: %w", err)
		}
	}
	return nil
}

func seedDemoCompliance(ctx context.Context, tx *sql.Tx, org, ws, productID string, stamp time.Time) error {
	documentID := randomUUIDv7()
	if documentID == "" {
		return errors.New("search repository: random demo compliance identifier failed")
	}
	issuedAt, expiresAt := stamp.AddDate(0, -1, 0), stamp.AddDate(1, 0, 0)
	result, err := tx.ExecContext(ctx, `INSERT INTO compliance_documents(id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,verification_source,verified_at,version,created_at,updated_at) VALUES($1,$2,$3,'declaration','DEMO-EAEU-RU-001','RU','Демонстрационный орган сертификации','demo.registry','DEMO-REGISTRY-001','draft',$4,$5,'',NULL,1,$6,$6) ON CONFLICT(organization_id,workspace_id,jurisdiction,document_type,number) WHERE status<>'revoked' DO NOTHING`, documentID, org, ws, issuedAt, expiresAt, stamp)
	if err != nil {
		return fmt.Errorf("search repository: insert demo compliance document: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("search repository: inspect demo compliance insert: %w", err)
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE compliance_documents SET status='valid',verification_source='demo.registry',verified_at=$4,version=2,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=1`, org, ws, documentID, stamp); err != nil {
			return fmt.Errorf("search repository: verify demo compliance document: %w", err)
		}
	} else if err := tx.QueryRowContext(ctx, `SELECT id FROM compliance_documents WHERE organization_id=$1 AND workspace_id=$2 AND jurisdiction='RU' AND document_type='declaration' AND number='DEMO-EAEU-RU-001' AND status<>'revoked'`, org, ws).Scan(&documentID); err != nil {
		return fmt.Errorf("search repository: find demo compliance document: %w", err)
	}
	bindingID := randomUUIDv7()
	if bindingID == "" {
		return errors.New("search repository: random demo compliance binding identifier failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,'product',$5,true,1,$6,$6) ON CONFLICT(organization_id,workspace_id,document_id,subject_type,subject_id) DO NOTHING`, bindingID, org, ws, documentID, productID, stamp); err != nil {
		return fmt.Errorf("search repository: bind demo compliance document: %w", err)
	}
	return nil
}

func seedDemoNotifications(ctx context.Context, tx *sql.Tx, org, ws, recipientID string, stamp time.Time) error {
	items := []struct {
		severity, key, title, body string
		offset                     time.Duration
	}{
		{"info", "demo.dataset.ready", "Демонстрационный контур готов", "Созданы товар, пять заказов, складской остаток и декларация соответствия.", -2 * time.Hour},
		{"warning", "demo.stock.reservation", "Часть остатка зарезервирована", "На демонстрационном складе зарезервировано 7 из 48 единиц товара DEMO-SKU.", -time.Hour},
		{"critical", "demo.compliance.expiry", "Проверьте срок декларации", "Демонстрационное критическое уведомление показывает, как выглядят события, требующие внимания.", 0},
	}
	for _, item := range items {
		id := randomUUIDv7()
		if id == "" {
			return errors.New("search repository: random demo notification identifier failed")
		}
		occurred := stamp.Add(item.offset)
		if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,organization_id,workspace_id,recipient_id,dedupe_key,severity,title,body,occurrence_count,first_occurred_at,last_occurred_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9,$9,$9) ON CONFLICT(organization_id,workspace_id,recipient_id,dedupe_key) DO NOTHING`, id, org, ws, recipientID, item.key, item.severity, item.title, item.body, occurred); err != nil {
			return fmt.Errorf("search repository: insert demo notification: %w", err)
		}
	}
	return nil
}

// DeleteDemoOrders logically removes only synthetic data while retaining the
// immutable canonical order history. It is safe to repeat.
func (r *Repository) DeleteDemoOrders(ctx context.Context, scope tenancy.Scope) (int, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() {
		return 0, search.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("search repository: begin demo delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	var appliedOrg, appliedWS string
	if err := tx.QueryRowContext(ctx, applyScope, org, ws).Scan(&appliedOrg, &appliedWS); err != nil {
		return 0, fmt.Errorf("search repository: scope demo delete: %w", err)
	}
	var deleted int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND order_number IN ('DEMO-001','DEMO-002','DEMO-003','DEMO-004','DEMO-005') AND NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)`, org, ws).Scan(&deleted); err != nil {
		return 0, fmt.Errorf("search repository: count demo orders: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO demo_dataset_tombstones(organization_id,workspace_id) VALUES($1,$2) ON CONFLICT(organization_id,workspace_id) DO UPDATE SET deleted_at=clock_timestamp()`, org, ws); err != nil {
		return 0, fmt.Errorf("search repository: hide demo dataset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("search repository: commit demo delete: %w", err)
	}
	return deleted, nil
}

func randomUUIDv7() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	millis := uint64(time.Now().UnixMilli())
	value[0], value[1], value[2], value[3], value[4], value[5] = byte(millis>>40), byte(millis>>32), byte(millis>>24), byte(millis>>16), byte(millis>>8), byte(millis)
	value[6], value[8] = (value[6]&0x0f)|0x70, (value[8]&0x3f)|0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("search repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) SearchProducts(ctx context.Context, scope tenancy.Scope, query search.ProductQuery) (search.ProductPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || query.Validate() != nil {
		return search.ProductPage{}, search.ErrInvalid
	}
	fingerprint := search.ProductFingerprint(query)
	cursor, err := search.ParseCursor(query.Cursor, fingerprint)
	if err != nil {
		return search.ProductPage{}, search.ErrInvalid
	}
	var page search.ProductPage
	err = r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		cursorAt := cursor.UpdatedAt
		if cursorAt.IsZero() {
			cursorAt = time.Unix(0, 0).UTC()
		}
		rows, err := tx.QueryContext(ctx, productSearchSQL,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), query.Text, query.Status,
			cursor.ID, cursor.Priority, cursorAt, query.Limit+1, likePrefix(query.Text),
		)
		if err != nil {
			return fmt.Errorf("search repository: product query failed: %w", err)
		}
		defer rows.Close()

		type rankedHit struct {
			hit      search.ProductHit
			priority int
		}
		hits := make([]rankedHit, 0, query.Limit+1)
		for rows.Next() {
			var item rankedHit
			if err := rows.Scan(&item.hit.ID, &item.hit.Code, &item.hit.Title, &item.hit.Status, &item.hit.UpdatedAt, &item.priority); err != nil {
				return fmt.Errorf("search repository: product row failed: %w", err)
			}
			item.hit.UpdatedAt = item.hit.UpdatedAt.UTC()
			if item.priority < 0 || item.priority > 2 || item.hit.Validate() != nil {
				return search.ErrInvalid
			}
			hits = append(hits, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("search repository: product rows failed: %w", err)
		}
		if len(hits) > query.Limit {
			last := hits[query.Limit-1]
			next, err := search.NewCursor(last.priority, last.hit.UpdatedAt, last.hit.ID, fingerprint)
			if err != nil {
				return err
			}
			page.NextCursor = next
			hits = hits[:query.Limit]
		}
		page.Items = make([]search.ProductHit, len(hits))
		for i := range hits {
			page.Items[i] = hits[i].hit
		}
		return page.Validate()
	})
	return page, err
}

func (r *Repository) SearchOrders(ctx context.Context, scope tenancy.Scope, query search.OrderQuery) (search.OrderPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || query.Validate() != nil {
		return search.OrderPage{}, search.ErrInvalid
	}
	fingerprint := search.OrderFingerprint(query)
	cursor, err := search.ParseCursor(query.Cursor, fingerprint)
	if err != nil {
		return search.OrderPage{}, search.ErrInvalid
	}
	var page search.OrderPage
	err = r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		cursorAt := cursor.UpdatedAt
		if cursorAt.IsZero() {
			cursorAt = time.Unix(0, 0).UTC()
		}
		var placedFrom, placedTo any
		if query.PlacedFrom != nil {
			placedFrom = *query.PlacedFrom
		}
		if query.PlacedTo != nil {
			placedTo = *query.PlacedTo
		}
		rows, err := tx.QueryContext(ctx, orderSearchSQL,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), query.Text, query.Status,
			cursor.ID, cursor.Priority, cursorAt, placedFrom, placedTo, query.Limit+1, likePrefix(query.Text),
		)
		if err != nil {
			return fmt.Errorf("search repository: order query failed: %w", err)
		}
		defer rows.Close()

		type rankedHit struct {
			hit      search.OrderHit
			priority int
		}
		hits := make([]rankedHit, 0, query.Limit+1)
		for rows.Next() {
			var item rankedHit
			if err := rows.Scan(&item.hit.ID, &item.hit.OrderNumber, &item.hit.Status, &item.hit.Currency, &item.hit.GrandMinorUnits, &item.hit.PlacedAt, &item.hit.UpdatedAt, &item.priority); err != nil {
				return fmt.Errorf("search repository: order row failed: %w", err)
			}
			item.hit.PlacedAt = item.hit.PlacedAt.UTC()
			item.hit.UpdatedAt = item.hit.UpdatedAt.UTC()
			if item.priority < 0 || item.priority > 2 || item.hit.Validate() != nil {
				return search.ErrInvalid
			}
			hits = append(hits, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("search repository: order rows failed: %w", err)
		}
		if len(hits) > query.Limit {
			last := hits[query.Limit-1]
			next, err := search.NewCursor(last.priority, last.hit.UpdatedAt, last.hit.ID, fingerprint)
			if err != nil {
				return err
			}
			page.NextCursor = next
			hits = hits[:query.Limit]
		}
		page.Items = make([]search.OrderHit, len(hits))
		for i := range hits {
			page.Items[i] = hits[i].hit
		}
		return page.Validate()
	})
	return page, err
}

func likePrefix(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value) + "%"
}

func (r *Repository) withReadTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() || fn == nil {
		return search.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("search repository: begin read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return fmt.Errorf("search repository: apply tenant scope: %w", err)
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return search.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search repository: commit read transaction: %w", err)
	}
	return nil
}
