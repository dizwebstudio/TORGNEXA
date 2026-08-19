package notificationrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type Destination struct {
	RecipientID     string                `json:"recipient_id"`
	Channel         notifications.Channel `json:"channel"`
	SecretReference secrets.Reference     `json:"secret_reference"`
	Version         int64                 `json:"version"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

func (r *Repository) PutDestination(ctx context.Context, scope tenancy.Scope, d Destination, expectedVersion int64) (Destination, error) {
	if err := validate(ctx, scope, r); err != nil {
		return Destination{}, err
	}
	if d.RecipientID == "" || (d.Channel != notifications.ChannelEmail && d.Channel != notifications.ChannelChat) || !d.SecretReference.Valid() || expectedVersion < 0 {
		return Destination{}, notifications.ErrInvalid
	}
	var out Destination
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO notification_destinations(organization_id,workspace_id,recipient_id,channel,destination_secret_reference,version,updated_at) VALUES($1,$2,$3,$4,$5,1,clock_timestamp()) ON CONFLICT(organization_id,workspace_id,recipient_id,channel) DO UPDATE SET destination_secret_reference=EXCLUDED.destination_secret_reference,version=notification_destinations.version+1,updated_at=clock_timestamp() WHERE notification_destinations.version=$6 RETURNING recipient_id,channel,destination_secret_reference,version,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), d.RecipientID, string(d.Channel), d.SecretReference.String(), expectedVersion)
		var raw string
		if err := row.Scan(&out.RecipientID, &out.Channel, &raw, &out.Version, &out.UpdatedAt); err != nil {
			return err
		}
		ref, err := secrets.ParseReference(raw)
		if err != nil {
			return err
		}
		out.SecretReference = ref
		out.UpdatedAt = out.UpdatedAt.UTC()
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Destination{}, notifications.ErrConflict
	}
	return out, err
}

func (r *Repository) Destination(ctx context.Context, scope tenancy.Scope, recipient string, channel notifications.Channel) (Destination, error) {
	if err := validate(ctx, scope, r); err != nil {
		return Destination{}, err
	}
	if recipient == "" || (channel != notifications.ChannelEmail && channel != notifications.ChannelChat) {
		return Destination{}, notifications.ErrInvalid
	}
	var out Destination
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT recipient_id,channel,destination_secret_reference,version,updated_at FROM notification_destinations WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND channel=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), recipient, string(channel)).Scan(&out.RecipientID, &out.Channel, &raw, &out.Version, &out.UpdatedAt)
		if err != nil {
			return err
		}
		ref, err := secrets.ParseReference(raw)
		if err != nil {
			return err
		}
		out.SecretReference = ref
		out.UpdatedAt = out.UpdatedAt.UTC()
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Destination{}, notifications.ErrNotFound
	}
	return out, err
}
