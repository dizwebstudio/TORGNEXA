// Package catalogimagerepo persists tenant-scoped product image references.
package catalogimagerepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

var ErrInvalid = errors.New("catalog image: invalid")

type Image struct {
	ID, ProductID, URL, AltText string
	Position                    int
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// validImage accepts either a real externally hosted https:// URL, a built-in
// demo asset, or the exact server-relative content path of a released upload
// (see uploads.ContentPath) — never an arbitrary relative path.
func validImage(v Image) bool {
	if len(v.URL) > 2048 || len(strings.TrimSpace(v.AltText)) > 300 || v.Position < 0 || v.Position > 255 {
		return false
	}
	if uploads.ValidContentPath(v.URL) || validDemoImagePath(v.URL) {
		return true
	}
	u, err := url.ParseRequestURI(v.URL)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func validDemoImagePath(value string) bool {
	const prefix, suffix = "/demo-images/demo-", ".svg"
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (r *Repository) List(ctx context.Context, scope tenancy.Scope, productID string) ([]Image, error) {
	var out []Image
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,product_id,url,alt_text,position,version,created_at,updated_at FROM catalog_product_images WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 ORDER BY position,id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), productID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v Image
			if err := rows.Scan(&v.ID, &v.ProductID, &v.URL, &v.AltText, &v.Position, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) Create(ctx context.Context, scope tenancy.Scope, v Image) (Image, error) {
	if !validImage(v) || strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.ProductID) == "" {
		return Image{}, ErrInvalid
	}
	var out Image
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO catalog_product_images(id,organization_id,workspace_id,product_id,url,alt_text,position) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,product_id,url,alt_text,position,version,created_at,updated_at`, v.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), v.ProductID, v.URL, strings.TrimSpace(v.AltText), v.Position).Scan(&out.ID, &out.ProductID, &out.URL, &out.AltText, &out.Position, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	})
	return out, err
}

func (r *Repository) Update(ctx context.Context, scope tenancy.Scope, v Image) (Image, error) {
	if !validImage(v) || v.Version < 1 {
		return Image{}, ErrInvalid
	}
	var out Image
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `UPDATE catalog_product_images SET url=$4,alt_text=$5,position=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7 RETURNING id,product_id,url,alt_text,position,version,created_at,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), v.ID, v.URL, strings.TrimSpace(v.AltText), v.Position, v.Version).Scan(&out.ID, &out.ProductID, &out.URL, &out.AltText, &out.Position, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	})
	return out, err
}

// Delete removes one image reference from a tenant-scoped product card.
func (r *Repository) Delete(ctx context.Context, scope tenancy.Scope, productID, imageID string) error {
	if strings.TrimSpace(productID) == "" || strings.TrimSpace(imageID) == "" {
		return ErrInvalid
	}
	return r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM catalog_product_images WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 AND id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), productID, imageID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (r *Repository) withTx(ctx context.Context, readOnly bool, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if !scope.Valid() {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var org, ws string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("catalog image: scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
