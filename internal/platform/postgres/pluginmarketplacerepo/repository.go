// Package pluginmarketplacerepo persists immutable Task-078 marketplace governance evidence.
package pluginmarketplacerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/pluginmarketplace"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("plugin marketplace repository: database required")
	}
	return &Repository{db: db}, nil
}

// ListVisible returns reviewed public listings plus private listings visible in
// the authenticated tenant. Globally revoked artifacts and publisher keys are
// excluded before metadata reaches an installation UI.
func (r *Repository) ListVisible(ctx context.Context, scope tenancy.Scope, limit int) ([]pluginmarketplace.ListingView, error) {
	if invalid(r, ctx, scope) || limit < 1 || limit > 201 {
		return nil, pluginmarketplace.ErrInvalid
	}
	views := make([]pluginmarketplace.ListingView, 0, limit)
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT security_descriptor,review_evidence,license_expression,security_contact,published_at,private_organization_id,private_workspace_id FROM (
SELECT v.security_descriptor,v.review_evidence,v.license_expression,v.security_contact,v.published_at,''::text private_organization_id,''::text private_workspace_id,v.plugin_id,v.plugin_version,v.artifact_sha256
FROM plugin_marketplace_versions v
WHERE NOT EXISTS (SELECT 1 FROM plugin_marketplace_revocations r WHERE (r.kind='artifact' AND r.plugin_id=v.plugin_id AND r.artifact_sha256=v.artifact_sha256) OR (r.kind='publisher_key' AND r.publisher_id=v.publisher_id AND r.publisher_key_id=v.publisher_key_id))
UNION ALL
SELECT p.security_descriptor,p.review_evidence,p.license_expression,p.security_contact,p.published_at,p.organization_id,p.workspace_id,p.plugin_id,p.plugin_version,p.artifact_sha256
FROM plugin_private_versions p
WHERE NOT EXISTS (SELECT 1 FROM plugin_marketplace_revocations r WHERE (r.kind='artifact' AND r.plugin_id=p.plugin_id AND r.artifact_sha256=p.artifact_sha256) OR (r.kind='publisher_key' AND r.publisher_id=p.publisher_id AND r.publisher_key_id=p.publisher_key_id))
) visible ORDER BY published_at DESC,plugin_id,plugin_version,artifact_sha256 LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var descriptor, review []byte
			var listing pluginmarketplace.Listing
			if err := rows.Scan(&descriptor, &review, &listing.LicenseExpression, &listing.SecurityContact, &listing.PublishedAt, &listing.PrivateOrganizationID, &listing.PrivateWorkspaceID); err != nil {
				return err
			}
			if json.Unmarshal(descriptor, &listing.Descriptor) != nil || json.Unmarshal(review, &listing.Review) != nil {
				return pluginmarketplace.ErrInvalid
			}
			listing.PublishedAt = listing.PublishedAt.UTC()
			listing.Review.ReviewedAt = listing.Review.ReviewedAt.UTC()
			view, err := listing.View()
			if err != nil {
				return err
			}
			views = append(views, view)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("plugin marketplace repository: list visible: %w", err)
	}
	return views, nil
}

// PublishPublic records one already-reviewed public marketplace version. Rows are
// immutable; correction or revocation is represented by new evidence, never UPDATE.
func (r *Repository) PublishPublic(ctx context.Context, listing pluginmarketplace.Listing) error {
	if r == nil || r.db == nil || ctx == nil || listing.Validate() != nil || listing.Descriptor.Trust == pluginsecurity.TrustPrivate {
		return pluginmarketplace.ErrInvalid
	}
	descriptor, review, err := marshalListing(listing)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO plugin_marketplace_versions(plugin_id,plugin_version,artifact_sha256,publisher_id,publisher_key_id,publisher_key_fingerprint_sha256,trust,license_expression,security_contact,security_descriptor,review_evidence,published_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, listing.Descriptor.Manifest.ID, listing.Descriptor.Manifest.Version, listing.Descriptor.Artifact.SHA256, listing.Descriptor.Artifact.PublisherID, listing.Descriptor.Artifact.KeyID, listing.Descriptor.Artifact.KeyFingerprint, string(listing.Descriptor.Trust), listing.LicenseExpression, listing.SecurityContact, descriptor, review, listing.PublishedAt)
	if err != nil {
		return fmt.Errorf("plugin marketplace repository: publish public: %w", err)
	}
	return nil
}

func (r *Repository) PublishPrivate(ctx context.Context, scope tenancy.Scope, listing pluginmarketplace.Listing) error {
	if invalid(r, ctx, scope) || listing.Validate() != nil || listing.Descriptor.Trust != pluginsecurity.TrustPrivate || listing.PrivateOrganizationID != scope.OrganizationID().String() || listing.PrivateWorkspaceID != scope.WorkspaceID().String() {
		return pluginmarketplace.ErrInvalid
	}
	descriptor, review, err := marshalListing(listing)
	if err != nil {
		return err
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO plugin_private_versions(organization_id,workspace_id,plugin_id,plugin_version,artifact_sha256,publisher_id,publisher_key_id,publisher_key_fingerprint_sha256,trust,license_expression,security_contact,security_descriptor,review_evidence,published_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,'private',$9,$10,$11,$12,$13)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), listing.Descriptor.Manifest.ID, listing.Descriptor.Manifest.Version, listing.Descriptor.Artifact.SHA256, listing.Descriptor.Artifact.PublisherID, listing.Descriptor.Artifact.KeyID, listing.Descriptor.Artifact.KeyFingerprint, listing.LicenseExpression, listing.SecurityContact, descriptor, review, listing.PublishedAt)
		return err
	})
}

func (r *Repository) RecordConsent(ctx context.Context, consent pluginmarketplace.Consent, listing pluginmarketplace.Listing) error {
	if r == nil || r.db == nil || ctx == nil || consent.Validate(listing) != nil {
		return pluginmarketplace.ErrInvalid
	}
	raw, err := json.Marshal(consent.Grant)
	if err != nil {
		return pluginmarketplace.ErrInvalid
	}
	return r.write(ctx, consent.Scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO plugin_marketplace_consents(organization_id,workspace_id,consent_id,plugin_id,plugin_version,artifact_sha256,permission_grant,actor_id,granted_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, consent.Scope.OrganizationID().String(), consent.Scope.WorkspaceID().String(), consent.ID, consent.Grant.ExtensionID, consent.Grant.ExtensionVersion, consent.Grant.ArtifactSHA256, raw, consent.ActorID, consent.GrantedAt)
		return err
	})
}

// RecordRevocation stores global artifact/key revocations or tenant-scoped
// installation revocations. All revocation evidence is append-only.
func (r *Repository) RecordRevocation(ctx context.Context, revocation pluginmarketplace.Revocation) error {
	if r == nil || r.db == nil || ctx == nil || revocation.Validate() != nil {
		return pluginmarketplace.ErrInvalid
	}
	switch revocation.Kind {
	case pluginmarketplace.RevokeArtifact:
		_, err := r.db.ExecContext(ctx, `INSERT INTO plugin_marketplace_revocations(revocation_id,kind,plugin_id,artifact_sha256,actor_id,reason,revoked_at) VALUES($1,'artifact',$2,$3,$4,$5,$6)`, revocation.ID, revocation.ExtensionID, revocation.ArtifactSHA256, revocation.ActorID, revocation.Reason, revocation.RevokedAt)
		return normalize("record artifact revocation", err)
	case pluginmarketplace.RevokePublisherKey:
		_, err := r.db.ExecContext(ctx, `INSERT INTO plugin_marketplace_revocations(revocation_id,kind,publisher_id,publisher_key_id,actor_id,reason,revoked_at) VALUES($1,'publisher_key',$2,$3,$4,$5,$6)`, revocation.ID, revocation.PublisherID, revocation.PublisherKeyID, revocation.ActorID, revocation.Reason, revocation.RevokedAt)
		return normalize("record key revocation", err)
	case pluginmarketplace.RevokeInstallation:
		scope, err := tenancy.ParseScope(revocation.OrganizationID, revocation.WorkspaceID)
		if err != nil {
			return pluginmarketplace.ErrInvalid
		}
		return r.write(ctx, scope, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO plugin_installation_revocations(organization_id,workspace_id,revocation_id,consent_id,actor_id,reason,revoked_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, revocation.OrganizationID, revocation.WorkspaceID, revocation.ID, revocation.ConsentID, revocation.ActorID, revocation.Reason, revocation.RevokedAt)
			return err
		})
	default:
		return pluginmarketplace.ErrInvalid
	}
}

// Revocations resolves all global and tenant revocations relevant to one exact
// listing/consent. Callers pass the resulting set to pluginmarketplace.Admit.
func (r *Repository) Revocations(ctx context.Context, listing pluginmarketplace.Listing, consent pluginmarketplace.Consent) (pluginmarketplace.RevocationSet, error) {
	if r == nil || r.db == nil || ctx == nil || consent.Validate(listing) != nil {
		return nil, pluginmarketplace.ErrInvalid
	}
	out := pluginmarketplace.RevocationSet{}
	rows, err := r.db.QueryContext(ctx, `SELECT revocation_id,kind,COALESCE(plugin_id,''),COALESCE(artifact_sha256,''),COALESCE(publisher_id,''),COALESCE(publisher_key_id,''),actor_id,reason,revoked_at
FROM plugin_marketplace_revocations WHERE (kind='artifact' AND plugin_id=$1 AND artifact_sha256=$2) OR (kind='publisher_key' AND publisher_id=$3 AND publisher_key_id=$4)
ORDER BY revoked_at,revocation_id`, listing.Descriptor.Manifest.ID, listing.Descriptor.Artifact.SHA256, listing.Descriptor.Artifact.PublisherID, listing.Descriptor.Artifact.KeyID)
	if err != nil {
		return nil, fmt.Errorf("plugin marketplace repository: global revocations: %w", err)
	}
	for rows.Next() {
		var item pluginmarketplace.Revocation
		if err := rows.Scan(&item.ID, &item.Kind, &item.ExtensionID, &item.ArtifactSHA256, &item.PublisherID, &item.PublisherKeyID, &item.ActorID, &item.Reason, &item.RevokedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	err = r.read(ctx, consent.Scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT revocation_id,consent_id,actor_id,reason,revoked_at FROM plugin_installation_revocations WHERE organization_id=$1 AND workspace_id=$2 AND consent_id=$3 ORDER BY revoked_at,revocation_id`, consent.Scope.OrganizationID().String(), consent.Scope.WorkspaceID().String(), consent.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item := pluginmarketplace.Revocation{Kind: pluginmarketplace.RevokeInstallation, OrganizationID: consent.Scope.OrganizationID().String(), WorkspaceID: consent.Scope.WorkspaceID().String()}
			if err := rows.Scan(&item.ID, &item.ConsentID, &item.ActorID, &item.Reason, &item.RevokedAt); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func marshalListing(listing pluginmarketplace.Listing) ([]byte, []byte, error) {
	descriptor, err := json.Marshal(listing.Descriptor)
	if err != nil {
		return nil, nil, pluginmarketplace.ErrInvalid
	}
	review, err := json.Marshal(listing.Review)
	if err != nil {
		return nil, nil, pluginmarketplace.ErrInvalid
	}
	return descriptor, review, nil
}

func invalid(r *Repository, ctx context.Context, scope tenancy.Scope) bool {
	return r == nil || r.db == nil || ctx == nil || !scope.Valid()
}

func (r *Repository) write(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if invalid(r, ctx, scope) || fn == nil {
		return pluginmarketplace.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("plugin marketplace repository: begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("plugin marketplace repository: scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return fmt.Errorf("plugin marketplace repository: write: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("plugin marketplace repository: commit: %w", err)
	}
	return nil
}

func (r *Repository) read(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if invalid(r, ctx, scope) || fn == nil {
		return pluginmarketplace.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("plugin marketplace repository: begin read: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("plugin marketplace repository: scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return fmt.Errorf("plugin marketplace repository: read: %w", err)
	}
	return tx.Commit()
}

func normalize(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("plugin marketplace repository: %s: %w", op, err)
}
