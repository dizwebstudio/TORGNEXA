// Package connectorrepo persists Connector SDK account metadata. Provider
// credentials are never stored here; only opaque secret references are allowed.
package connectorrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

var bulkAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

const accountSelect = `SELECT id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, health_reason_code, health_checked_at, created_at, updated_at
FROM connector_accounts`

const listAccountsStatement = accountSelect + `
 WHERE organization_id=$1 AND workspace_id=$2 AND id>$3
 ORDER BY id ASC LIMIT $4`

const createAccountStatement = `INSERT INTO connector_accounts (
  id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,'disabled',$6,1,'unknown',clock_timestamp(),clock_timestamp())
RETURNING id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, health_reason_code, health_checked_at, created_at, updated_at`

const changeStatusStatement = `UPDATE connector_accounts
SET status=$4, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5
RETURNING id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, health_reason_code, health_checked_at, created_at, updated_at`

const recordHealthStatement = `UPDATE connector_accounts
SET health_status=$4, health_reason_code=$5, health_checked_at=$6, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7
RETURNING id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, health_reason_code, health_checked_at, created_at, updated_at`

const bindSecretStatement = `UPDATE connector_accounts
SET secret_reference=$4, status='disabled', health_status='unknown', health_reason_code=NULL,
    health_checked_at=NULL, version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5
RETURNING id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, health_reason_code, health_checked_at, created_at, updated_at`

const changeCapabilitiesStatement = `UPDATE connector_accounts
SET version=version+1, updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4
RETURNING id, organization_id, workspace_id, provider, family, status, secret_reference,
  version, health_status, health_reason_code, health_checked_at, created_at, updated_at`

const currentCapabilitiesStatement = `SELECT capability,direction,risk_class,approval_required,enabled
FROM connector_account_capability_history
WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3
  AND account_version=(SELECT max(account_version) FROM connector_account_capability_history
    WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3)
ORDER BY capability`

const insertCapabilityRevisionStatement = `INSERT INTO connector_account_capability_history
  (organization_id,workspace_id,connector_account_id,account_version,capability,direction,risk_class,approval_required,enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`

type Repository struct{ database *sql.DB }

var _ sdk.AccountRepository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("connector account repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) CreateAccount(ctx context.Context, command sdk.AccountCreate, manifest sdk.Manifest) (sdk.Account, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return sdk.Account{}, err
	}
	if err := command.Validate(); err != nil {
		return sdk.Account{}, err
	}
	if err := manifest.Validate(); err != nil {
		return sdk.Account{}, err
	}
	bindingID := command.ConnectorID
	manifestID := manifest.ID
	if bindingID != manifestID {
		return sdk.Account{}, sdk.ErrAccountConnector
	}
	scope, err := tenancy.ParseScope(command.OrganizationID, command.WorkspaceID)
	if err != nil {
		return sdk.Account{}, tenancy.ErrInvalidScope
	}
	var account sdk.Account
	err = repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		account, err = scanAccount(tx.QueryRowContext(ctx, createAccountStatement,
			command.ID, command.OrganizationID, command.WorkspaceID, manifest.ID, string(manifest.Family), nullableSecret(command.SecretReference)))
		if err != nil {
			return err
		}
		return ValidatePersistedAccount(account, manifest)
	})
	return account, mapConflict(err)
}

// ListAccounts returns one bounded stable page ordered by account id. afterID
// is an opaque-cursor payload decoded by the API boundary, never tenant input.
func (repository *Repository) ListAccounts(ctx context.Context, organizationID, workspaceID, afterID string, limit int) ([]sdk.Account, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 101 || (afterID != "" && len(afterID) > 128) {
		return nil, sdk.ErrInvalidAccount
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		return nil, tenancy.ErrInvalidScope
	}
	accounts := make([]sdk.Account, 0, limit)
	err = repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, listAccountsStatement, organizationID, workspaceID, afterID, limit)
		if queryErr != nil {
			return fmt.Errorf("connector account repository: list: %w", queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			account, scanErr := scanAccount(rows)
			if scanErr != nil {
				return scanErr
			}
			accounts = append(accounts, account)
		}
		return rows.Err()
	})
	return accounts, err
}

func (repository *Repository) AccountByID(ctx context.Context, organizationID, workspaceID, accountID string) (sdk.Account, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return sdk.Account{}, err
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		return sdk.Account{}, tenancy.ErrInvalidScope
	}
	var account sdk.Account
	err = repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		account, err = scanAccount(tx.QueryRowContext(ctx, accountSelect+` WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, organizationID, workspaceID, accountID))
		return err
	})
	return account, err
}

func (repository *Repository) ChangeAccountStatus(ctx context.Context, command sdk.AccountStatusChange) (sdk.Account, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return sdk.Account{}, err
	}
	if err := command.Validate(); err != nil {
		return sdk.Account{}, err
	}
	scope, err := tenancy.ParseScope(command.OrganizationID, command.WorkspaceID)
	if err != nil {
		return sdk.Account{}, tenancy.ErrInvalidScope
	}
	var account sdk.Account
	err = repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		account, err = scanAccount(tx.QueryRowContext(ctx, changeStatusStatement, command.OrganizationID, command.WorkspaceID, command.AccountID, string(command.Status), command.ExpectedVersion))
		if errors.Is(err, sdk.ErrAccountNotFound) {
			return sdk.ErrAccountConflict
		}
		return err
	})
	return account, err
}

// BindSecret atomically attaches an opaque encrypted-secret reference while
// leaving the account disabled until a connector health validation succeeds.
func (repository *Repository) BindSecret(ctx context.Context, organizationID, workspaceID, accountID string, reference sdk.SecretReference, expectedVersion int64) (sdk.Account, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return sdk.Account{}, err
	}
	if !reference.Valid() || expectedVersion < 1 {
		return sdk.Account{}, sdk.ErrInvalidAccount
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		return sdk.Account{}, tenancy.ErrInvalidScope
	}
	var account sdk.Account
	err = repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		account, err = scanAccount(tx.QueryRowContext(ctx, bindSecretStatement, organizationID, workspaceID, accountID, string(reference), expectedVersion))
		if errors.Is(err, sdk.ErrAccountNotFound) {
			return sdk.ErrAccountConflict
		}
		return err
	})
	return account, err
}

func (repository *Repository) RecordAccountHealth(ctx context.Context, command sdk.AccountHealthUpdate) (sdk.Account, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return sdk.Account{}, err
	}
	if err := command.Validate(); err != nil {
		return sdk.Account{}, err
	}
	scope, err := tenancy.ParseScope(command.OrganizationID, command.WorkspaceID)
	if err != nil {
		return sdk.Account{}, tenancy.ErrInvalidScope
	}
	var account sdk.Account
	err = repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		account, err = scanAccount(tx.QueryRowContext(ctx, recordHealthStatement, command.OrganizationID, command.WorkspaceID, command.AccountID,
			string(command.Health.Status), nullableString(command.Health.ReasonCode), command.Health.CheckedAt.UTC(), command.ExpectedVersion))
		if errors.Is(err, sdk.ErrAccountNotFound) {
			return sdk.ErrAccountConflict
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO connector_health_history (organization_id,workspace_id,connector_account_id,status,category,reason_code,checked_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			command.OrganizationID, command.WorkspaceID, command.AccountID, string(command.Health.Status), healthCategory(command.Health), nullableString(command.Health.ReasonCode), command.Health.CheckedAt.UTC())
		return err
	})
	return account, err
}

// AccountCapabilities returns the latest complete capability snapshot. An
// empty result is valid and means default deny for every manifest capability.
func (repository *Repository) AccountCapabilities(ctx context.Context, scope tenancy.Scope, accountID string) ([]sdk.AccountCapabilitySetting, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return nil, err
	}
	if !scope.Valid() || accountID == "" || len(accountID) > 128 {
		return nil, sdk.ErrInvalidAccount
	}
	settings := make([]sdk.AccountCapabilitySetting, 0)
	err := repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, currentCapabilitiesStatement, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
		if err != nil {
			return fmt.Errorf("connector account repository: list capabilities: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var setting sdk.AccountCapabilitySetting
			if err = rows.Scan(&setting.Capability, &setting.Direction, &setting.Risk, &setting.ApprovalRequired, &setting.Enabled); err != nil {
				return fmt.Errorf("connector account repository: scan capability: %w", err)
			}
			definition, ok := sdk.CapabilityDefinitionFor(setting.Capability)
			if !ok || setting.Direction != definition.Direction || setting.Risk != definition.Risk || setting.ApprovalRequired != definition.ApprovalRequired {
				return sdk.ErrInvalidCapabilitySettings
			}
			settings = append(settings, setting)
		}
		return rows.Err()
	})
	return settings, err
}

// AccountCapabilitiesBulk returns the latest capability revision for each
// requested account in one tenant-scoped query. The integration-state center
// uses this boundary to avoid one database round-trip per card.
func (repository *Repository) AccountCapabilitiesBulk(ctx context.Context, scope tenancy.Scope, accountIDs []string) (map[string][]sdk.AccountCapabilitySetting, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return nil, err
	}
	if !scope.Valid() || len(accountIDs) > 101 {
		return nil, sdk.ErrInvalidAccount
	}
	result := make(map[string][]sdk.AccountCapabilitySetting, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(accountIDs))
	args := make([]any, 0, len(accountIDs)+2)
	args = append(args, scope.OrganizationID().String(), scope.WorkspaceID().String())
	for i, accountID := range accountIDs {
		if !bulkAccountIDPattern.MatchString(accountID) {
			return nil, sdk.ErrInvalidAccount
		}
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, accountID)
		result[accountID] = []sdk.AccountCapabilitySetting{}
	}
	query := `SELECT h.connector_account_id,h.capability,h.direction,h.risk_class,h.approval_required,h.enabled
FROM connector_account_capability_history h
WHERE h.organization_id=$1 AND h.workspace_id=$2
  AND h.connector_account_id IN (` + strings.Join(placeholders, ",") + `)
  AND h.account_version=(SELECT max(latest.account_version) FROM connector_account_capability_history latest
    WHERE latest.organization_id=$1 AND latest.workspace_id=$2 AND latest.connector_account_id=h.connector_account_id)
ORDER BY h.connector_account_id,h.capability`
	err := repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("connector account repository: list capabilities bulk: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var accountID string
			var setting sdk.AccountCapabilitySetting
			if err := rows.Scan(&accountID, &setting.Capability, &setting.Direction, &setting.Risk, &setting.ApprovalRequired, &setting.Enabled); err != nil {
				return fmt.Errorf("connector account repository: scan capability bulk: %w", err)
			}
			definition, ok := sdk.CapabilityDefinitionFor(setting.Capability)
			if !ok || setting.Direction != definition.Direction || setting.Risk != definition.Risk || setting.ApprovalRequired != definition.ApprovalRequired {
				return sdk.ErrInvalidCapabilitySettings
			}
			result[accountID] = append(result[accountID], setting)
		}
		return rows.Err()
	})
	return result, err
}

// ReplaceAccountCapabilities appends a complete manifest-bound snapshot and
// advances the account optimistic version in the same tenant-scoped transaction.
func (repository *Repository) ReplaceAccountCapabilities(ctx context.Context, scope tenancy.Scope, accountID string, expectedVersion int64, manifest sdk.Manifest, enabled []sdk.Capability) (sdk.Account, []sdk.AccountCapabilitySetting, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return sdk.Account{}, nil, err
	}
	if !scope.Valid() || accountID == "" || len(accountID) > 128 || expectedVersion < 1 {
		return sdk.Account{}, nil, sdk.ErrInvalidAccount
	}
	settings, err := sdk.BuildAccountCapabilitySettings(manifest, enabled)
	if err != nil {
		return sdk.Account{}, nil, err
	}
	var account sdk.Account
	err = repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		current, lookupErr := scanAccount(tx.QueryRowContext(ctx, accountSelect+` WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID))
		if lookupErr != nil {
			return lookupErr
		}
		if current.Version != expectedVersion || sdk.ValidateAccountAgainstManifest(current, manifest) != nil {
			return sdk.ErrAccountConflict
		}
		account, err = scanAccount(tx.QueryRowContext(ctx, changeCapabilitiesStatement, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, expectedVersion))
		if err != nil {
			return sdk.ErrAccountConflict
		}
		for _, setting := range settings {
			if _, err = tx.ExecContext(ctx, insertCapabilityRevisionStatement,
				scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, account.Version,
				string(setting.Capability), string(setting.Direction), string(setting.Risk), setting.ApprovalRequired, setting.Enabled); err != nil {
				return fmt.Errorf("connector account repository: append capability revision: %w", err)
			}
		}
		return nil
	})
	if errors.Is(err, sdk.ErrAccountNotFound) {
		err = sdk.ErrAccountConflict
	}
	return account, settings, err
}

func ValidatePersistedAccount(account sdk.Account, manifest sdk.Manifest) error {
	return sdk.ValidateAccountAgainstManifest(account, manifest)
}

func validateRepositoryCall(ctx context.Context, repository *Repository) error {
	if ctx == nil {
		return errors.New("connector account repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.database == nil {
		return errors.New("connector account repository: repository is not initialized")
	}
	return nil
}

func (repository *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, operation func(*sql.Tx) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("connector account repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("connector account repository: scope: %w", err)
	}
	if org != scope.OrganizationID().String() || ws != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connector account repository: commit: %w", err)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanAccount(row scanner) (sdk.Account, error) {
	var account sdk.Account
	var family, status, healthStatus string
	var secret, healthReason sql.NullString
	var healthChecked sql.NullTime
	if err := row.Scan(&account.ID, &account.OrganizationID, &account.WorkspaceID, &account.ConnectorID, &family, &status, &secret,
		&account.Version, &healthStatus, &healthReason, &healthChecked, &account.CreatedAt, &account.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sdk.Account{}, sdk.ErrAccountNotFound
		}
		return sdk.Account{}, fmt.Errorf("connector account repository: scan: %w", err)
	}
	account.Family = sdk.Family(family)
	account.Status = sdk.AccountStatus(status)
	if secret.Valid {
		account.SecretReference = sdk.SecretReference(secret.String)
	}
	account.Health.Status = sdk.HealthStatus(healthStatus)
	if healthReason.Valid {
		account.Health.ReasonCode = healthReason.String
	}
	if healthChecked.Valid {
		account.Health.CheckedAt = healthChecked.Time.UTC()
	}
	account.CreatedAt = account.CreatedAt.UTC()
	account.UpdatedAt = account.UpdatedAt.UTC()
	if err := account.Validate(); err != nil {
		return sdk.Account{}, err
	}
	return account, nil
}

func nullableSecret(reference sdk.SecretReference) any {
	if reference == "" {
		return nil
	}
	return string(reference)
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mapConflict(err error) error {
	if errors.Is(err, sdk.ErrAccountNotFound) {
		return sdk.ErrAccountConflict
	}
	return err
}
