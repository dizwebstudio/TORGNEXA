// Package socialrepo implements tenant-scoped PostgreSQL persistence for Social Core.
package socialrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/social"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`
const contentSelect = `SELECT id,organization_id,workspace_id,title,body,status,version,created_at,updated_at FROM social_contents WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
const variantSelect = `SELECT id,organization_id,workspace_id,content_id,format,title,body,buttons,version,created_at FROM social_content_variants WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
const channelSelect = `SELECT id,organization_id,workspace_id,connector_account_id,display_name,capabilities,status,version,created_at,updated_at FROM social_channel_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
const publicationSelect = `SELECT id,organization_id,workspace_id,variant_id,channel_account_id,schedule_mode,scheduled_at,status,attempt,COALESCE(reason_code,''),version,created_at,updated_at,published_at FROM social_publications WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

type Repository struct{ db *sql.DB }

var _ social.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("social repository: database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Content(ctx context.Context, scope social.Scope, id social.ContentID) (social.Content, error) {
	if err := validate(ctx, r, scope); err != nil {
		return social.Content{}, err
	}
	if !id.Valid() {
		return social.Content{}, social.ErrInvalidRecord
	}
	var result social.Content
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanContent(tx.QueryRowContext(ctx, contentSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) Variant(ctx context.Context, scope social.Scope, id social.VariantID) (social.ContentVariant, error) {
	if err := validate(ctx, r, scope); err != nil {
		return social.ContentVariant{}, err
	}
	if !id.Valid() {
		return social.ContentVariant{}, social.ErrInvalidRecord
	}
	var result social.ContentVariant
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error { var err error; result, err = loadVariant(ctx, tx, scope, id); return err })
	return result, err
}

func (r *Repository) ChannelAccount(ctx context.Context, scope social.Scope, id social.ChannelAccountID) (social.ChannelAccount, error) {
	if err := validate(ctx, r, scope); err != nil {
		return social.ChannelAccount{}, err
	}
	if !id.Valid() {
		return social.ChannelAccount{}, social.ErrInvalidRecord
	}
	var result social.ChannelAccount
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanChannel(tx.QueryRowContext(ctx, channelSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) Publication(ctx context.Context, scope social.Scope, id social.PublicationID) (social.Publication, error) {
	if err := validate(ctx, r, scope); err != nil {
		return social.Publication{}, err
	}
	if !id.Valid() {
		return social.Publication{}, social.ErrInvalidRecord
	}
	var result social.Publication
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanPublication(tx.QueryRowContext(ctx, publicationSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) PublicationStatusEvents(ctx context.Context, scope social.Scope, id social.PublicationID, limit int) ([]social.StatusEvent, error) {
	if err := validate(ctx, r, scope); err != nil {
		return nil, err
	}
	if !id.Valid() || limit < 1 || limit > 1000 {
		return nil, social.ErrInvalidRecord
	}
	var result []social.StatusEvent
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT event_id,publication_id,publication_version,status,attempt,COALESCE(reason_code,''),correlation_id,occurred_at FROM social_publication_status_events WHERE organization_id=$1 AND workspace_id=$2 AND publication_id=$3 ORDER BY publication_version,event_id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), id.String(), limit)
		if err != nil {
			return fmt.Errorf("social repository: status history: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			event, err := scanStatusEvent(rows)
			if err != nil {
				return err
			}
			result = append(result, event)
		}
		return rows.Err()
	})
	return result, err
}

func (r *Repository) DuePublications(ctx context.Context, scope social.Scope, at time.Time, limit int) ([]social.Publication, error) {
	if err := validate(ctx, r, scope); err != nil {
		return nil, err
	}
	if at.IsZero() || at.Location() != time.UTC || limit < 1 || limit > 1000 {
		return nil, social.ErrInvalidRecord
	}
	var result []social.Publication
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,variant_id,channel_account_id,schedule_mode,scheduled_at,status,attempt,COALESCE(reason_code,''),version,created_at,updated_at,published_at FROM social_publications WHERE organization_id=$1 AND workspace_id=$2 AND status='scheduled' AND scheduled_at<=$3 ORDER BY scheduled_at,id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), at, limit)
		if err != nil {
			return fmt.Errorf("social repository: due publications: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			publication, err := scanPublication(rows)
			if err != nil {
				return err
			}
			result = append(result, publication)
		}
		return rows.Err()
	})
	return result, err
}

// ListChannelAccounts returns a bounded stable page for the tenant social UI.
func (r *Repository) ListChannelAccounts(ctx context.Context, scope social.Scope, limit int) ([]social.ChannelAccount, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 100 {
		return nil, social.ErrInvalidRecord
	}
	result := make([]social.ChannelAccount, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,connector_account_id,display_name,capabilities,status,version,created_at,updated_at FROM social_channel_accounts WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return fmt.Errorf("social repository: list channels: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			value, scanErr := scanChannel(rows)
			if scanErr != nil {
				return scanErr
			}
			result = append(result, value)
		}
		return rows.Err()
	})
	return result, err
}

// ListPublications returns recent canonical publications for the tenant social UI.
func (r *Repository) ListPublications(ctx context.Context, scope social.Scope, limit int) ([]social.Publication, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 100 {
		return nil, social.ErrInvalidRecord
	}
	result := make([]social.Publication, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,variant_id,channel_account_id,schedule_mode,scheduled_at,status,attempt,COALESCE(reason_code,''),version,created_at,updated_at,published_at FROM social_publications WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return fmt.Errorf("social repository: list publications: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			value, scanErr := scanPublication(rows)
			if scanErr != nil {
				return scanErr
			}
			result = append(result, value)
		}
		return rows.Err()
	})
	return result, err
}

func (r *Repository) CreateContent(ctx context.Context, scope social.Scope, command social.CreateContent, mutation social.Mutation) (social.Content, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.Content{}, err
	}
	if err := command.Validate(); err != nil {
		return social.Content{}, err
	}
	var result social.Content
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		result, err = scanContent(tx.QueryRowContext(ctx, `INSERT INTO social_contents(id,organization_id,workspace_id,title,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,title,body,status,version,created_at,updated_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.Title, command.Body))
		if errors.Is(err, social.ErrNotFound) {
			return social.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.content.created", "content", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"status": string(result.Status), "version": result.Version}); err != nil {
			return err
		}
		return enqueueContentEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

func (r *Repository) UpdateContent(ctx context.Context, scope social.Scope, command social.UpdateContent, mutation social.Mutation) (social.Content, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.Content{}, err
	}
	if err := command.Validate(); err != nil {
		return social.Content{}, err
	}
	var result social.Content
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanContent(tx.QueryRowContext(ctx, contentSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return social.ErrConflict
		}
		if current.Status != social.ContentDraft {
			return social.ErrInvalidState
		}
		result, err = scanContent(tx.QueryRowContext(ctx, `UPDATE social_contents SET title=$4,body=$5,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,organization_id,workspace_id,title,body,status,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), command.Title, command.Body, command.ExpectedVersion))
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.content.updated", "content", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"version": result.Version}); err != nil {
			return err
		}
		return enqueueContentEvent(ctx, tx, scope, mutation, result, "updated")
	})
	return result, err
}

func (r *Repository) ChangeContentStatus(ctx context.Context, scope social.Scope, command social.ChangeContentStatus, mutation social.Mutation) (social.Content, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.Content{}, err
	}
	if err := command.Validate(); err != nil {
		return social.Content{}, err
	}
	var result social.Content
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanContent(tx.QueryRowContext(ctx, contentSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return social.ErrConflict
		}
		if err := social.ValidateContentTransition(current.Status, command.Status); err != nil {
			return err
		}
		result, err = scanContent(tx.QueryRowContext(ctx, `UPDATE social_contents SET status=$4,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 RETURNING id,organization_id,workspace_id,title,body,status,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), string(command.Status), command.ExpectedVersion))
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.content.status_changed", "content", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"from": string(current.Status), "to": string(result.Status), "version": result.Version}); err != nil {
			return err
		}
		return enqueueContentEvent(ctx, tx, scope, mutation, result, "status_changed")
	})
	return result, err
}

func (r *Repository) CreateVariant(ctx context.Context, scope social.Scope, command social.CreateVariant, mutation social.Mutation) (social.ContentVariant, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.ContentVariant{}, err
	}
	if err := command.Validate(); err != nil {
		return social.ContentVariant{}, err
	}
	var result social.ContentVariant
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		content, err := scanContent(tx.QueryRowContext(ctx, contentSelect+` FOR SHARE`, scope.OrganizationID(), scope.WorkspaceID(), command.ContentID.String()))
		if err != nil {
			return err
		}
		if content.Status == social.ContentArchived {
			return social.ErrInvalidState
		}
		for _, media := range command.Media {
			var state string
			if err := tx.QueryRowContext(ctx, `SELECT state FROM uploads WHERE id=$1 AND organization_id=$2 AND workspace_id=$3`, media.UploadID, scope.OrganizationID(), scope.WorkspaceID()).Scan(&state); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return social.ErrMediaUnavailable
				}
				return fmt.Errorf("social repository: validate media release: %w", err)
			}
			if state != "released" {
				return social.ErrMediaUnavailable
			}
		}
		buttons, _ := json.Marshal(command.Buttons)
		result, err = scanVariantBase(tx.QueryRowContext(ctx, `INSERT INTO social_content_variants(id,organization_id,workspace_id,content_id,format,title,body,buttons) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,content_id,format,title,body,buttons,version,created_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.ContentID.String(), string(command.Format), command.Title, command.Body, string(buttons)))
		if errors.Is(err, social.ErrNotFound) {
			return social.ErrConflict
		}
		if err != nil {
			return err
		}
		for position, media := range command.Media {
			if _, err := tx.ExecContext(ctx, `INSERT INTO social_variant_media_refs(organization_id,workspace_id,variant_id,position,upload_id,kind,alt_text) VALUES($1,$2,$3,$4,$5,$6,$7)`, scope.OrganizationID(), scope.WorkspaceID(), result.ID.String(), position, media.UploadID, string(media.Kind), media.AltText); err != nil {
				return fmt.Errorf("social repository: insert media ref: %w", err)
			}
		}
		result.Media = append([]social.MediaRef(nil), command.Media...)
		if err := result.Validate(); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.variant.created", "content_variant", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"content_id": result.ContentID.String(), "format": string(result.Format), "media_count": len(result.Media), "version": result.Version}); err != nil {
			return err
		}
		return enqueueVariantEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

func (r *Repository) CreateChannelAccount(ctx context.Context, scope social.Scope, command social.CreateChannelAccount, mutation social.Mutation) (social.ChannelAccount, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.ChannelAccount{}, err
	}
	if err := command.Validate(); err != nil {
		return social.ChannelAccount{}, err
	}
	caps, _ := json.Marshal(command.Capabilities)
	var result social.ChannelAccount
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var family string
		if err := tx.QueryRowContext(ctx, `SELECT family FROM connector_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), command.ConnectorAccountID).Scan(&family); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return social.ErrNotFound
			}
			return fmt.Errorf("social repository: connector account: %w", err)
		}
		if family != "social" {
			return social.ErrInvalidRecord
		}
		var err error
		result, err = scanChannel(tx.QueryRowContext(ctx, `INSERT INTO social_channel_accounts(id,organization_id,workspace_id,connector_account_id,display_name,capabilities) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,connector_account_id,display_name,capabilities,status,version,created_at,updated_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.ConnectorAccountID, command.DisplayName, string(caps)))
		if errors.Is(err, social.ErrNotFound) {
			return social.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.channel_account.created", "channel_account", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"connector_account_id": result.ConnectorAccountID, "status": string(result.Status), "capabilities": capabilityStrings(result.Capabilities), "version": result.Version}); err != nil {
			return err
		}
		return enqueueChannelEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

func (r *Repository) UpdateChannelAccount(ctx context.Context, scope social.Scope, command social.UpdateChannelAccount, mutation social.Mutation) (social.ChannelAccount, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.ChannelAccount{}, err
	}
	if err := command.Validate(); err != nil {
		return social.ChannelAccount{}, err
	}
	caps, _ := json.Marshal(command.Capabilities)
	var result social.ChannelAccount
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanChannel(tx.QueryRowContext(ctx, channelSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return social.ErrConflict
		}
		if command.Status == social.ChannelActive {
			var family, status string
			if err := tx.QueryRowContext(ctx, `SELECT family,status FROM connector_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), current.ConnectorAccountID).Scan(&family, &status); err != nil {
				return err
			}
			if family != "social" || status != "active" {
				return social.ErrChannelUnavailable
			}
		}
		result, err = scanChannel(tx.QueryRowContext(ctx, `UPDATE social_channel_accounts SET display_name=$4,capabilities=$5::jsonb,status=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7 RETURNING id,organization_id,workspace_id,connector_account_id,display_name,capabilities,status,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), command.DisplayName, string(caps), string(command.Status), command.ExpectedVersion))
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.channel_account.updated", "channel_account", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"from_status": string(current.Status), "to_status": string(result.Status), "capabilities": capabilityStrings(result.Capabilities), "version": result.Version}); err != nil {
			return err
		}
		return enqueueChannelEvent(ctx, tx, scope, mutation, result, "updated")
	})
	return result, err
}

func (r *Repository) CreatePublication(ctx context.Context, scope social.Scope, command social.CreatePublication, mutation social.Mutation) (social.Publication, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.Publication{}, err
	}
	if err := command.Validate(); err != nil {
		return social.Publication{}, err
	}
	initial, _ := social.InitialPublicationStatus(command.Schedule)
	var scheduledAt any
	if command.Schedule.PublishAt != nil {
		scheduledAt = *command.Schedule.PublishAt
	}
	var result social.Publication
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		variant, err := loadVariant(ctx, tx, scope, command.VariantID)
		if err != nil {
			return err
		}
		channel, err := scanChannel(tx.QueryRowContext(ctx, channelSelect+` FOR SHARE`, scope.OrganizationID(), scope.WorkspaceID(), command.ChannelAccountID.String()))
		if err != nil {
			return err
		}
		if err := social.ValidatePublicationPlan(channel, variant); err != nil {
			return err
		}
		var contentStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM social_contents WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), variant.ContentID.String()).Scan(&contentStatus); err != nil {
			return err
		}
		if contentStatus != "ready" {
			return social.ErrInvalidState
		}
		result, err = scanPublication(tx.QueryRowContext(ctx, `INSERT INTO social_publications(id,organization_id,workspace_id,variant_id,channel_account_id,schedule_mode,scheduled_at,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,variant_id,channel_account_id,schedule_mode,scheduled_at,status,attempt,COALESCE(reason_code,''),version,created_at,updated_at,published_at`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.VariantID.String(), command.ChannelAccountID.String(), string(command.Schedule.Mode), scheduledAt, string(initial), mutation.OccurredAt))
		if errors.Is(err, social.ErrNotFound) {
			return social.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := insertStatusEvent(ctx, tx, scope, mutation, result); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.publication.created", "publication", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"variant_id": result.VariantID.String(), "channel_account_id": result.ChannelAccountID.String(), "status": string(result.Status), "schedule_mode": string(result.Schedule.Mode), "version": result.Version}); err != nil {
			return err
		}
		return enqueuePublicationEvent(ctx, tx, scope, mutation, result)
	})
	return result, err
}

func (r *Repository) ChangePublicationStatus(ctx context.Context, scope social.Scope, command social.ChangePublicationStatus, mutation social.Mutation) (social.Publication, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return social.Publication{}, err
	}
	if err := command.Validate(); err != nil {
		return social.Publication{}, err
	}
	var result social.Publication
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanPublication(tx.QueryRowContext(ctx, publicationSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return social.ErrConflict
		}
		if err := social.ValidatePublicationTransition(current.Status, command.Status); err != nil {
			return err
		}
		attempt := current.Attempt
		if command.Status == social.PublicationPublishing {
			attempt++
		}
		var publishedAt any
		if command.Status == social.PublicationPublished {
			publishedAt = mutation.OccurredAt
		}
		var reason any
		if command.ReasonCode != "" {
			reason = command.ReasonCode
		}
		result, err = scanPublication(tx.QueryRowContext(ctx, `UPDATE social_publications SET status=$4,attempt=$5,reason_code=$6,published_at=$7,version=version+1,updated_at=$8 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$9 RETURNING id,organization_id,workspace_id,variant_id,channel_account_id,schedule_mode,scheduled_at,status,attempt,COALESCE(reason_code,''),version,created_at,updated_at,published_at`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), string(command.Status), attempt, reason, publishedAt, mutation.OccurredAt, command.ExpectedVersion))
		if err != nil {
			return err
		}
		if err := insertStatusEvent(ctx, tx, scope, mutation, result); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "social.publication.status_changed", "publication", result.ID.String(), audit.RiskWriteSafe, audit.Summary{"from": string(current.Status), "to": string(result.Status), "attempt": result.Attempt, "reason_code": result.ReasonCode, "version": result.Version}); err != nil {
			return err
		}
		return enqueuePublicationEvent(ctx, tx, scope, mutation, result)
	})
	return result, err
}

func (r *Repository) tx(ctx context.Context, scope social.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("social repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("social repository: scope: %w", err)
	}
	if org != scope.OrganizationID() || ws != scope.WorkspaceID() {
		return social.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("social repository: commit: %w", err)
	}
	return nil
}

func validate(ctx context.Context, r *Repository, scope social.Scope) error {
	if ctx == nil {
		return errors.New("social repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.db == nil {
		return errors.New("social repository: uninitialized")
	}
	if !scope.Valid() {
		return social.ErrInvalidScope
	}
	return nil
}
func validateMutation(ctx context.Context, r *Repository, scope social.Scope, mutation social.Mutation) error {
	if err := validate(ctx, r, scope); err != nil {
		return err
	}
	return mutation.Validate()
}

type scanner interface{ Scan(...any) error }

func scanContent(row scanner) (social.Content, error) {
	var result social.Content
	var id, org, ws, status string
	if err := row.Scan(&id, &org, &ws, &result.Title, &result.Body, &status, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return social.Content{}, social.ErrNotFound
		}
		return social.Content{}, fmt.Errorf("social repository: scan content: %w", err)
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.Status = social.ContentID(id), org, ws, social.ContentStatus(status)
	result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return social.Content{}, err
	}
	return result, nil
}

func scanVariantBase(row scanner) (social.ContentVariant, error) {
	var result social.ContentVariant
	var id, org, ws, contentID, format string
	var rawButtons []byte
	if err := row.Scan(&id, &org, &ws, &contentID, &format, &result.Title, &result.Body, &rawButtons, &result.Version, &result.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return social.ContentVariant{}, social.ErrNotFound
		}
		return social.ContentVariant{}, fmt.Errorf("social repository: scan variant: %w", err)
	}
	if len(rawButtons) == 0 || json.Unmarshal(rawButtons, &result.Buttons) != nil {
		return social.ContentVariant{}, social.ErrInvalidRecord
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.ContentID, result.Format = social.VariantID(id), org, ws, social.ContentID(contentID), social.VariantFormat(format)
	result.CreatedAt = result.CreatedAt.UTC()
	return result, nil
}

func loadVariant(ctx context.Context, tx *sql.Tx, scope social.Scope, id social.VariantID) (social.ContentVariant, error) {
	result, err := scanVariantBase(tx.QueryRowContext(ctx, variantSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
	if err != nil {
		return social.ContentVariant{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT upload_id,kind,alt_text FROM social_variant_media_refs WHERE organization_id=$1 AND workspace_id=$2 AND variant_id=$3 ORDER BY position`, scope.OrganizationID(), scope.WorkspaceID(), id.String())
	if err != nil {
		return social.ContentVariant{}, fmt.Errorf("social repository: media refs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref social.MediaRef
		var kind string
		if err := rows.Scan(&ref.UploadID, &kind, &ref.AltText); err != nil {
			return social.ContentVariant{}, err
		}
		ref.Kind = social.MediaKind(kind)
		result.Media = append(result.Media, ref)
	}
	if err := rows.Err(); err != nil {
		return social.ContentVariant{}, err
	}
	if err := result.Validate(); err != nil {
		return social.ContentVariant{}, err
	}
	return result, nil
}

func scanChannel(row scanner) (social.ChannelAccount, error) {
	var result social.ChannelAccount
	var id, org, ws, status string
	var raw []byte
	if err := row.Scan(&id, &org, &ws, &result.ConnectorAccountID, &result.DisplayName, &raw, &status, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return social.ChannelAccount{}, social.ErrNotFound
		}
		return social.ChannelAccount{}, fmt.Errorf("social repository: scan channel: %w", err)
	}
	if err := json.Unmarshal(raw, &result.Capabilities); err != nil {
		return social.ChannelAccount{}, social.ErrInvalidRecord
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.Status = social.ChannelAccountID(id), org, ws, social.ChannelStatus(status)
	result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return social.ChannelAccount{}, err
	}
	return result, nil
}

func scanPublication(row scanner) (social.Publication, error) {
	var result social.Publication
	var id, org, ws, variantID, channelID, scheduleMode, status string
	var scheduledAt, publishedAt sql.NullTime
	if err := row.Scan(&id, &org, &ws, &variantID, &channelID, &scheduleMode, &scheduledAt, &status, &result.Attempt, &result.ReasonCode, &result.Version, &result.CreatedAt, &result.UpdatedAt, &publishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return social.Publication{}, social.ErrNotFound
		}
		return social.Publication{}, fmt.Errorf("social repository: scan publication: %w", err)
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.VariantID, result.ChannelAccountID = social.PublicationID(id), org, ws, social.VariantID(variantID), social.ChannelAccountID(channelID)
	result.Schedule.Mode, result.Status = social.ScheduleMode(scheduleMode), social.PublicationStatus(status)
	if scheduledAt.Valid {
		at := scheduledAt.Time.UTC()
		result.Schedule.PublishAt = &at
	}
	if publishedAt.Valid {
		at := publishedAt.Time.UTC()
		result.PublishedAt = &at
	}
	result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return social.Publication{}, err
	}
	return result, nil
}

func scanStatusEvent(row scanner) (social.StatusEvent, error) {
	var result social.StatusEvent
	var publicationID, status string
	if err := row.Scan(&result.EventID, &publicationID, &result.PublicationVersion, &status, &result.Attempt, &result.ReasonCode, &result.CorrelationID, &result.OccurredAt); err != nil {
		return social.StatusEvent{}, fmt.Errorf("social repository: scan status event: %w", err)
	}
	result.PublicationID, result.Status, result.OccurredAt = social.PublicationID(publicationID), social.PublicationStatus(status), result.OccurredAt.UTC()
	if err := result.Validate(); err != nil {
		return social.StatusEvent{}, err
	}
	return result, nil
}

func tenantScope(scope social.Scope) (tenancy.Scope, error) {
	return tenancy.ParseScope(scope.OrganizationID(), scope.WorkspaceID())
}

func appendAudit(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, action, resourceType, resourceID string, risk audit.Risk, summary audit.Summary) error {
	ts, err := tenantScope(scope)
	if err != nil {
		return err
	}
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	record := audit.Record{ID: mutation.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: mutation.ActorID, Source: mutation.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: mutation.CorrelationID, Risk: risk, Summary: safe, CreatedAt: mutation.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, ts, record)
}

func insertStatusEvent(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, publication social.Publication) error {
	var reason any
	if publication.ReasonCode != "" {
		reason = publication.ReasonCode
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO social_publication_status_events(organization_id,workspace_id,event_id,publication_id,publication_version,status,attempt,reason_code,correlation_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, scope.OrganizationID(), scope.WorkspaceID(), mutation.EventID, publication.ID.String(), publication.Version, string(publication.Status), publication.Attempt, reason, mutation.CorrelationID, mutation.OccurredAt)
	if err != nil {
		return fmt.Errorf("social repository: insert status event: %w", err)
	}
	return nil
}

func enqueueContentEvent(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, content social.Content, change string) error {
	payload := struct {
		ContentID string               `json:"content_id"`
		Status    social.ContentStatus `json:"status"`
		Version   int64                `json:"version"`
		Change    string               `json:"change"`
	}{content.ID.String(), content.Status, content.Version, change}
	return enqueue(ctx, tx, scope, mutation, "commerce.social.content_changed.v1", "content", content.ID.String(), payload)
}
func enqueueVariantEvent(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, variant social.ContentVariant, change string) error {
	payload := struct {
		VariantID  string               `json:"variant_id"`
		ContentID  string               `json:"content_id"`
		Format     social.VariantFormat `json:"format"`
		MediaCount int                  `json:"media_count"`
		Version    int64                `json:"version"`
		Change     string               `json:"change"`
	}{variant.ID.String(), variant.ContentID.String(), variant.Format, len(variant.Media), variant.Version, change}
	return enqueue(ctx, tx, scope, mutation, "commerce.social.variant_changed.v1", "content_variant", variant.ID.String(), payload)
}
func enqueueChannelEvent(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, account social.ChannelAccount, change string) error {
	payload := struct {
		ChannelAccountID   string               `json:"channel_account_id"`
		ConnectorAccountID string               `json:"connector_account_id"`
		Status             social.ChannelStatus `json:"status"`
		Capabilities       []string             `json:"capabilities"`
		Version            int64                `json:"version"`
		Change             string               `json:"change"`
	}{account.ID.String(), account.ConnectorAccountID, account.Status, capabilityStrings(account.Capabilities), account.Version, change}
	return enqueue(ctx, tx, scope, mutation, "commerce.social.channel_account_changed.v1", "channel_account", account.ID.String(), payload)
}
func enqueuePublicationEvent(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, publication social.Publication) error {
	var scheduledAt *string
	if publication.Schedule.PublishAt != nil {
		value := publication.Schedule.PublishAt.UTC().Format(time.RFC3339Nano)
		scheduledAt = &value
	}
	payload := struct {
		PublicationID    string                   `json:"publication_id"`
		VariantID        string                   `json:"variant_id"`
		ChannelAccountID string                   `json:"channel_account_id"`
		ScheduleMode     social.ScheduleMode      `json:"schedule_mode"`
		ScheduledAt      *string                  `json:"scheduled_at"`
		Status           social.PublicationStatus `json:"status"`
		Attempt          int                      `json:"attempt"`
		ReasonCode       string                   `json:"reason_code,omitempty"`
		Version          int64                    `json:"version"`
	}{publication.ID.String(), publication.VariantID.String(), publication.ChannelAccountID.String(), publication.Schedule.Mode, scheduledAt, publication.Status, publication.Attempt, publication.ReasonCode, publication.Version}
	return enqueue(ctx, tx, scope, mutation, "commerce.social.publication_status_changed.v1", "publication", publication.ID.String(), payload)
}

func enqueue(ctx context.Context, tx *sql.Tx, scope social.Scope, mutation social.Mutation, eventType, entityType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return err
	}
	at, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return err
	}
	event := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, TraceID: mutation.TraceID, Data: data}
	if err := event.Validate(); err != nil {
		return err
	}
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, event)
}

func capabilityStrings(values []social.Capability) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}
