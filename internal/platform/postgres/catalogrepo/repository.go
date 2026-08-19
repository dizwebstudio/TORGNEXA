// Package catalogrepo implements the tenant-scoped PostgreSQL catalog repository.
package catalogrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const productSelect = `SELECT id, organization_id, workspace_id, code, title, description, status, version, created_at, updated_at
FROM products WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
const offerSelect = `SELECT id, organization_id, workspace_id, product_id, sku, COALESCE(gtin,''), status, version, created_at, updated_at
FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

// Repository persists canonical Product/Offer aggregates and writes the
// corresponding Task-007 event intent through Task-008 outbox in the same SQL transaction.
type Repository struct{ database *sql.DB }

var _ catalog.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("catalog repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) Product(ctx context.Context, scope catalog.Scope, id catalog.ProductID) (catalog.Product, error) {
	if err := validateRead(ctx, repository, scope); err != nil {
		return catalog.Product{}, err
	}
	if !id.Valid() {
		return catalog.Product{}, catalog.ErrInvalidRecord
	}
	var result catalog.Product
	err := repository.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		var err error
		result, err = scanProduct(tx.QueryRowContext(ctx, productSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (repository *Repository) Offer(ctx context.Context, scope catalog.Scope, id catalog.OfferID) (catalog.Offer, error) {
	if err := validateRead(ctx, repository, scope); err != nil {
		return catalog.Offer{}, err
	}
	if !id.Valid() {
		return catalog.Offer{}, catalog.ErrInvalidRecord
	}
	var result catalog.Offer
	err := repository.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		var err error
		result, err = scanOffer(tx.QueryRowContext(ctx, offerSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (repository *Repository) OffersByProduct(ctx context.Context, scope catalog.Scope, productID catalog.ProductID, limit int) ([]catalog.Offer, error) {
	if err := validateRead(ctx, repository, scope); err != nil {
		return nil, err
	}
	if !productID.Valid() || limit < 1 || limit > 1000 {
		return nil, catalog.ErrInvalidRecord
	}
	var result []catalog.Offer
	err := repository.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, organization_id, workspace_id, product_id, sku, COALESCE(gtin,''), status, version, created_at, updated_at
FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 ORDER BY id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), productID.String(), limit)
		if err != nil {
			return fmt.Errorf("catalog repository: list offers: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			offer, err := scanOffer(rows)
			if err != nil {
				return err
			}
			result = append(result, offer)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("catalog repository: list offers: %w", err)
		}
		return nil
	})
	return result, err
}

func (repository *Repository) CreateProduct(ctx context.Context, scope catalog.Scope, command catalog.CreateProduct, mutation catalog.Mutation) (catalog.Product, error) {
	if err := validateMutation(ctx, repository, scope, mutation); err != nil {
		return catalog.Product{}, err
	}
	if err := command.Validate(); err != nil {
		return catalog.Product{}, err
	}
	var result catalog.Product
	err := repository.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO products (id, organization_id, workspace_id, code, title, description)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING
RETURNING id, organization_id, workspace_id, code, title, description, status, version, created_at, updated_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.Code, command.Title, command.Description)
		var err error
		result, err = scanProduct(row)
		if errors.Is(err, catalog.ErrNotFound) {
			return catalog.ErrConflict
		}
		if err != nil {
			return err
		}
		return enqueueProductEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

func (repository *Repository) UpdateProduct(ctx context.Context, scope catalog.Scope, command catalog.UpdateProduct, mutation catalog.Mutation) (catalog.Product, error) {
	if err := validateMutation(ctx, repository, scope, mutation); err != nil {
		return catalog.Product{}, err
	}
	if err := command.Validate(); err != nil {
		return catalog.Product{}, err
	}
	var result catalog.Product
	err := repository.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE products SET title=$4, description=$5, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 AND status <> 'archived'
RETURNING id, organization_id, workspace_id, code, title, description, status, version, created_at, updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), command.Title, command.Description, command.ExpectedVersion)
		var err error
		result, err = scanProduct(row)
		if errors.Is(err, catalog.ErrNotFound) {
			return classifyProductMutationMiss(ctx, tx, scope, command.ID, command.ExpectedVersion)
		}
		if err != nil {
			return err
		}
		return enqueueProductEvent(ctx, tx, scope, mutation, result, "updated")
	})
	return result, err
}

func (repository *Repository) ChangeProductStatus(ctx context.Context, scope catalog.Scope, command catalog.ChangeProductStatus, mutation catalog.Mutation) (catalog.Product, error) {
	if err := validateMutation(ctx, repository, scope, mutation); err != nil {
		return catalog.Product{}, err
	}
	if err := command.Validate(); err != nil {
		return catalog.Product{}, err
	}
	var result catalog.Product
	err := repository.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		current, err := scanProduct(tx.QueryRowContext(ctx, productSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return catalog.ErrConflict
		}
		if err := catalog.ValidateProductTransition(current.Status, command.Status); err != nil {
			return err
		}
		if command.Status == catalog.StatusArchived {
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND product_id=$3 AND status <> 'archived')`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()).Scan(&exists); err != nil {
				return fmt.Errorf("catalog repository: check product offers: %w", err)
			}
			if exists {
				return catalog.ErrProductHasOffers
			}
		}
		result, err = scanProduct(tx.QueryRowContext(ctx, `UPDATE products SET status=$4, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5
RETURNING id, organization_id, workspace_id, code, title, description, status, version, created_at, updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), string(command.Status), command.ExpectedVersion))
		if err != nil {
			return err
		}
		return enqueueProductEvent(ctx, tx, scope, mutation, result, "status_changed")
	})
	return result, err
}

func (repository *Repository) CreateOffer(ctx context.Context, scope catalog.Scope, command catalog.CreateOffer, mutation catalog.Mutation) (catalog.Offer, error) {
	if err := validateMutation(ctx, repository, scope, mutation); err != nil {
		return catalog.Offer{}, err
	}
	if err := command.Validate(); err != nil {
		return catalog.Offer{}, err
	}
	var result catalog.Offer
	err := repository.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		product, err := scanProduct(tx.QueryRowContext(ctx, productSelect+` FOR SHARE`, scope.OrganizationID(), scope.WorkspaceID(), command.ProductID.String()))
		if err != nil {
			return err
		}
		if product.Status == catalog.StatusArchived {
			return catalog.ErrInvalidState
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO offers (id, organization_id, workspace_id, product_id, sku, gtin)
VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')) ON CONFLICT DO NOTHING
RETURNING id, organization_id, workspace_id, product_id, sku, COALESCE(gtin,''), status, version, created_at, updated_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.ProductID.String(), command.SKU, command.GTIN)
		result, err = scanOffer(row)
		if errors.Is(err, catalog.ErrNotFound) {
			return catalog.ErrConflict
		}
		if err != nil {
			return err
		}
		return enqueueOfferEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

func (repository *Repository) UpdateOffer(ctx context.Context, scope catalog.Scope, command catalog.UpdateOffer, mutation catalog.Mutation) (catalog.Offer, error) {
	if err := validateMutation(ctx, repository, scope, mutation); err != nil {
		return catalog.Offer{}, err
	}
	if err := command.Validate(); err != nil {
		return catalog.Offer{}, err
	}
	var result catalog.Offer
	err := repository.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE offers SET gtin=NULLIF($4,''), version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 AND status <> 'archived'
RETURNING id, organization_id, workspace_id, product_id, sku, COALESCE(gtin,''), status, version, created_at, updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), command.GTIN, command.ExpectedVersion)
		var err error
		result, err = scanOffer(row)
		if errors.Is(err, catalog.ErrNotFound) {
			return classifyOfferMutationMiss(ctx, tx, scope, command.ID, command.ExpectedVersion)
		}
		if err != nil {
			return err
		}
		return enqueueOfferEvent(ctx, tx, scope, mutation, result, "updated")
	})
	return result, err
}

func (repository *Repository) ChangeOfferStatus(ctx context.Context, scope catalog.Scope, command catalog.ChangeOfferStatus, mutation catalog.Mutation) (catalog.Offer, error) {
	if err := validateMutation(ctx, repository, scope, mutation); err != nil {
		return catalog.Offer{}, err
	}
	if err := command.Validate(); err != nil {
		return catalog.Offer{}, err
	}
	var result catalog.Offer
	err := repository.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		current, err := scanOffer(tx.QueryRowContext(ctx, offerSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return catalog.ErrConflict
		}
		if err := catalog.ValidateOfferTransition(current.Status, command.Status); err != nil {
			return err
		}
		if command.Status == catalog.StatusActive {
			product, err := scanProduct(tx.QueryRowContext(ctx, productSelect+` FOR SHARE`, scope.OrganizationID(), scope.WorkspaceID(), current.ProductID.String()))
			if err != nil {
				return err
			}
			if product.Status != catalog.StatusActive {
				return catalog.ErrInvalidState
			}
		}
		result, err = scanOffer(tx.QueryRowContext(ctx, `UPDATE offers SET status=$4, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5
RETURNING id, organization_id, workspace_id, product_id, sku, COALESCE(gtin,''), status, version, created_at, updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), string(command.Status), command.ExpectedVersion))
		if err != nil {
			return err
		}
		return enqueueOfferEvent(ctx, tx, scope, mutation, result, "status_changed")
	})
	return result, err
}

func validateRead(ctx context.Context, repository *Repository, scope catalog.Scope) error {
	if ctx == nil {
		return errors.New("catalog repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.database == nil {
		return errors.New("catalog repository: repository is not initialized")
	}
	if !scope.Valid() {
		return catalog.ErrInvalidScope
	}
	return nil
}
func validateMutation(ctx context.Context, repository *Repository, scope catalog.Scope, mutation catalog.Mutation) error {
	if err := validateRead(ctx, repository, scope); err != nil {
		return err
	}
	return mutation.Validate()
}

func (repository *Repository) withTx(ctx context.Context, readOnly bool, scope catalog.Scope, operation func(*sql.Tx) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("catalog repository: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID(), scope.WorkspaceID()).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("catalog repository: apply tenant scope: %w", err)
	}
	if organizationID != scope.OrganizationID() || workspaceID != scope.WorkspaceID() {
		return catalog.ErrInvalidScope
	}
	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog repository: commit transaction: %w", err)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (catalog.Product, error) {
	var product catalog.Product
	var id, organizationID, workspaceID, status string
	if err := row.Scan(&id, &organizationID, &workspaceID, &product.Code, &product.Title, &product.Description, &status, &product.Version, &product.CreatedAt, &product.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Product{}, catalog.ErrNotFound
		}
		return catalog.Product{}, fmt.Errorf("catalog repository: scan product: %w", err)
	}
	product.ID = catalog.ProductID(id)
	product.OrganizationID = organizationID
	product.WorkspaceID = workspaceID
	product.Status = catalog.Status(status)
	product.CreatedAt = product.CreatedAt.UTC()
	product.UpdatedAt = product.UpdatedAt.UTC()
	if err := product.Validate(); err != nil {
		return catalog.Product{}, err
	}
	return product, nil
}
func scanOffer(row scanner) (catalog.Offer, error) {
	var offer catalog.Offer
	var id, organizationID, workspaceID, productID, status string
	if err := row.Scan(&id, &organizationID, &workspaceID, &productID, &offer.SKU, &offer.GTIN, &status, &offer.Version, &offer.CreatedAt, &offer.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Offer{}, catalog.ErrNotFound
		}
		return catalog.Offer{}, fmt.Errorf("catalog repository: scan offer: %w", err)
	}
	offer.ID = catalog.OfferID(id)
	offer.ProductID = catalog.ProductID(productID)
	offer.OrganizationID = organizationID
	offer.WorkspaceID = workspaceID
	offer.Status = catalog.Status(status)
	offer.CreatedAt = offer.CreatedAt.UTC()
	offer.UpdatedAt = offer.UpdatedAt.UTC()
	if err := offer.Validate(); err != nil {
		return catalog.Offer{}, err
	}
	return offer, nil
}

func classifyProductMutationMiss(ctx context.Context, tx *sql.Tx, scope catalog.Scope, id catalog.ProductID, expected int64) error {
	current, err := scanProduct(tx.QueryRowContext(ctx, productSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
	if err != nil {
		return err
	}
	if current.Status == catalog.StatusArchived {
		return catalog.ErrInvalidState
	}
	if current.Version != expected {
		return catalog.ErrConflict
	}
	return catalog.ErrConflict
}
func classifyOfferMutationMiss(ctx context.Context, tx *sql.Tx, scope catalog.Scope, id catalog.OfferID, expected int64) error {
	current, err := scanOffer(tx.QueryRowContext(ctx, offerSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
	if err != nil {
		return err
	}
	if current.Status == catalog.StatusArchived {
		return catalog.ErrInvalidState
	}
	if current.Version != expected {
		return catalog.ErrConflict
	}
	return catalog.ErrConflict
}

func enqueueProductEvent(ctx context.Context, tx *sql.Tx, scope catalog.Scope, mutation catalog.Mutation, product catalog.Product, change string) error {
	event, err := buildProductEvent(scope, mutation, product, change)
	if err != nil {
		return err
	}
	return enqueueEvent(ctx, tx, event)
}
func enqueueOfferEvent(ctx context.Context, tx *sql.Tx, scope catalog.Scope, mutation catalog.Mutation, offer catalog.Offer, change string) error {
	event, err := buildOfferEvent(scope, mutation, offer, change)
	if err != nil {
		return err
	}
	return enqueueEvent(ctx, tx, event)
}
func buildProductEvent(scope catalog.Scope, mutation catalog.Mutation, product catalog.Product, change string) (eventbus.Event, error) {
	data, err := json.Marshal(struct {
		ProductID string         `json:"product_id"`
		Version   int64          `json:"version"`
		Status    catalog.Status `json:"status"`
		Change    string         `json:"change"`
	}{product.ID.String(), product.Version, product.Status, change})
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("catalog repository: encode product event: %w", err)
	}
	return buildEvent(scope, mutation, "commerce.catalog.product_changed.v1", "product", product.ID.String(), data)
}
func buildOfferEvent(scope catalog.Scope, mutation catalog.Mutation, offer catalog.Offer, change string) (eventbus.Event, error) {
	data, err := json.Marshal(struct {
		OfferID   string         `json:"offer_id"`
		ProductID string         `json:"product_id"`
		Version   int64          `json:"version"`
		Status    catalog.Status `json:"status"`
		Change    string         `json:"change"`
	}{offer.ID.String(), offer.ProductID.String(), offer.Version, offer.Status, change})
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("catalog repository: encode offer event: %w", err)
	}
	return buildEvent(scope, mutation, "commerce.catalog.offer_changed.v1", "offer", offer.ID.String(), data)
}
func buildEvent(scope catalog.Scope, mutation catalog.Mutation, typeValue, entityType, entityID string, data json.RawMessage) (eventbus.Event, error) {
	eventType, err := eventbus.ParseEventType(typeValue)
	if err != nil {
		return eventbus.Event{}, err
	}
	occurredAt, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return eventbus.Event{}, err
	}
	event := eventbus.Event{ID: mutation.EventID, Type: eventType, OccurredAt: occurredAt, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, TraceID: mutation.TraceID, Data: data}
	if err := event.Validate(); err != nil {
		return eventbus.Event{}, fmt.Errorf("catalog repository: event: %w", err)
	}
	return event, nil
}
func enqueueEvent(ctx context.Context, tx *sql.Tx, event eventbus.Event) error {
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	if err := enqueuer.Enqueue(ctx, event); err != nil {
		return fmt.Errorf("catalog repository: enqueue event: %w", err)
	}
	return nil
}
