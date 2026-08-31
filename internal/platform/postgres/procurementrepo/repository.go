// Package procurementrepo implements the tenant-scoped procurement workbench.
// It extends the Task 052 tables and deliberately does not introduce a second
// purchase-order state machine.
package procurementrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/procurement"
)

var (
	ErrNotFound = errors.New("procurement repository: not found")
	ErrConflict = errors.New("procurement repository: conflict")
	ErrNotReady = errors.New("procurement repository: preview is not ready")
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository persists supplier, offer, price-list and purchase-order workbench
// state. It never accepts an unscoped query.
type Repository struct{ db *sql.DB }

// New constructs a tenant-scoped procurement repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("procurement repository: database is required")
	}
	return &Repository{db: db}, nil
}

// ListSuppliers returns a bounded supplier directory.
func (r *Repository) ListSuppliers(ctx context.Context, scope tenancy.Scope, limit int) ([]procurement.SupplierRecord, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		return nil, procurement.ErrInvalid
	}
	items := make([]procurement.SupplierRecord, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,legal_party_id,name,status,payment_terms,default_currency,lead_time_days,minimum_order_minor,minimum_order_currency,contacts,contracts,version,created_at,updated_at FROM procurement_suppliers WHERE organization_id=$1 AND workspace_id=$2 ORDER BY name,id LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanSupplier(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// Supplier returns one supplier in the current workspace.
func (r *Repository) Supplier(ctx context.Context, scope tenancy.Scope, id string) (procurement.SupplierRecord, error) {
	if err := r.validate(ctx, scope); err != nil || !safeID(id) {
		return procurement.SupplierRecord{}, procurement.ErrInvalid
	}
	var item procurement.SupplierRecord
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		item, err = scanSupplier(tx.QueryRowContext(ctx, `SELECT id,legal_party_id,name,status,payment_terms,default_currency,lead_time_days,minimum_order_minor,minimum_order_currency,contacts,contracts,version,created_at,updated_at FROM procurement_suppliers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id))
		return err
	})
	return item, err
}

// CreateSupplier creates a supplier relationship referencing a canonical
// supplier counterparty. The caller must create that LegalParty first.
func (r *Repository) CreateSupplier(ctx context.Context, scope tenancy.Scope, item procurement.SupplierRecord, m procurement.Mutation) (procurement.SupplierRecord, error) {
	if item.Status == "" {
		item.Status = procurement.SupplierActive
	}
	if item.Currency == "" {
		item.Currency = "RUB"
	}
	if item.MinimumOrderCurrency == "" {
		item.MinimumOrderCurrency = item.Currency
	}
	if err := r.validateMutation(ctx, scope, m); err != nil || !supplierInputValid(item, scope) {
		return procurement.SupplierRecord{}, procurement.ErrInvalid
	}
	item.Version = 1
	item.CreatedAt, item.UpdatedAt = m.OccurredAt, m.OccurredAt
	var result procurement.SupplierRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var canonicalRole bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM counterparties WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND role='supplier' AND status <> 'archived')`, scope.OrganizationID(), scope.WorkspaceID(), item.LegalPartyID).Scan(&canonicalRole); err != nil {
			return err
		}
		if !canonicalRole {
			return ErrNotFound
		}
		contacts, _ := json.Marshal(item.Contacts)
		contracts, _ := json.Marshal(item.Contracts)
		row := tx.QueryRowContext(ctx, `INSERT INTO procurement_suppliers(id,organization_id,workspace_id,legal_party_id,name,active,version,status,payment_terms,default_currency,lead_time_days,minimum_order_minor,minimum_order_currency,contacts,contracts,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb,$15,$15) RETURNING id,legal_party_id,name,status,payment_terms,default_currency,lead_time_days,minimum_order_minor,minimum_order_currency,contacts,contracts,version,created_at,updated_at`, item.ID, scope.OrganizationID(), scope.WorkspaceID(), item.LegalPartyID, item.Name, item.Status == procurement.SupplierActive, string(item.Status), item.PaymentTerms, item.Currency, item.LeadTimeDays, item.MinimumOrderMinor, item.MinimumOrderCurrency, contacts, contracts, m.OccurredAt)
		var err error
		result, err = scanSupplier(row)
		if err != nil {
			return mapError(err)
		}
		if err := appendAudit(ctx, tx, scope, m, "procurement.supplier.changed", "supplier", result.ID, audit.RiskWriteSensitive, audit.Summary{"change": "created", "legal_party_id": result.LegalPartyID, "status": result.Status, "version": result.Version}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, m, "procurement.supplier.changed.v1", "supplier", result.ID, map[string]any{"supplier_id": result.ID, "legal_party_id": result.LegalPartyID, "status": result.Status, "version": result.Version, "change": "created"})
	})
	return result, err
}

// UpdateSupplier changes a supplier profile under optimistic concurrency.
func (r *Repository) UpdateSupplier(ctx context.Context, scope tenancy.Scope, item procurement.SupplierRecord, expected int64, m procurement.Mutation) (procurement.SupplierRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !supplierInputValid(item, scope) || expected < 1 || !safeID(item.ID) {
		return procurement.SupplierRecord{}, procurement.ErrInvalid
	}
	contacts, _ := json.Marshal(item.Contacts)
	contracts, _ := json.Marshal(item.Contracts)
	var result procurement.SupplierRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE procurement_suppliers SET name=$4,active=$5,status=$6,payment_terms=$7,default_currency=$8,lead_time_days=$9,minimum_order_minor=$10,minimum_order_currency=$11,contacts=$12::jsonb,contracts=$13::jsonb,version=version+1,updated_at=$14 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$15 RETURNING id,legal_party_id,name,status,payment_terms,default_currency,lead_time_days,minimum_order_minor,minimum_order_currency,contacts,contracts,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), item.ID, item.Name, item.Status == procurement.SupplierActive, item.Status, item.PaymentTerms, item.Currency, item.LeadTimeDays, item.MinimumOrderMinor, item.MinimumOrderCurrency, contacts, contracts, m.OccurredAt, expected)
		var err error
		result, err = scanSupplier(row)
		if err != nil {
			return mapError(err)
		}
		if err := appendAudit(ctx, tx, scope, m, "procurement.supplier.changed", "supplier", result.ID, audit.RiskWriteSensitive, audit.Summary{"change": "updated", "version": result.Version}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, m, "procurement.supplier.changed.v1", "supplier", result.ID, map[string]any{"supplier_id": result.ID, "status": result.Status, "version": result.Version, "change": "updated"})
	})
	return result, err
}

// ListOffers returns current supplier offers.
func (r *Repository) ListOffers(ctx context.Context, scope tenancy.Scope, supplierID string, limit int) ([]procurement.SupplierOfferRecord, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 500 || (supplierID != "" && !safeID(supplierID)) {
		return nil, procurement.ErrInvalid
	}
	items := make([]procurement.SupplierOfferRecord, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		query := `SELECT id,supplier_id,canonical_offer_id,supplier_sku,gtin,sku,unit,unit_price_minor,currency,min_quantity,case_pack,lead_time_days,priority,minimum_order_minor,minimum_order_currency,valid_from,valid_until,version FROM supplier_offers WHERE organization_id=$1 AND workspace_id=$2 AND ($3='' OR supplier_id=$3) ORDER BY supplier_id,sku,id LIMIT $4`
		rows, err := tx.QueryContext(ctx, query, scope.OrganizationID(), scope.WorkspaceID(), supplierID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanOffer(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// CreateOffer creates a supplier quote and its first immutable price history.
func (r *Repository) CreateOffer(ctx context.Context, scope tenancy.Scope, item procurement.SupplierOfferRecord, m procurement.Mutation) (procurement.SupplierOfferRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !offerInputValid(item) {
		return procurement.SupplierOfferRecord{}, procurement.ErrInvalid
	}
	var result procurement.SupplierOfferRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var supplierOK bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_suppliers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='active')`, scope.OrganizationID(), scope.WorkspaceID(), item.SupplierID).Scan(&supplierOK); err != nil {
			return err
		}
		if !supplierOK {
			return ErrNotFound
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO supplier_offers(id,organization_id,workspace_id,supplier_id,sku,unit_price_minor,currency,min_quantity,lead_time_days,valid_until,canonical_offer_id,supplier_sku,gtin,unit,case_pack,priority,minimum_order_minor,minimum_order_currency,valid_from,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20) RETURNING id,supplier_id,canonical_offer_id,supplier_sku,gtin,sku,unit,unit_price_minor,currency,min_quantity,case_pack,lead_time_days,priority,minimum_order_minor,minimum_order_currency,valid_from,valid_until,version`, item.ID, scope.OrganizationID(), scope.WorkspaceID(), item.SupplierID, item.SKU, item.UnitPriceMinor, item.Currency, item.MOQ, item.LeadTimeDays, item.ValidUntil, item.CanonicalOfferID, item.SupplierSKU, item.GTIN, item.Unit, item.CasePack, item.Priority, item.MinimumOrderMinor, item.MinimumOrderCurrency, item.ValidFrom, m.OccurredAt)
		var err error
		result, err = scanOffer(row)
		if err != nil {
			return mapError(err)
		}
		if err := insertOfferHistory(ctx, tx, scope, result, m, result.Version); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, m, "procurement.supplier_offer.changed", "supplier_offer", result.ID, audit.RiskWriteSensitive, audit.Summary{"change": "created", "supplier_id": result.SupplierID, "currency": result.Currency, "version": result.Version}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, m, "procurement.supplier_offer.changed.v1", "supplier_offer", result.ID, map[string]any{"supplier_offer_id": result.ID, "supplier_id": result.SupplierID, "version": result.Version, "change": "created"})
	})
	return result, err
}

// CreatePriceListPreview persists normalized rows and matching evidence. It
// does not change SupplierOffer records.
func (r *Repository) CreatePriceListPreview(ctx context.Context, scope tenancy.Scope, preview procurement.PriceListPreview, m procurement.Mutation) (procurement.PriceListPreview, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !safeID(preview.ID) || !safeID(preview.SupplierID) || !safeUploadID(preview.UploadID) || preview.SourceSHA256 == "" || preview.MappingFingerprint == "" {
		return procurement.PriceListPreview{}, procurement.ErrInvalid
	}
	var result procurement.PriceListPreview
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var supplierOK bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_suppliers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3)`, scope.OrganizationID(), scope.WorkspaceID(), preview.SupplierID).Scan(&supplierOK); err != nil {
			return err
		}
		if !supplierOK {
			return ErrNotFound
		}
		for i := range preview.Rows {
			row := &preview.Rows[i]
			if hasImportError(preview.Errors, row.Row) {
				continue
			}
			matched, method, err := matchOffer(ctx, tx, scope, preview.SupplierID, *row)
			if err != nil {
				return err
			}
			row.CanonicalOfferID, row.MatchMethod = matched, method
			if matched == "" {
				preview.UnresolvedRows++
				preview.Errors = appendBoundedError(preview.Errors, procurement.ImportError{Row: row.Row, Code: "unresolved_or_ambiguous_match", Detail: "operator mapping required"})
			}
		}
		preview.TotalRows = len(preview.Rows)
		preview.InvalidRows, preview.UnresolvedRows = 0, 0
		for _, row := range preview.Rows {
			switch {
			case hasImportError(preview.Errors, row.Row):
				preview.InvalidRows++
			case row.CanonicalOfferID == "":
				preview.UnresolvedRows++
			}
		}
		preview.ValidRows = preview.TotalRows - preview.InvalidRows - preview.UnresolvedRows
		preview.Status = "preview"
		if preview.TotalRows > 0 && preview.InvalidRows == 0 && preview.UnresolvedRows == 0 {
			preview.Status = "ready"
		}
		preview.Version = 1
		preview.CreatedAt, preview.UpdatedAt = m.OccurredAt, m.OccurredAt
		rowsJSON, _ := json.Marshal(preview.Rows)
		errorsJSON, _ := json.Marshal(preview.Errors)
		if _, err := tx.ExecContext(ctx, `INSERT INTO procurement_price_list_previews(organization_id,workspace_id,preview_id,supplier_id,upload_id,source_sha256,mapping_fingerprint,status,total_rows,valid_rows,invalid_rows,unresolved_rows,errors,rows,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb,1,$15,$15)`, scope.OrganizationID(), scope.WorkspaceID(), preview.ID, preview.SupplierID, preview.UploadID, preview.SourceSHA256, preview.MappingFingerprint, preview.Status, preview.TotalRows, preview.ValidRows, preview.InvalidRows, preview.UnresolvedRows, errorsJSON, rowsJSON, m.OccurredAt); err != nil {
			return mapError(err)
		}
		result = preview
		return appendAudit(ctx, tx, scope, m, "procurement.price_list.imported", "price_list_preview", result.ID, audit.RiskWriteSensitive, audit.Summary{"supplier_id": result.SupplierID, "total_rows": result.TotalRows, "valid_rows": result.ValidRows, "unresolved_rows": result.UnresolvedRows, "status": result.Status})
	})
	return result, err
}

// CommitPriceList applies only matched, valid rows from a persisted preview in
// one transaction. Unresolved or ambiguous rows are retained as evidence.
func (r *Repository) CommitPriceList(ctx context.Context, scope tenancy.Scope, previewID string, allowPartial bool, m procurement.Mutation) (procurement.PriceListPreview, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !safeID(previewID) {
		return procurement.PriceListPreview{}, procurement.ErrInvalid
	}
	var result procurement.PriceListPreview
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var rowsJSON, errorsJSON []byte
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT preview_id,supplier_id,upload_id,source_sha256,mapping_fingerprint,status,total_rows,valid_rows,invalid_rows,unresolved_rows,errors,rows,version,created_at,updated_at FROM procurement_price_list_previews WHERE organization_id=$1 AND workspace_id=$2 AND preview_id=$3 FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), previewID).Scan(&result.ID, &result.SupplierID, &result.UploadID, &result.SourceSHA256, &result.MappingFingerprint, &status, &result.TotalRows, &result.ValidRows, &result.InvalidRows, &result.UnresolvedRows, &errorsJSON, &rowsJSON, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		result.Status = status
		if status == "committed" {
			return nil
		}
		if status != "ready" && !allowPartial {
			return ErrNotReady
		}
		if json.Unmarshal(rowsJSON, &result.Rows) != nil || json.Unmarshal(errorsJSON, &result.Errors) != nil {
			return procurement.ErrInvalid
		}
		applied := 0
		for _, row := range result.Rows {
			if row.CanonicalOfferID == "" || hasImportError(result.Errors, row.Row) || row.UnitPriceMinor < 0 || row.Currency == "" {
				continue
			}
			current, err := scanOffer(tx.QueryRowContext(ctx, `SELECT id,supplier_id,canonical_offer_id,supplier_sku,gtin,sku,unit,unit_price_minor,currency,min_quantity,case_pack,lead_time_days,priority,minimum_order_minor,minimum_order_currency,valid_from,valid_until,version FROM supplier_offers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND supplier_id=$4 FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), row.CanonicalOfferID, result.SupplierID))
			if err != nil {
				continue
			}
			if err := insertOfferHistory(ctx, tx, scope, current, m, current.Version); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE supplier_offers SET supplier_sku=COALESCE(NULLIF($4,''),supplier_sku),gtin=COALESCE(NULLIF($5,''),gtin),sku=COALESCE(NULLIF($6,''),sku),unit=$7,unit_price_minor=$8,currency=$9,min_quantity=$10,case_pack=$11,lead_time_days=$12,priority=$13,minimum_order_minor=$14,minimum_order_currency=$15,valid_until=$16,updated_at=$17,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), current.ID, row.SupplierSKU, row.GTIN, row.SKU, row.Unit, row.UnitPriceMinor, row.Currency, row.MOQ, row.CasePack, row.LeadTimeDays, row.MinimumOrderMinor, row.MinimumOrderCurrency, m.OccurredAt.Add(30*24*time.Hour), m.OccurredAt); err != nil {
				return err
			}
			applied++
		}
		if applied == 0 {
			return ErrNotReady
		}
		if _, err := tx.ExecContext(ctx, `UPDATE procurement_price_list_previews SET status='committed',updated_at=$4,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND preview_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), previewID, m.OccurredAt); err != nil {
			return err
		}
		result.Status, result.UpdatedAt, result.Version = "committed", m.OccurredAt, result.Version+1
		if err := appendAudit(ctx, tx, scope, m, "procurement.price_list.imported", "price_list_preview", previewID, audit.RiskWriteSensitive, audit.Summary{"supplier_id": result.SupplierID, "status": "committed", "applied_rows": applied, "partial": allowPartial}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, m, "procurement.price_list.imported.v1", "price_list_preview", previewID, map[string]any{"preview_id": previewID, "supplier_id": result.SupplierID, "applied_rows": applied, "partial": allowPartial})
	})
	return result, err
}

// ListPurchaseOrders returns PO headers and their lines.
func (r *Repository) ListPurchaseOrders(ctx context.Context, scope tenancy.Scope, status, supplierID string, limit int) ([]procurement.PurchaseOrderRecord, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 || (supplierID != "" && !safeID(supplierID)) || (status != "" && !procurement.POStatus(status).Valid()) {
		return nil, procurement.ErrInvalid
	}
	items := make([]procurement.PurchaseOrderRecord, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT p.id,p.supplier_id,p.status,p.currency,p.version,p.created_at,p.updated_at,p.warehouse_id,p.recommendation_id,p.recommendation_digest,p.idempotency_key,p.approval_request_id,p.send_state,p.error_code,p.expected_receipt_at,p.creator_id FROM purchase_orders p WHERE p.organization_id=$1 AND p.workspace_id=$2 AND ($3='' OR p.status=$3) AND ($4='' OR p.supplier_id=$4) ORDER BY p.updated_at DESC,p.id DESC LIMIT $5`, scope.OrganizationID(), scope.WorkspaceID(), status, supplierID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanPO(rows, scope)
			if err != nil {
				return err
			}
			if err := loadLines(ctx, tx, scope, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// PurchaseOrder returns one order with its current lines.
func (r *Repository) PurchaseOrder(ctx context.Context, scope tenancy.Scope, id string) (procurement.PurchaseOrderRecord, error) {
	if err := r.validate(ctx, scope); err != nil || !safeID(id) {
		return procurement.PurchaseOrderRecord{}, procurement.ErrInvalid
	}
	var item procurement.PurchaseOrderRecord
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error { return loadPO(ctx, tx, scope, id, &item) })
	return item, err
}

// CreatePurchaseOrder creates a draft PO, optionally linked to a recommendation
// snapshot. Idempotency returns the original PO for an identical key.
func (r *Repository) CreatePurchaseOrder(ctx context.Context, scope tenancy.Scope, item procurement.PurchaseOrderRecord, m procurement.Mutation) (procurement.PurchaseOrderRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || item.SupplierID == "" || item.Currency.Validate() != nil || len(item.Lines) == 0 || !safeID(item.ID) || item.Version != 1 || item.Status != procurement.PODraft || !safeID(item.WarehouseID) || !safeID(item.IdempotencyKey) {
		return procurement.PurchaseOrderRecord{}, procurement.ErrInvalid
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = m.OccurredAt
	}
	item.UpdatedAt = m.OccurredAt
	var result procurement.PurchaseOrderRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var supplierOK bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_suppliers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='active')`, scope.OrganizationID(), scope.WorkspaceID(), item.SupplierID).Scan(&supplierOK); err != nil {
			return err
		}
		if !supplierOK {
			return ErrNotFound
		}
		if item.RecommendationID != "" {
			var snapshotDigest string
			err := tx.QueryRowContext(ctx, `SELECT s.digest FROM replenishment_recommendations r JOIN replenishment_snapshots s ON s.organization_id=r.organization_id AND s.workspace_id=r.workspace_id AND s.id=r.snapshot_id WHERE r.organization_id=$1 AND r.workspace_id=$2 AND r.id=$3`, scope.OrganizationID(), scope.WorkspaceID(), item.RecommendationID).Scan(&snapshotDigest)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if snapshotDigest != item.RecommendationDigest {
				return ErrConflict
			}
		}
		if item.IdempotencyKey != "" {
			var existingID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM purchase_orders WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID(), scope.WorkspaceID(), item.IdempotencyKey).Scan(&existingID); err == nil {
				if err := loadPO(ctx, tx, scope, existingID, &result); err != nil {
					return err
				}
				if result.ID != item.ID || result.SupplierID != item.SupplierID {
					return ErrConflict
				}
				return nil
			}
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO purchase_orders(id,organization_id,workspace_id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id) VALUES($1,$2,$3,$4,'draft',$5,1,$6,$6,$7,$8,$9,$10,$11,'not_sent','',$12,$13) RETURNING id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id`, item.ID, scope.OrganizationID(), scope.WorkspaceID(), item.SupplierID, item.Currency, item.CreatedAt, item.WarehouseID, item.RecommendationID, item.RecommendationDigest, item.IdempotencyKey, item.ApprovalRequestID, item.ExpectedReceiptAt, item.CreatorID)
		var err error
		result, err = scanPO(row, scope)
		if err != nil {
			return mapError(err)
		}
		for _, line := range item.Lines {
			if line.ID == "" || line.OfferID == "" || line.SKU == "" || line.Quantity.Validate() != nil || line.UnitPrice.Validate() != nil || line.UnitPrice.Currency() != item.Currency {
				return procurement.ErrInvalid
			}
			var offerOK bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier_offers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND supplier_id=$4)`, scope.OrganizationID(), scope.WorkspaceID(), line.OfferID, item.SupplierID).Scan(&offerOK); err != nil {
				return err
			}
			if !offerOK {
				return ErrNotFound
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_order_lines(organization_id,workspace_id,purchase_order_id,line_id,offer_id,sku,quantity,unit_price_minor,unit,supplier_sku,received_quantity) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'0')`, scope.OrganizationID(), scope.WorkspaceID(), item.ID, line.ID, line.OfferID, line.SKU, line.Quantity.Value.String(), line.UnitPrice.MinorUnits(), line.Quantity.Unit, ""); err != nil {
				return mapError(err)
			}
		}
		result.Lines = item.Lines
		if err := appendPOEvent(ctx, tx, scope, m, result, procurement.PODraft, "created"); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, m, "procurement.purchase_order.changed", "purchase_order", result.ID, audit.RiskWriteSensitive, audit.Summary{"change": "created", "supplier_id": result.SupplierID, "status": result.Status, "line_count": len(result.Lines), "version": result.Version}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, m, "procurement.purchase_order.changed.v1", "purchase_order", result.ID, map[string]any{"purchase_order_id": result.ID, "supplier_id": result.SupplierID, "status": result.Status, "version": result.Version, "change": "created"})
	})
	return result, err
}

// ChangePurchaseOrderStatus applies the existing Task 052 lifecycle with an
// optimistic version and an append-only event.
func (r *Repository) ChangePurchaseOrderStatus(ctx context.Context, scope tenancy.Scope, id string, next procurement.POStatus, expected int64, action, approvalRequestID string, m procurement.Mutation) (procurement.PurchaseOrderRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !safeID(id) || !next.Valid() || expected < 1 || !safeID(action) || (approvalRequestID != "" && !safeID(approvalRequestID)) {
		return procurement.PurchaseOrderRecord{}, procurement.ErrInvalid
	}
	var result procurement.PurchaseOrderRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if existing, found, err := idempotentPO(ctx, tx, scope, id, m.CorrelationID); err != nil {
			return err
		} else if found {
			result = existing
			return nil
		}
		var current procurement.PurchaseOrderRecord
		if err := loadPO(ctx, tx, scope, id, &current); err != nil {
			return err
		}
		if current.Version != expected || !procurement.CanTransition(current.Status, next) {
			return ErrConflict
		}
		row := tx.QueryRowContext(ctx, `UPDATE purchase_orders SET status=$4,send_state=$5,approval_request_id=COALESCE(NULLIF($6,''),approval_request_id),updated_at=$7,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$8 RETURNING id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id`, scope.OrganizationID(), scope.WorkspaceID(), id, next, mapSendState(next), approvalRequestID, m.OccurredAt, expected)
		var err error
		result, err = scanPO(row, scope)
		if err != nil {
			return mapError(err)
		}
		result.Lines = current.Lines
		if err := appendPOEvent(ctx, tx, scope, m, result, current.Status, action); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, m, "procurement.purchase_order.changed", "purchase_order", result.ID, audit.RiskWriteSensitive, audit.Summary{"change": action, "from": current.Status, "to": result.Status, "version": result.Version}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, m, "procurement.purchase_order.changed.v1", "purchase_order", result.ID, map[string]any{"purchase_order_id": result.ID, "from": current.Status, "status": result.Status, "version": result.Version, "change": action})
	})
	return result, err
}

// MarkSendUnknown records an ambiguous remote/export outcome without changing
// the PO lifecycle. A later retry or operator reconciliation can resolve it.
func (r *Repository) MarkSendUnknown(ctx context.Context, scope tenancy.Scope, id string, expected int64, m procurement.Mutation) (procurement.PurchaseOrderRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !safeID(id) || expected < 1 {
		return procurement.PurchaseOrderRecord{}, procurement.ErrInvalid
	}
	var result procurement.PurchaseOrderRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if existing, found, err := idempotentPO(ctx, tx, scope, id, m.CorrelationID); err != nil {
			return err
		} else if found {
			result = existing
			return nil
		}
		row := tx.QueryRowContext(ctx, `UPDATE purchase_orders SET send_state='unknown',error_code='send_timeout',updated_at=$4,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 AND status='sent' RETURNING id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id`, scope.OrganizationID(), scope.WorkspaceID(), id, m.OccurredAt, expected)
		var err error
		result, err = scanPO(row, scope)
		if err != nil {
			return mapError(err)
		}
		return enqueue(ctx, tx, scope, m, "procurement.purchase_order.changed.v1", "purchase_order", id, map[string]any{"purchase_order_id": id, "status": result.Status, "send_state": "unknown", "error_code": "send_timeout", "version": result.Version})
	})
	return result, err
}

// RetrySend resolves a previously unknown export outcome to a new queued send
// attempt. It does not alter the PurchaseOrder lifecycle.
func (r *Repository) RetrySend(ctx context.Context, scope tenancy.Scope, id string, expected int64, m procurement.Mutation) (procurement.PurchaseOrderRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !safeID(id) || expected < 1 {
		return procurement.PurchaseOrderRecord{}, procurement.ErrInvalid
	}
	var result procurement.PurchaseOrderRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if existing, found, err := idempotentPO(ctx, tx, scope, id, m.CorrelationID); err != nil {
			return err
		} else if found {
			result = existing
			return nil
		}
		row := tx.QueryRowContext(ctx, `UPDATE purchase_orders SET send_state='sent',error_code='',updated_at=$4,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 AND status='sent' AND send_state='unknown' RETURNING id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id`, scope.OrganizationID(), scope.WorkspaceID(), id, m.OccurredAt, expected)
		var err error
		result, err = scanPO(row, scope)
		if err != nil {
			return mapError(err)
		}
		return enqueue(ctx, tx, scope, m, "procurement.purchase_order.changed.v1", "purchase_order", id, map[string]any{"purchase_order_id": id, "status": result.Status, "send_state": "sent", "change": "send_retry", "version": result.Version})
	})
	return result, err
}

// Receive appends an inbound receipt and advances the existing PO status to
// partially_received or received. Inventory changes remain WMS ledger work.
func (r *Repository) Receive(ctx context.Context, scope tenancy.Scope, receipt procurement.ReceivingRecord, m procurement.Mutation) (procurement.PurchaseOrderRecord, error) {
	if err := r.validateMutation(ctx, scope, m); err != nil || !safeID(receipt.ID) || !safeID(receipt.PurchaseOrderID) || !safeID(receipt.LineID) || (receipt.IdempotencyKey != "" && !safeID(receipt.IdempotencyKey)) || receipt.Quantity.Validate() != nil || receipt.Quantity.Value.Coefficient() < 0 {
		return procurement.PurchaseOrderRecord{}, procurement.ErrInvalid
	}
	var result procurement.PurchaseOrderRecord
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if receipt.IdempotencyKey != "" {
			var existingID string
			err := tx.QueryRowContext(ctx, `SELECT receipt_id FROM procurement_receipts WHERE organization_id=$1 AND workspace_id=$2 AND purchase_order_id=$3 AND idempotency_key=$4`, scope.OrganizationID(), scope.WorkspaceID(), receipt.PurchaseOrderID, receipt.IdempotencyKey).Scan(&existingID)
			if err == nil {
				return loadPO(ctx, tx, scope, receipt.PurchaseOrderID, &result)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if err := loadPO(ctx, tx, scope, receipt.PurchaseOrderID, &result); err != nil {
			return err
		}
		if result.Status != procurement.POSent && result.Status != procurement.POPartiallyReceived {
			return procurement.ErrInvalidState
		}
		var line procurement.Line
		found := false
		for _, candidate := range result.Lines {
			if candidate.ID == receipt.LineID {
				line, found = candidate, true
				break
			}
		}
		if !found || line.Quantity.Unit != receipt.Quantity.Unit || receipt.WarehouseID == "" {
			return procurement.ErrInvalid
		}
		oldStatus := result.Status
		lines := result.Lines
		var previous string
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(quantity::numeric),0)::text FROM procurement_receipts WHERE organization_id=$1 AND workspace_id=$2 AND purchase_order_id=$3 AND line_id=$4`, scope.OrganizationID(), scope.WorkspaceID(), receipt.PurchaseOrderID, receipt.LineID).Scan(&previous); err != nil {
			return err
		}
		if decimalCompare(previous, line.Quantity.Value.String()) > 0 || decimalCompare(addDecimalText(previous, receipt.Quantity.Value.String()), line.Quantity.Value.String()) > 0 {
			return procurement.ErrInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO procurement_receipts(organization_id,workspace_id,receipt_id,purchase_order_id,warehouse_id,line_id,quantity,unit,status,discrepancy_code,note,idempotency_key,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, scope.OrganizationID(), scope.WorkspaceID(), receipt.ID, receipt.PurchaseOrderID, receipt.WarehouseID, receipt.LineID, receipt.Quantity.Value.String(), receipt.Quantity.Unit, receipt.Status, receipt.DiscrepancyCode, receipt.Note, receipt.IdempotencyKey, m.OccurredAt); err != nil {
			return mapError(err)
		}
		totalReceived := decimalCompare(addDecimalText(previous, receipt.Quantity.Value.String()), line.Quantity.Value.String())
		next := procurement.POPartiallyReceived
		if totalReceived == 0 {
			next = procurement.POReceived
		}
		if !procurement.CanTransition(result.Status, next) {
			return ErrConflict
		}
		row := tx.QueryRowContext(ctx, `UPDATE purchase_orders SET status=$4,updated_at=$5,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id`, scope.OrganizationID(), scope.WorkspaceID(), receipt.PurchaseOrderID, next, m.OccurredAt, result.Version)
		scanned, err := scanPO(row, scope)
		if err != nil {
			return mapError(err)
		}
		result = scanned
		result.Lines = lines
		if err := appendPOEvent(ctx, tx, scope, m, result, oldStatus, "receiving"); err != nil {
			return err
		}
		if receipt.DiscrepancyCode != "" {
			discrepancyMutation := m
			discrepancyMutation.EventID = m.EventID + "/discrepancy"
			if err := enqueue(ctx, tx, scope, discrepancyMutation, "procurement.receiving.discrepancy.v1", "purchase_order", receipt.PurchaseOrderID, map[string]any{"purchase_order_id": receipt.PurchaseOrderID, "receipt_id": receipt.ID, "line_id": receipt.LineID, "discrepancy_code": receipt.DiscrepancyCode}); err != nil {
				return err
			}
		}
		return enqueue(ctx, tx, scope, m, "procurement.purchase_order.changed.v1", "purchase_order", result.ID, map[string]any{"purchase_order_id": result.ID, "status": result.Status, "version": result.Version, "change": "receiving", "wms_ledger_required": true})
	})
	return result, err
}

// ListFindings returns redacted reconciliation findings.
func (r *Repository) ListFindings(ctx context.Context, scope tenancy.Scope, limit int) ([]procurement.ReconciliationFinding, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		return nil, procurement.ErrInvalid
	}
	items := make([]procurement.ReconciliationFinding, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT finding_id,kind,purchase_order_id,supplier_offer_id,expected,observed,status,detected_at FROM procurement_reconciliation_findings WHERE organization_id=$1 AND workspace_id=$2 ORDER BY detected_at DESC,finding_id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item procurement.ReconciliationFinding
			if err := rows.Scan(&item.ID, &item.Kind, &item.PurchaseOrderID, &item.SupplierOfferID, &item.Expected, &item.Observed, &item.Status, &item.DetectedAt); err != nil {
				return err
			}
			item.DetectedAt = item.DetectedAt.UTC()
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// DetectFindings records bounded deterministic drifts for overdue receipts and
// unknown send outcomes. It is safe to call before every operator refresh.
func (r *Repository) DetectFindings(ctx context.Context, scope tenancy.Scope, now time.Time, m procurement.Mutation) error {
	if err := r.validateMutation(ctx, scope, m); err != nil || now.IsZero() || now.Location() != time.UTC {
		return procurement.ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,send_state FROM purchase_orders WHERE organization_id=$1 AND workspace_id=$2 AND ((expected_receipt_at IS NOT NULL AND expected_receipt_at < $3 AND status NOT IN ('received','cancelled')) OR send_state='unknown') LIMIT 100`, scope.OrganizationID(), scope.WorkspaceID(), now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, sendState string
			if err := rows.Scan(&id, &sendState); err != nil {
				return err
			}
			kind, expected, observed := "overdue_receipt", "receipt before expected date", now.Format(time.RFC3339)
			if sendState == "unknown" {
				kind, expected, observed = "unknown_send_outcome", "remote outcome", "unknown"
			}
			findingID := fmt.Sprintf("%s/%s", id, kind)
			if _, err := tx.ExecContext(ctx, `INSERT INTO procurement_reconciliation_findings(organization_id,workspace_id,finding_id,kind,purchase_order_id,expected,observed,status,detected_at) VALUES($1,$2,$3,$4,$5,$6,$7,'open',$8) ON CONFLICT DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), findingID, kind, id, expected, observed, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func supplierInputValid(item procurement.SupplierRecord, scope tenancy.Scope) bool {
	return safeID(item.ID) && safeID(item.LegalPartyID) && strings.TrimSpace(item.Name) == item.Name && item.Name != "" && len(item.Name) <= 200 && item.Status.Valid() && item.Currency.Validate() == nil && item.MinimumOrderCurrency.Validate() == nil && item.LeadTimeDays >= 0 && item.LeadTimeDays <= 3650 && item.MinimumOrderMinor >= 0 && item.Version >= 1 && scope.Valid()
}

func offerInputValid(item procurement.SupplierOfferRecord) bool {
	return safeID(item.ID) && safeID(item.SupplierID) && strings.TrimSpace(item.SKU) == item.SKU && item.SKU != "" && item.UnitPriceMinor >= 0 && item.Currency.Validate() == nil && item.MinimumOrderCurrency.Validate() == nil && item.MOQ.Validate() == nil && item.CasePack.Validate() == nil && item.LeadTimeDays >= 0 && item.LeadTimeDays <= 3650 && item.Priority >= 0 && item.ValidFrom.UTC() == item.ValidFrom && item.ValidUntil.UTC() == item.ValidUntil && !item.ValidUntil.Before(item.ValidFrom)
}

func scanSupplier(row interface{ Scan(...any) error }, out ...*procurement.SupplierRecord) (procurement.SupplierRecord, error) {
	var item procurement.SupplierRecord
	var currency, minCurrency, status string
	var contacts, contracts []byte
	if err := row.Scan(&item.ID, &item.LegalPartyID, &item.Name, &status, &item.PaymentTerms, &currency, &item.LeadTimeDays, &item.MinimumOrderMinor, &minCurrency, &contacts, &contracts, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, ErrNotFound
		}
		return item, err
	}
	item.Status = procurement.SupplierStatus(status)
	item.Currency, _ = domain.NewCurrency(currency)
	item.MinimumOrderCurrency, _ = domain.NewCurrency(minCurrency)
	if json.Unmarshal(contacts, &item.Contacts) != nil || json.Unmarshal(contracts, &item.Contracts) != nil {
		return procurement.SupplierRecord{}, procurement.ErrInvalid
	}
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	if len(out) > 0 {
		*out[0] = item
	}
	return item, nil
}

func scanOffer(row interface{ Scan(...any) error }) (procurement.SupplierOfferRecord, error) {
	var item procurement.SupplierOfferRecord
	var currency, minCurrency, moq, pack string
	if err := row.Scan(&item.ID, &item.SupplierID, &item.CanonicalOfferID, &item.SupplierSKU, &item.GTIN, &item.SKU, &item.Unit, &item.UnitPriceMinor, &currency, &moq, &pack, &item.LeadTimeDays, &item.Priority, &item.MinimumOrderMinor, &minCurrency, &item.ValidFrom, &item.ValidUntil, &item.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, ErrNotFound
		}
		return item, err
	}
	var err error
	item.Currency, err = domain.NewCurrency(currency)
	if err != nil {
		return item, procurement.ErrInvalid
	}
	item.MinimumOrderCurrency, err = domain.NewCurrency(minCurrency)
	if err != nil {
		return item, procurement.ErrInvalid
	}
	item.MOQ, err = quantity(moq, item.Unit)
	if err != nil {
		return item, procurement.ErrInvalid
	}
	item.CasePack, err = quantity(pack, item.Unit)
	if err != nil {
		return item, procurement.ErrInvalid
	}
	item.ValidFrom, item.ValidUntil = item.ValidFrom.UTC(), item.ValidUntil.UTC()
	return item, nil
}

func scanPO(row interface{ Scan(...any) error }, scope tenancy.Scope) (procurement.PurchaseOrderRecord, error) {
	var item procurement.PurchaseOrderRecord
	var status, currency string
	if err := row.Scan(&item.ID, &item.SupplierID, &status, &currency, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.WarehouseID, &item.RecommendationID, &item.RecommendationDigest, &item.IdempotencyKey, &item.ApprovalRequestID, &item.SendState, &item.ErrorCode, &item.ExpectedReceiptAt, &item.CreatorID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, ErrNotFound
		}
		return item, err
	}
	item.OrganizationID, item.WorkspaceID = scope.OrganizationID().String(), scope.WorkspaceID().String()
	item.Status = procurement.POStatus(status)
	item.Currency, _ = domain.NewCurrency(currency)
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	return item, nil
}

func loadPO(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id string, out *procurement.PurchaseOrderRecord) error {
	item, err := scanPO(tx.QueryRowContext(ctx, `SELECT id,supplier_id,status,currency,version,created_at,updated_at,warehouse_id,recommendation_id,recommendation_digest,idempotency_key,approval_request_id,send_state,error_code,expected_receipt_at,creator_id FROM purchase_orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id), scope)
	if err != nil {
		return err
	}
	if err := loadLines(ctx, tx, scope, &item); err != nil {
		return err
	}
	*out = item
	return nil
}

func loadLines(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, po *procurement.PurchaseOrderRecord) error {
	rows, err := tx.QueryContext(ctx, `SELECT line_id,offer_id,sku,quantity,unit,unit_price_minor FROM purchase_order_lines WHERE organization_id=$1 AND workspace_id=$2 AND purchase_order_id=$3 ORDER BY line_id`, scope.OrganizationID(), scope.WorkspaceID(), po.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	po.Lines = nil
	for rows.Next() {
		var id, offer, sku, value, unit string
		var price int64
		if err := rows.Scan(&id, &offer, &sku, &value, &unit, &price); err != nil {
			return err
		}
		q, err := quantity(value, unit)
		if err != nil {
			return err
		}
		money, err := domain.NewMoney(price, po.Currency)
		if err != nil {
			return err
		}
		po.Lines = append(po.Lines, procurement.Line{ID: id, OfferID: offer, SKU: sku, Quantity: q, UnitPrice: money})
	}
	return rows.Err()
}

func quantity(value, unit string) (domain.Quantity, error) {
	d, err := domain.ParseDecimal(value)
	if err != nil {
		return domain.Quantity{}, err
	}
	u, err := domain.NewUnitCode(unit)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.NewQuantity(d, u)
}

func matchOffer(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, supplierID string, row procurement.PriceListRow) (string, string, error) {
	if row.CanonicalOfferID != "" {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM supplier_offers WHERE organization_id=$1 AND workspace_id=$2 AND supplier_id=$3 AND id=$4`, scope.OrganizationID(), scope.WorkspaceID(), supplierID, row.CanonicalOfferID).Scan(&id)
		if err == nil {
			return id, "manual", nil
		}
		return "", "", nil
	}
	if row.GTIN != "" {
		ids, err := offerIDs(ctx, tx, scope, supplierID, `gtin=$4`, row.GTIN)
		if err != nil {
			return "", "", err
		}
		if len(ids) == 1 {
			return ids[0], "gtin_exact", nil
		}
		if len(ids) > 1 {
			return "", "", nil
		}
	}
	if row.SupplierSKU != "" {
		ids, err := offerIDs(ctx, tx, scope, supplierID, `supplier_sku=$4`, row.SupplierSKU)
		if err != nil {
			return "", "", err
		}
		if len(ids) == 1 {
			return ids[0], "supplier_sku_exact", nil
		}
	}
	return "", "", nil
}

func offerIDs(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, supplierID, predicate, value string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM supplier_offers WHERE organization_id=$1 AND workspace_id=$2 AND supplier_id=$3 AND `+predicate+` LIMIT 3`, scope.OrganizationID(), scope.WorkspaceID(), supplierID, value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func insertOfferHistory(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, item procurement.SupplierOfferRecord, m procurement.Mutation, version int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO procurement_supplier_offer_history(organization_id,workspace_id,history_id,supplier_offer_id,version,unit_price_minor,currency,minimum_quantity,case_pack,lead_time_days,valid_from,valid_until,changed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, scope.OrganizationID(), scope.WorkspaceID(), m.EventID+"/"+item.ID+fmt.Sprintf("/%d", version), item.ID, version, item.UnitPriceMinor, item.Currency, item.MOQ.Value.String(), item.CasePack.Value.String(), item.LeadTimeDays, item.ValidFrom, item.ValidUntil, m.OccurredAt)
	return mapError(err)
}

func appendPOEvent(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, m procurement.Mutation, po procurement.PurchaseOrderRecord, from procurement.POStatus, action string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO procurement_purchase_order_events(organization_id,workspace_id,event_id,purchase_order_id,from_status,to_status,action,actor_id,idempotency_key,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), m.EventID+"/"+po.ID, po.ID, from, po.Status, action, m.ActorID, m.CorrelationID, m.OccurredAt)
	return mapError(err)
}

func idempotentPO(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, purchaseOrderID, key string) (procurement.PurchaseOrderRecord, bool, error) {
	if key == "" {
		return procurement.PurchaseOrderRecord{}, false, nil
	}
	var eventID string
	err := tx.QueryRowContext(ctx, `SELECT event_id FROM procurement_purchase_order_events WHERE organization_id=$1 AND workspace_id=$2 AND purchase_order_id=$3 AND idempotency_key=$4`, scope.OrganizationID(), scope.WorkspaceID(), purchaseOrderID, key).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return procurement.PurchaseOrderRecord{}, false, nil
	}
	if err != nil {
		return procurement.PurchaseOrderRecord{}, false, err
	}
	var item procurement.PurchaseOrderRecord
	if err := loadPO(ctx, tx, scope, purchaseOrderID, &item); err != nil {
		return procurement.PurchaseOrderRecord{}, false, err
	}
	return item, true, nil
}

func appendAudit(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, m procurement.Mutation, action, resourceType, resourceID string, risk audit.Risk, summary audit.Summary) error {
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	return auditrepo.AppendTransaction(ctx, tx, scope, audit.Record{ID: m.AuditID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: m.CorrelationID, Risk: risk, Summary: safe, CreatedAt: m.OccurredAt})
}

func enqueue(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, m procurement.Mutation, eventType, entityType, entityID string, payload map[string]any) error {
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	instant, err := domain.NewUTCInstant(m.OccurredAt)
	if err != nil {
		return err
	}
	event := eventbus.Event{ID: m.EventID, Type: typ, OccurredAt: instant, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: entityType, EntityID: entityID, Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: data}
	if err := event.Validate(); err != nil {
		return err
	}
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, event)
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return procurement.ErrInvalid
	}
	return ctx.Err()
}
func (r *Repository) validateMutation(ctx context.Context, scope tenancy.Scope, m procurement.Mutation) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if m.Validate() != nil {
		return procurement.ErrInvalid
	}
	return nil
}
func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return err
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return procurement.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate key") || strings.Contains(message, "version") || strings.Contains(message, "unique") {
		return ErrConflict
	}
	return err
}
func safeID(value string) bool {
	return value != "" && len(value) <= 192 && !strings.ContainsAny(value, "\r\n\t /")
}
func safeUploadID(value string) bool {
	return strings.HasPrefix(value, "upl_") && len(value) == 36 && safeID(value)
}
func mapSendState(status procurement.POStatus) string {
	if status == procurement.POSent {
		return "sent"
	}
	return "not_sent"
}
func appendBoundedError(items []procurement.ImportError, item procurement.ImportError) []procurement.ImportError {
	if len(items) < 100 {
		return append(items, item)
	}
	return items
}

func hasImportError(items []procurement.ImportError, row int) bool {
	for _, item := range items {
		if item.Row == row && item.Code != "unresolved_or_ambiguous_match" {
			return true
		}
	}
	return false
}
func decimalCompare(left, right string) int {
	l, lok := new(big.Rat).SetString(left)
	r, rok := new(big.Rat).SetString(right)
	if !lok || !rok {
		return 1
	}
	return l.Cmp(r)
}
func addDecimalText(left, right string) string {
	l, lok := new(big.Rat).SetString(left)
	r, rok := new(big.Rat).SetString(right)
	if !lok || !rok {
		return "-1"
	}
	return new(big.Rat).Add(l, r).FloatString(9)
}
