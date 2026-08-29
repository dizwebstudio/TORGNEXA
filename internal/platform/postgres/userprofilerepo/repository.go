// Package userprofilerepo persists the tenant-scoped current-user profile.
package userprofilerepo

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const profileColumns = `organization_id,workspace_id,subject_ref,username,email,given_name,family_name,birthdate,job_title,department,phone_number,picture_upload_id,version,created_at,updated_at`

// Repository is the PostgreSQL adapter for current-user profiles. Every query
// is executed inside a transaction-local tenant scope and no raw OIDC subject
// is persisted; callers provide the one-way SubjectRef from the auth layer.
type Repository struct {
	database *sql.DB
}

var _ interface {
	Ensure(context.Context, tenancy.Scope, userprofile.Identity) (userprofile.Profile, error)
	Get(context.Context, tenancy.Scope, string) (userprofile.Profile, error)
	Update(context.Context, tenancy.Scope, userprofile.Update) (userprofile.Profile, error)
} = (*Repository)(nil)

// New constructs a profile repository over the application database pool.
func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("user profile repository: database is required")
	}
	return &Repository{database: database}, nil
}

// Ensure returns the current profile and creates its first provider-derived
// projection when this subject has not used the workspace before. Existing
// editable fields are never overwritten by later OIDC claims.
func (repository *Repository) Ensure(ctx context.Context, scope tenancy.Scope, identity userprofile.Identity) (userprofile.Profile, error) {
	if ctx == nil || !scope.Valid() || !identity.Valid() {
		return userprofile.Profile{}, userprofile.ErrInvalid
	}
	if repository == nil || repository.database == nil {
		return userprofile.Profile{}, errors.New("user profile repository: repository is not initialized")
	}
	var profile userprofile.Profile
	err := repository.withTransaction(ctx, scope, false, func(tx *sql.Tx) error {
		if err := applyScope(ctx, tx, scope); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO user_profiles (`+profileColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULL,1,clock_timestamp(),clock_timestamp()) ON CONFLICT (organization_id,workspace_id,subject_ref) DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), identity.SubjectRef, identity.Username, identity.Email, identity.GivenName, identity.FamilyName, identity.Birthdate, identity.JobTitle, identity.Department, identity.PhoneNumber)
		if err != nil {
			return fmt.Errorf("insert profile seed: %w", err)
		}
		row := tx.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM user_profiles WHERE organization_id=$1 AND workspace_id=$2 AND subject_ref=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), identity.SubjectRef)
		var scanErr error
		profile, scanErr = scanProfile(row)
		if scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return userprofile.ErrNotFound
			}
			return fmt.Errorf("read profile: %w", scanErr)
		}
		if !profile.Valid() || profile.OrganizationID != scope.OrganizationID() || profile.WorkspaceID != scope.WorkspaceID() || profile.SubjectRef != identity.SubjectRef {
			return userprofile.ErrInvalid
		}
		return nil
	})
	return profile, err
}

// Get returns a tenant-scoped profile without creating or changing it. It is
// used by administrative workspace views after the membership boundary has
// resolved the opaque subject reference.
func (repository *Repository) Get(ctx context.Context, scope tenancy.Scope, subjectRef string) (userprofile.Profile, error) {
	if ctx == nil || !scope.Valid() || len(subjectRef) != 64 {
		return userprofile.Profile{}, userprofile.ErrInvalid
	}
	if _, err := hex.DecodeString(subjectRef); err != nil {
		return userprofile.Profile{}, userprofile.ErrInvalid
	}
	if repository == nil || repository.database == nil {
		return userprofile.Profile{}, errors.New("user profile repository: repository is not initialized")
	}
	var profile userprofile.Profile
	err := repository.withTransaction(ctx, scope, true, func(tx *sql.Tx) error {
		if err := applyScope(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		profile, err = scanProfile(tx.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM user_profiles WHERE organization_id=$1 AND workspace_id=$2 AND subject_ref=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), subjectRef))
		if errors.Is(err, sql.ErrNoRows) {
			return userprofile.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read profile: %w", err)
		}
		if !profile.Valid() || profile.OrganizationID != scope.OrganizationID() || profile.WorkspaceID != scope.WorkspaceID() || profile.SubjectRef != subjectRef {
			return userprofile.ErrInvalid
		}
		return nil
	})
	return profile, err
}

// Update applies an optimistic, idempotent update to the current user's
// editable fields. The caller must verify any picture upload before passing
// its released upload ID here.
func (repository *Repository) Update(ctx context.Context, scope tenancy.Scope, update userprofile.Update) (userprofile.Profile, error) {
	if ctx == nil || !scope.Valid() || !update.Valid() {
		return userprofile.Profile{}, userprofile.ErrInvalid
	}
	if repository == nil || repository.database == nil {
		return userprofile.Profile{}, errors.New("user profile repository: repository is not initialized")
	}
	var profile userprofile.Profile
	err := repository.withTransaction(ctx, scope, false, func(tx *sql.Tx) error {
		if err := applyScope(ctx, tx, scope); err != nil {
			return err
		}
		current, mutationKey, mutationHash, err := scanProfileWithMutation(tx.QueryRowContext(ctx, `SELECT `+profileColumns+`,last_mutation_key,last_mutation_hash FROM user_profiles WHERE organization_id=$1 AND workspace_id=$2 AND subject_ref=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), update.SubjectRef))
		if errors.Is(err, sql.ErrNoRows) {
			return userprofile.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock profile: %w", err)
		}
		if !current.Valid() || current.OrganizationID != scope.OrganizationID() || current.WorkspaceID != scope.WorkspaceID() || current.SubjectRef != update.SubjectRef {
			return userprofile.ErrInvalid
		}
		if mutationKey == update.MutationKey {
			if mutationHash != update.MutationHash {
				return userprofile.ErrConflict
			}
			profile = current
			return nil
		}
		if current.Version != update.ExpectedVersion {
			return userprofile.ErrConflict
		}
		profile, err = scanProfile(tx.QueryRowContext(ctx, `UPDATE user_profiles SET given_name=$4,family_name=$5,birthdate=$6,job_title=$7,department=$8,phone_number=$9,picture_upload_id=NULLIF($10,''),version=version+1,last_mutation_key=$11,last_mutation_hash=$12,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND subject_ref=$3 AND version=$13 RETURNING `+profileColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), update.SubjectRef, update.GivenName, update.FamilyName, update.Birthdate, update.JobTitle, update.Department, update.PhoneNumber, update.PictureUploadID, update.MutationKey, update.MutationHash, update.ExpectedVersion))
		if errors.Is(err, sql.ErrNoRows) {
			return userprofile.ErrConflict
		}
		if err != nil {
			return fmt.Errorf("update profile: %w", err)
		}
		if !profile.Valid() || profile.OrganizationID != scope.OrganizationID() || profile.WorkspaceID != scope.WorkspaceID() || profile.SubjectRef != update.SubjectRef {
			return userprofile.ErrInvalid
		}
		return nil
	})
	return profile, err
}

func applyScope(ctx context.Context, tx *sql.Tx, scope tenancy.Scope) error {
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("apply tenant scope: %w", err)
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return fmt.Errorf("apply tenant scope: %w", tenancy.ErrInvalidScope)
	}
	return nil
}

func (repository *Repository) withTransaction(ctx context.Context, scope tenancy.Scope, readOnly bool, operation func(*sql.Tx) error) error {
	if ctx == nil || !scope.Valid() || repository == nil || repository.database == nil || operation == nil {
		return userprofile.ErrInvalid
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin profile transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit profile transaction: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanProfile(row rowScanner) (userprofile.Profile, error) {
	var profile userprofile.Profile
	var organizationID, workspaceID string
	var pictureUploadID sql.NullString
	if err := row.Scan(&organizationID, &workspaceID, &profile.SubjectRef, &profile.Username, &profile.Email, &profile.GivenName, &profile.FamilyName, &profile.Birthdate, &profile.JobTitle, &profile.Department, &profile.PhoneNumber, &pictureUploadID, &profile.Version, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
		return userprofile.Profile{}, err
	}
	var err error
	profile.OrganizationID, err = tenancy.ParseOrganizationID(organizationID)
	if err != nil {
		return userprofile.Profile{}, userprofile.ErrInvalid
	}
	profile.WorkspaceID, err = tenancy.ParseWorkspaceID(workspaceID)
	if err != nil {
		return userprofile.Profile{}, userprofile.ErrInvalid
	}
	if pictureUploadID.Valid {
		profile.PictureUploadID = pictureUploadID.String
	}
	profile.CreatedAt, profile.UpdatedAt = profile.CreatedAt.UTC(), profile.UpdatedAt.UTC()
	return profile, nil
}

func scanProfileWithMutation(row rowScanner) (userprofile.Profile, string, string, error) {
	var profile userprofile.Profile
	var mutationKey, mutationHash string
	var organizationID, workspaceID string
	var pictureUploadID sql.NullString
	if err := row.Scan(&organizationID, &workspaceID, &profile.SubjectRef, &profile.Username, &profile.Email, &profile.GivenName, &profile.FamilyName, &profile.Birthdate, &profile.JobTitle, &profile.Department, &profile.PhoneNumber, &pictureUploadID, &profile.Version, &profile.CreatedAt, &profile.UpdatedAt, &mutationKey, &mutationHash); err != nil {
		return userprofile.Profile{}, "", "", err
	}
	var err error
	profile.OrganizationID, err = tenancy.ParseOrganizationID(organizationID)
	if err != nil {
		return userprofile.Profile{}, "", "", userprofile.ErrInvalid
	}
	profile.WorkspaceID, err = tenancy.ParseWorkspaceID(workspaceID)
	if err != nil {
		return userprofile.Profile{}, "", "", userprofile.ErrInvalid
	}
	if pictureUploadID.Valid {
		profile.PictureUploadID = pictureUploadID.String
	}
	profile.CreatedAt, profile.UpdatedAt = profile.CreatedAt.UTC(), profile.UpdatedAt.UTC()
	return profile, mutationKey, mutationHash, nil
}
