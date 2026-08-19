package securitysettingsrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

// ListProviders returns the bounded current projection without secret handles.
func (r *Repository) ListProviders(ctx context.Context, scope tenancy.Scope, limit int) ([]securitysettings.ProviderConfiguration, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 100 {
		return nil, securitysettings.ErrIdentityInvalid
	}
	items := make([]securitysettings.ProviderConfiguration, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, identityProjectionSQL+` WHERE h.organization_id=$1 AND h.workspace_id=$2 ORDER BY h.provider_id LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanIdentity(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list identity providers: %w", err)
	}
	return items, nil
}

// Provider returns one current provider projection.
func (r *Repository) Provider(ctx context.Context, scope tenancy.Scope, idpID string) (securitysettings.ProviderConfiguration, error) {
	if err := validate(ctx, r, scope); err != nil || idpID == "" {
		return securitysettings.ProviderConfiguration{}, securitysettings.ErrIdentityInvalid
	}
	var item securitysettings.ProviderConfiguration
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		item, err = scanIdentity(tx.QueryRowContext(ctx, identityProjectionSQL+` WHERE h.organization_id=$1 AND h.workspace_id=$2 AND h.provider_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return item, securitysettings.ErrIdentityNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read identity provider: %w", err)
	}
	return item, nil
}

// SaveProvider appends a draft revision and advances only the current pointer.
func (r *Repository) SaveProvider(ctx context.Context, scope tenancy.Scope, draft securitysettings.ProviderDraft) (securitysettings.ProviderConfiguration, error) {
	if err := validate(ctx, r, scope); err != nil || draft.ID == "" || draft.CorrelationID == "" || draft.CreatedAt.IsZero() {
		return securitysettings.ProviderConfiguration{}, securitysettings.ErrIdentityInvalid
	}
	var output securitysettings.ProviderConfiguration
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var current, version uint64
		var secret sql.NullString
		var lastCorrelation string
		err := tx.QueryRowContext(ctx, `SELECT h.current_revision,h.version,r.client_secret_reference,h.last_correlation_id FROM settings_identity_providers h JOIN settings_identity_provider_revisions r ON r.organization_id=h.organization_id AND r.workspace_id=h.workspace_id AND r.provider_id=h.provider_id AND r.revision=h.current_revision WHERE h.organization_id=$1 AND h.workspace_id=$2 AND h.provider_id=$3 FOR UPDATE OF h`, scope.OrganizationID().String(), scope.WorkspaceID().String(), draft.ID).Scan(&current, &version, &secret, &lastCorrelation)
		if errors.Is(err, sql.ErrNoRows) {
			if draft.ExpectedVersion != 0 {
				return securitysettings.ErrIdentityConflict
			}
			current, version = 1, 1
			if _, err = tx.ExecContext(ctx, `INSERT INTO settings_identity_providers(organization_id,workspace_id,provider_id,current_revision,enabled,version,last_correlation_id,updated_at) VALUES($1,$2,$3,1,false,1,$4,$5)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), draft.ID, draft.CorrelationID, draft.CreatedAt.UTC()); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if lastCorrelation == draft.CorrelationID {
				output, err = loadIdentityTx(ctx, tx, scope, draft.ID)
				output.Replayed = true
				return err
			}
			if version != draft.ExpectedVersion {
				return securitysettings.ErrIdentityConflict
			}
			current++
			version++
		}
		secretReference := draft.SecretReference
		if secretReference == "" && secret.Valid {
			secretReference = secret.String
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings_identity_provider_revisions(organization_id,workspace_id,provider_id,revision,protocol,display_name,issuer_url,client_id,callback_url,client_secret_reference,correlation_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), draft.ID, current, draft.Protocol, draft.DisplayName, draft.IssuerURL, draft.ClientID, draft.CallbackURL, secretReference, draft.CorrelationID, draft.CreatedAt.UTC()); err != nil {
			return err
		}
		if current > 1 {
			result, err := tx.ExecContext(ctx, `UPDATE settings_identity_providers SET current_revision=$4,version=version+1,last_correlation_id=$5,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND version=$7`, scope.OrganizationID().String(), scope.WorkspaceID().String(), draft.ID, current, draft.CorrelationID, draft.CreatedAt.UTC(), draft.ExpectedVersion)
			if err != nil {
				return err
			}
			if rows, _ := result.RowsAffected(); rows != 1 {
				return securitysettings.ErrIdentityConflict
			}
		}
		var loadErr error
		output, loadErr = loadIdentityTx(ctx, tx, scope, draft.ID)
		return loadErr
	})
	if err != nil {
		return output, err
	}
	return output, nil
}

// RecordProviderValidation appends bounded discovery evidence.
func (r *Repository) RecordProviderValidation(ctx context.Context, scope tenancy.Scope, value securitysettings.ProviderValidation) (securitysettings.ProviderConfiguration, error) {
	if err := validate(ctx, r, scope); err != nil || value.ID == "" || value.IdentityID == "" || value.Revision == 0 || value.CheckedAt.IsZero() || value.ExpectedVersion == 0 || value.CorrelationID == "" {
		return securitysettings.ProviderConfiguration{}, securitysettings.ErrIdentityInvalid
	}
	var output securitysettings.ProviderConfiguration
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var locked int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM settings_identity_providers WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.IdentityID).Scan(&locked); err != nil {
			return err
		}
		current, err := loadIdentityTx(ctx, tx, scope, value.IdentityID)
		if err != nil {
			return err
		}
		if current.LastCorrelationID == value.CorrelationID {
			current.Replayed = true
			output = current
			return nil
		}
		if current.Revision != value.Revision || current.Version != value.ExpectedVersion {
			return securitysettings.ErrIdentityConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO settings_identity_provider_validations(id,organization_id,workspace_id,provider_id,revision,status,reason_code,metadata_digest,issuer_url,authorization_url,token_url,jwks_url,correlation_id,checked_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,$14)`, value.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.IdentityID, value.Revision, value.Status, value.ReasonCode, value.MetadataDigest, value.Issuer, value.AuthorizationURL, value.TokenURL, value.JWKSURL, value.CorrelationID, value.CheckedAt.UTC())
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE settings_identity_providers SET version=version+1,last_correlation_id=$4,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND version=$6`, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.IdentityID, value.CorrelationID, value.CheckedAt.UTC(), value.ExpectedVersion); err != nil {
			return err
		}
		output, err = loadIdentityTx(ctx, tx, scope, value.IdentityID)
		return err
	})
	return output, err
}

func (r *Repository) ActivateProvider(ctx context.Context, scope tenancy.Scope, idpID string, expected uint64, correlation string, at time.Time) (securitysettings.ProviderConfiguration, error) {
	return r.changeIdentityHead(ctx, scope, idpID, expected, correlation, func(tx *sql.Tx, current securitysettings.ProviderConfiguration) error {
		if current.ValidationStatus != "valid" {
			return securitysettings.ErrIdentityNotValidated
		}
		_, err := tx.ExecContext(ctx, `UPDATE settings_identity_providers SET active_revision=current_revision,enabled=true,version=version+1,last_correlation_id=$4,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND version=$6`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID, correlation, at.UTC(), expected)
		return err
	})
}

// RollbackProvider copies a previously validated revision into a new immutable
// revision and activates it atomically, preserving monotonic history.
func (r *Repository) RollbackProvider(ctx context.Context, scope tenancy.Scope, idpID string, target, expected uint64, validationID, correlation string, at time.Time) (securitysettings.ProviderConfiguration, error) {
	return r.changeIdentityHead(ctx, scope, idpID, expected, correlation, func(tx *sql.Tx, current securitysettings.ProviderConfiguration) error {
		if target == 0 || target >= current.Revision || validationID == "" {
			return securitysettings.ErrIdentityInvalid
		}
		var revision securitysettings.ProviderRevision
		var secret sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT protocol,display_name,issuer_url,client_id,callback_url,client_secret_reference FROM settings_identity_provider_revisions WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND revision=$4 AND EXISTS (SELECT 1 FROM settings_identity_provider_validations v WHERE v.organization_id=$1 AND v.workspace_id=$2 AND v.provider_id=$3 AND v.revision=$4 AND v.status='valid')`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID, target).Scan(&revision.Protocol, &revision.DisplayName, &revision.IssuerURL, &revision.ClientID, &revision.CallbackURL, &secret); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return securitysettings.ErrIdentityNotValidated
			}
			return err
		}
		next := current.Revision + 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings_identity_provider_revisions(organization_id,workspace_id,provider_id,revision,protocol,display_name,issuer_url,client_id,callback_url,client_secret_reference,correlation_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID, next, revision.Protocol, revision.DisplayName, revision.IssuerURL, revision.ClientID, revision.CallbackURL, secret, correlation, at.UTC()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings_identity_provider_validations(id,organization_id,workspace_id,provider_id,revision,status,reason_code,metadata_digest,issuer_url,authorization_url,token_url,jwks_url,correlation_id,checked_at) SELECT $5,organization_id,workspace_id,provider_id,$6,'valid','validated',metadata_digest,issuer_url,authorization_url,token_url,jwks_url,$7,$8 FROM settings_identity_provider_validations WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND revision=$4 AND status='valid' ORDER BY checked_at DESC,id DESC LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID, target, validationID, next, correlation, at.UTC()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE settings_identity_providers SET current_revision=$4,active_revision=$4,enabled=true,version=version+1,last_correlation_id=$5,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND version=$7`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID, next, correlation, at.UTC(), expected)
		return err
	})
}

func (r *Repository) DisableProvider(ctx context.Context, scope tenancy.Scope, idpID string, expected uint64, correlation string, at time.Time) (securitysettings.ProviderConfiguration, error) {
	return r.changeIdentityHead(ctx, scope, idpID, expected, correlation, func(tx *sql.Tx, _ securitysettings.ProviderConfiguration) error {
		_, err := tx.ExecContext(ctx, `UPDATE settings_identity_providers SET enabled=false,version=version+1,last_correlation_id=$4,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 AND version=$6`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID, correlation, at.UTC(), expected)
		return err
	})
}

func (r *Repository) changeIdentityHead(ctx context.Context, scope tenancy.Scope, idpID string, expected uint64, correlation string, change func(*sql.Tx, securitysettings.ProviderConfiguration) error) (securitysettings.ProviderConfiguration, error) {
	if err := validate(ctx, r, scope); err != nil || idpID == "" || expected == 0 || correlation == "" || change == nil {
		return securitysettings.ProviderConfiguration{}, securitysettings.ErrIdentityInvalid
	}
	var output securitysettings.ProviderConfiguration
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var locked int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM settings_identity_providers WHERE organization_id=$1 AND workspace_id=$2 AND provider_id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID).Scan(&locked); err != nil {
			return err
		}
		current, err := loadIdentityTx(ctx, tx, scope, idpID)
		if err != nil {
			return err
		}
		if current.LastCorrelationID == correlation {
			current.Replayed = true
			output = current
			return nil
		}
		if current.Version != expected {
			return securitysettings.ErrIdentityConflict
		}
		if err = change(tx, current); err != nil {
			return err
		}
		output, err = loadIdentityTx(ctx, tx, scope, idpID)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return output, securitysettings.ErrIdentityNotFound
	}
	return output, err
}

const identityProjectionSQL = `SELECT h.provider_id,r.protocol,r.display_name,r.issuer_url,r.client_id,r.callback_url,(r.client_secret_reference IS NOT NULL),r.revision,r.created_at,h.version,COALESCE(h.active_revision,0),h.enabled,COALESCE(v.status,'not_validated'),COALESCE(v.reason_code,'not_validated'),v.checked_at,h.updated_at,h.last_correlation_id
FROM settings_identity_providers h
JOIN settings_identity_provider_revisions r ON r.organization_id=h.organization_id AND r.workspace_id=h.workspace_id AND r.provider_id=h.provider_id AND r.revision=h.current_revision
LEFT JOIN LATERAL (SELECT status,reason_code,checked_at FROM settings_identity_provider_validations WHERE organization_id=h.organization_id AND workspace_id=h.workspace_id AND provider_id=h.provider_id AND revision=h.current_revision ORDER BY checked_at DESC,id DESC LIMIT 1) v ON true`

type identityScanner interface{ Scan(...any) error }

func scanIdentity(row identityScanner) (securitysettings.ProviderConfiguration, error) {
	var item securitysettings.ProviderConfiguration
	var hasSecret bool
	var checked sql.NullTime
	err := row.Scan(&item.ID, &item.Protocol, &item.DisplayName, &item.IssuerURL, &item.ClientID, &item.CallbackURL, &hasSecret, &item.Revision, &item.CreatedAt, &item.Version, &item.ActiveRevision, &item.Enabled, &item.ValidationStatus, &item.ValidationReason, &checked, &item.UpdatedAt, &item.LastCorrelationID)
	if err != nil {
		return item, err
	}
	if hasSecret {
		item.SecretReference = "present"
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if checked.Valid {
		value := checked.Time.UTC()
		item.ValidatedAt = &value
	}
	return item, nil
}

func loadIdentityTx(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, idpID string) (securitysettings.ProviderConfiguration, error) {
	return scanIdentity(tx.QueryRowContext(ctx, identityProjectionSQL+` WHERE h.organization_id=$1 AND h.workspace_id=$2 AND h.provider_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), idpID))
}

var _ securitysettings.IdentityProviderStore = (*Repository)(nil)
