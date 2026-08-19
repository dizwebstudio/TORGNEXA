package pimrepo

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/pim"
)

// Categories returns canonical categories available in the tenant scope.
func (r *Repository) Categories(ctx context.Context, scope pim.Scope, limit int) ([]pim.Category, error) {
	if !scope.Valid() || limit < 1 || limit > 1000 {
		return nil, pim.ErrInvalid
	}
	var result []pim.Category
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,code,name,COALESCE(parent_id,''),status,version,created_at,updated_at FROM pim_categories WHERE organization_id=$1 AND workspace_id=$2 AND status<>'archived' ORDER BY name,id LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return fmt.Errorf("pim repository: list categories: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanCategory(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

// CategoriesByProduct returns active category assignments for a product.
func (r *Repository) CategoriesByProduct(ctx context.Context, scope pim.Scope, productID pim.ID) ([]pim.Category, error) {
	if !scope.Valid() || !productID.Valid() {
		return nil, pim.ErrInvalid
	}
	var result []pim.Category
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT c.id,c.organization_id,c.workspace_id,c.code,c.name,COALESCE(c.parent_id,''),c.status,c.version,c.created_at,c.updated_at FROM pim_categories c JOIN pim_product_categories pc ON pc.organization_id=c.organization_id AND pc.workspace_id=c.workspace_id AND pc.category_id=c.id WHERE pc.organization_id=$1 AND pc.workspace_id=$2 AND pc.product_id=$3 AND pc.active AND c.status<>'archived' ORDER BY pc.is_primary DESC,c.name,c.id`, scope.OrganizationID(), scope.WorkspaceID(), productID.String())
		if err != nil {
			return fmt.Errorf("pim repository: product categories: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanCategory(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}
