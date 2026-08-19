// Package notificationrepo implements PostgreSQL persistence for the notification inbox.
package notificationrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

var _ notifications.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("notification repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Upsert(ctx context.Context, scope tenancy.Scope, n notifications.Notification) (notifications.Notification, notifications.Disposition, error) {
	if err := validate(ctx, scope, r); err != nil {
		return notifications.Notification{}, "", err
	}
	if n.Validate() != nil {
		return notifications.Notification{}, "", notifications.ErrInvalid
	}
	var out notifications.Notification
	var disposition notifications.Disposition
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		lockKey := scope.OrganizationID().String() + "|" + scope.WorkspaceID().String() + "|" + n.RecipientID + "|" + n.DedupeKey
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return err
		}
		// Serialize the dedupe key to make occurrence counting and severity escalation deterministic.
		var existingSeverity string
		var existingSource sql.NullString
		var existingLast time.Time
		err := tx.QueryRowContext(ctx, `SELECT severity,source_event_id,last_occurred_at FROM notifications WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND dedupe_key=$4 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), n.RecipientID, n.DedupeKey).Scan(&existingSeverity, &existingSource, &existingLast)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			disposition = notifications.DispositionCreated
			_, err = tx.ExecContext(ctx, `INSERT INTO notifications
(id,organization_id,workspace_id,recipient_id,dedupe_key,severity,title,body,entity_type,entity_id,source_event_id,source_event_type,occurrence_count,first_occurred_at,last_occurred_at,read_at,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$13,NULL,$14,$14)`, n.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), n.RecipientID, n.DedupeKey, string(n.Severity), n.Title, n.Body, nullString(n.EntityType), nullString(n.EntityID), nullString(n.SourceEventID), nullString(n.SourceEventType.String()), n.FirstOccurredAt, n.CreatedAt)
			if err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			old := notifications.Severity(existingSeverity)
			sameOccurrence := (n.SourceEventID != "" && existingSource.Valid && existingSource.String == n.SourceEventID) || existingLast.Equal(n.LastOccurredAt)
			if sameOccurrence {
				disposition = notifications.DispositionReplay
				break
			}
			if severityRank(n.Severity) > severityRank(old) {
				disposition = notifications.DispositionEscalated
			} else {
				disposition = notifications.DispositionDeduplicated
			}
			// Keep the first identity; refresh presentation/source facts, count every distinct occurrence,
			// never lower severity, and make a newly escalated item unread again.
			_, err = tx.ExecContext(ctx, `UPDATE notifications SET
severity=CASE WHEN $6='critical' OR ($6='warning' AND severity='info') THEN $6 ELSE severity END,
title=$7,body=$8,entity_type=COALESCE($9,entity_type),entity_id=COALESCE($10,entity_id),source_event_id=COALESCE($11,source_event_id),source_event_type=COALESCE($12,source_event_type),
read_at=CASE WHEN $6='critical' AND severity<>'critical' OR $6='warning' AND severity='info' THEN NULL ELSE read_at END,
occurrence_count=occurrence_count+1,last_occurred_at=GREATEST(last_occurred_at,$13),updated_at=$14
WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND dedupe_key=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), n.RecipientID, n.DedupeKey, n.ID, string(n.Severity), n.Title, n.Body, nullString(n.EntityType), nullString(n.EntityID), nullString(n.SourceEventID), nullString(n.SourceEventType.String()), n.LastOccurredAt, n.UpdatedAt)
			if err != nil {
				return err
			}
		}
		row := tx.QueryRowContext(ctx, notificationSelect+` WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND dedupe_key=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), n.RecipientID, n.DedupeKey)
		return scanNotification(row, &out)
	})
	if err != nil {
		return notifications.Notification{}, "", normalize(err)
	}
	return out, disposition, nil
}

func (r *Repository) List(ctx context.Context, scope tenancy.Scope, recipient string, limit int) ([]notifications.Notification, error) {
	if err := validate(ctx, scope, r); err != nil {
		return nil, err
	}
	if recipient == "" || limit < 1 || limit > notifications.MaxPageSize {
		return nil, notifications.ErrInvalid
	}
	out := []notifications.Notification{}
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, notificationSelect+` WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND (dedupe_key NOT LIKE 'demo.%' OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)) ORDER BY last_occurred_at DESC,id DESC LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), recipient, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n notifications.Notification
			if err := scanNotification(rows, &n); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, normalize(err)
	}
	return out, nil
}

func (r *Repository) MarkRead(ctx context.Context, scope tenancy.Scope, recipient, id string, now time.Time) (notifications.Notification, error) {
	if err := validate(ctx, scope, r); err != nil {
		return notifications.Notification{}, err
	}
	if recipient == "" || id == "" || now.Location() != time.UTC {
		return notifications.Notification{}, notifications.ErrInvalid
	}
	var out notifications.Notification
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE notifications SET read_at=COALESCE(read_at,$5),updated_at=GREATEST(updated_at,$5) WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND id=$4 RETURNING id,recipient_id,dedupe_key,severity,title,body,entity_type,entity_id,source_event_id,source_event_type,occurrence_count,first_occurred_at,last_occurred_at,read_at,created_at,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), recipient, id, now)
		return scanNotification(row, &out)
	})
	if err != nil {
		return notifications.Notification{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) PutPreference(ctx context.Context, scope tenancy.Scope, p notifications.Preference) (notifications.Preference, error) {
	if err := validate(ctx, scope, r); err != nil {
		return notifications.Preference{}, err
	}
	if p.Validate() != nil {
		return notifications.Preference{}, notifications.ErrInvalid
	}
	var out notifications.Preference
	categories, err := json.Marshal(p.Categories)
	if err != nil {
		return notifications.Preference{}, notifications.ErrInvalid
	}
	err = r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO notification_preferences(organization_id,workspace_id,recipient_id,channel,enabled,min_severity,categories,quiet_enabled,quiet_start,quiet_end,timezone,version,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1,$12)
ON CONFLICT(organization_id,workspace_id,recipient_id,channel) DO UPDATE SET enabled=EXCLUDED.enabled,min_severity=EXCLUDED.min_severity,categories=EXCLUDED.categories,quiet_enabled=EXCLUDED.quiet_enabled,quiet_start=EXCLUDED.quiet_start,quiet_end=EXCLUDED.quiet_end,timezone=EXCLUDED.timezone,version=notification_preferences.version+1,updated_at=EXCLUDED.updated_at WHERE notification_preferences.version=$13
RETURNING recipient_id,channel,enabled,min_severity,categories,quiet_enabled,quiet_start,quiet_end,timezone,version,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), p.RecipientID, string(p.Channel), p.Enabled, string(p.MinSeverity), categories, p.QuietEnabled, p.QuietStart, p.QuietEnd, p.Timezone, p.UpdatedAt, p.Version)
		var raw []byte
		if err := row.Scan(&out.RecipientID, &out.Channel, &out.Enabled, &out.MinSeverity, &raw, &out.QuietEnabled, &out.QuietStart, &out.QuietEnd, &out.Timezone, &out.Version, &out.UpdatedAt); err != nil {
			return err
		}
		return json.Unmarshal(raw, &out.Categories)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notifications.Preference{}, notifications.ErrConflict
		}
		return notifications.Preference{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) Preference(ctx context.Context, scope tenancy.Scope, recipient string, ch notifications.Channel) (notifications.Preference, error) {
	if err := validate(ctx, scope, r); err != nil {
		return notifications.Preference{}, err
	}
	if recipient == "" || !ch.Valid() {
		return notifications.Preference{}, notifications.ErrInvalid
	}
	var out notifications.Preference
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		var raw []byte
		err := tx.QueryRowContext(ctx, `SELECT recipient_id,channel,enabled,min_severity,categories,quiet_enabled,quiet_start,quiet_end,timezone,version,updated_at FROM notification_preferences WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND channel=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), recipient, string(ch)).Scan(&out.RecipientID, &out.Channel, &out.Enabled, &out.MinSeverity, &raw, &out.QuietEnabled, &out.QuietStart, &out.QuietEnd, &out.Timezone, &out.Version, &out.UpdatedAt)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &out.Categories)
	})
	if err != nil {
		return notifications.Preference{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) RecordDelivery(ctx context.Context, scope tenancy.Scope, d notifications.Delivery) error {
	if err := validate(ctx, scope, r); err != nil {
		return err
	}
	if d.Validate() != nil {
		return notifications.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		lockKey := scope.OrganizationID().String() + "|" + scope.WorkspaceID().String() + "|" + d.NotificationID + "|" + string(d.Channel) + "|" + fmt.Sprint(d.Occurrence)
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries(notification_id,organization_id,workspace_id,channel,status,error_code,occurrence,attempt,attempted_at) SELECT $1,$2,$3,$4,$5,$6,$7,COALESCE(MAX(attempt),0)+1,$8 FROM notification_deliveries WHERE organization_id=$2 AND workspace_id=$3 AND notification_id=$1 AND channel=$4 AND occurrence=$7`, d.NotificationID, scope.OrganizationID().String(), scope.WorkspaceID().String(), string(d.Channel), string(d.Status), nullString(d.ErrorCode), d.Occurrence, d.AttemptedAt)
		return err
	})
}
func (r *Repository) Deliveries(ctx context.Context, scope tenancy.Scope, recipient, id string) ([]notifications.Delivery, error) {
	if err := validate(ctx, scope, r); err != nil {
		return nil, err
	}
	if recipient == "" || id == "" {
		return nil, notifications.ErrInvalid
	}
	out := []notifications.Delivery{}
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM notifications WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND recipient_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, recipient).Scan(&exists); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT notification_id,channel,status,COALESCE(error_code,''),occurrence,attempt,attempted_at FROM notification_deliveries WHERE organization_id=$1 AND workspace_id=$2 AND notification_id=$3 ORDER BY occurrence,channel,attempt`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d notifications.Delivery
			if err := rows.Scan(&d.NotificationID, &d.Channel, &d.Status, &d.ErrorCode, &d.Occurrence, &d.Attempt, &d.AttemptedAt); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, normalize(err)
	}
	return out, nil
}

const notificationSelect = `SELECT id,recipient_id,dedupe_key,severity,title,body,entity_type,entity_id,source_event_id,source_event_type,occurrence_count,first_occurred_at,last_occurred_at,read_at,created_at,updated_at FROM notifications`

type scanner interface{ Scan(...any) error }

func scanNotification(row scanner, n *notifications.Notification) error {
	var entityType, entityID, sourceID, sourceType sql.NullString
	if err := row.Scan(&n.ID, &n.RecipientID, &n.DedupeKey, &n.Severity, &n.Title, &n.Body, &entityType, &entityID, &sourceID, &sourceType, &n.OccurrenceCount, &n.FirstOccurredAt, &n.LastOccurredAt, &n.ReadAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return err
	}
	n.EntityType = entityType.String
	n.EntityID = entityID.String
	n.SourceEventID = sourceID.String
	if sourceType.Valid {
		typ, err := eventbus.ParseEventType(sourceType.String)
		if err != nil {
			return err
		}
		n.SourceEventType = typ
	}
	n.FirstOccurredAt = n.FirstOccurredAt.UTC()
	n.LastOccurredAt = n.LastOccurredAt.UTC()
	n.CreatedAt = n.CreatedAt.UTC()
	n.UpdatedAt = n.UpdatedAt.UTC()
	if n.ReadAt != nil {
		readAt := n.ReadAt.UTC()
		n.ReadAt = &readAt
	}
	if n.Validate() != nil {
		return notifications.ErrInvalid
	}
	return nil
}

func (r *Repository) read(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, &sql.TxOptions{ReadOnly: true}, fn)
}
func (r *Repository) write(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, nil, fn)
}
func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func validate(ctx context.Context, scope tenancy.Scope, r *Repository) error {
	if ctx == nil || !scope.Valid() || r == nil || r.db == nil {
		return notifications.ErrInvalid
	}
	return nil
}
func normalize(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notifications.ErrNotFound
	}
	return fmt.Errorf("notification repository: %w", err)
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func severityRank(v notifications.Severity) int {
	switch v {
	case notifications.SeverityInfo:
		return 1
	case notifications.SeverityWarning:
		return 2
	case notifications.SeverityCritical:
		return 3
	}
	return 0
}
