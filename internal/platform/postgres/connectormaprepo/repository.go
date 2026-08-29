// Package connectormaprepo persists provider-neutral local/remote entity mappings.
package connectormaprepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	mappingport "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`
const mappingSelect = `SELECT organization_id, workspace_id, connector_account_id, entity_type, local_entity_id, remote_id, version, created_at, updated_at FROM connector_entity_mappings`

var entityTypePattern = regexp.MustCompile(`^(product|offer|order|warehouse|brand|category|attribute|legal_entity|individual_entrepreneur|branch|counterparty|bank_account|contract|authority_reference)$`)
var localIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type Repository struct{ database *sql.DB }

var _ mappingport.MappingRepository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("connector mapping repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) UpsertMapping(ctx context.Context, command mappingport.MappingUpsert) (mappingport.EntityMapping, error) {
	if ctx == nil {
		return mappingport.EntityMapping{}, errors.New("connector mapping repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return mappingport.EntityMapping{}, err
	}
	if repository == nil || repository.database == nil {
		return mappingport.EntityMapping{}, errors.New("connector mapping repository: repository is not initialized")
	}
	if err := command.Validate(); err != nil || !entityTypePattern.MatchString(command.EntityType) {
		return mappingport.EntityMapping{}, mappingport.ErrInvalidMapping
	}
	scope, err := tenancy.ParseScope(command.OrganizationID, command.WorkspaceID)
	if err != nil {
		return mappingport.EntityMapping{}, tenancy.ErrInvalidScope
	}
	var result mappingport.EntityMapping
	err = repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var row *sql.Row
		if command.ExpectedVersion == 0 {
			row = tx.QueryRowContext(ctx, `INSERT INTO connector_entity_mappings (organization_id,workspace_id,connector_account_id,entity_type,local_entity_id,remote_id)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING
RETURNING organization_id, workspace_id, connector_account_id, entity_type, local_entity_id, remote_id, version, created_at, updated_at`, command.OrganizationID, command.WorkspaceID, command.ConnectorAccountID, command.EntityType, command.LocalEntityID, command.RemoteID)
		} else {
			row = tx.QueryRowContext(ctx, `UPDATE connector_entity_mappings SET remote_id=$6, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type=$4 AND local_entity_id=$5 AND version=$7
RETURNING organization_id, workspace_id, connector_account_id, entity_type, local_entity_id, remote_id, version, created_at, updated_at`, command.OrganizationID, command.WorkspaceID, command.ConnectorAccountID, command.EntityType, command.LocalEntityID, command.RemoteID, command.ExpectedVersion)
		}
		result, err = scanMapping(row)
		if errors.Is(err, mappingport.ErrMappingNotFound) {
			return mappingport.ErrMappingConflict
		}
		return err
	})
	return result, err
}

func (repository *Repository) MappingByLocal(ctx context.Context, organizationID, workspaceID, connectorAccountID, entityType, localEntityID string) (mappingport.EntityMapping, error) {
	if !entityTypePattern.MatchString(entityType) || !localIDPattern.MatchString(localEntityID) {
		return mappingport.EntityMapping{}, mappingport.ErrInvalidMapping
	}
	return repository.lookup(ctx, organizationID, workspaceID, mappingSelect+` WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type=$4 AND local_entity_id=$5`, connectorAccountID, entityType, localEntityID)
}
func (repository *Repository) MappingByRemote(ctx context.Context, organizationID, workspaceID, connectorAccountID, entityType, remoteID string) (mappingport.EntityMapping, error) {
	if !entityTypePattern.MatchString(entityType) || remoteID == "" {
		return mappingport.EntityMapping{}, mappingport.ErrInvalidMapping
	}
	return repository.lookup(ctx, organizationID, workspaceID, mappingSelect+` WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type=$4 AND remote_id=$5`, connectorAccountID, entityType, remoteID)
}

func (repository *Repository) lookup(ctx context.Context, organizationID, workspaceID, statement, accountID, entityType, identity string) (mappingport.EntityMapping, error) {
	if ctx == nil {
		return mappingport.EntityMapping{}, errors.New("connector mapping repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return mappingport.EntityMapping{}, err
	}
	if repository == nil || repository.database == nil {
		return mappingport.EntityMapping{}, errors.New("connector mapping repository: repository is not initialized")
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		return mappingport.EntityMapping{}, tenancy.ErrInvalidScope
	}
	var result mappingport.EntityMapping
	err = repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanMapping(tx.QueryRowContext(ctx, statement, organizationID, workspaceID, accountID, entityType, identity))
		return err
	})
	return result, err
}

func (repository *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, operation func(*sql.Tx) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("connector mapping repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("connector mapping repository: scope: %w", err)
	}
	if org != scope.OrganizationID().String() || ws != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connector mapping repository: commit: %w", err)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanMapping(row scanner) (mappingport.EntityMapping, error) {
	var mapping mappingport.EntityMapping
	if err := row.Scan(&mapping.OrganizationID, &mapping.WorkspaceID, &mapping.ConnectorAccountID, &mapping.EntityType, &mapping.LocalEntityID, &mapping.RemoteID, &mapping.Version, &mapping.CreatedAt, &mapping.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return mappingport.EntityMapping{}, mappingport.ErrMappingNotFound
		}
		return mappingport.EntityMapping{}, fmt.Errorf("connector mapping repository: scan: %w", err)
	}
	mapping.CreatedAt = mapping.CreatedAt.UTC()
	mapping.UpdatedAt = mapping.UpdatedAt.UTC()
	if err := mapping.Validate(); err != nil {
		return mappingport.EntityMapping{}, err
	}
	return mapping, nil
}
