// Package securitysettingsrepo implements tenant-scoped settings security storage.
package securitysettingsrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository stores minimized OIDC session evidence in PostgreSQL.
type Repository struct{ database *sql.DB }

// New creates a repository using an application role subject to forced RLS.
func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("settings security repository: database is required")
	}
	return &Repository{database: database}, nil
}

// Observe registers a validated OIDC session or rejects a previously revoked one.
func (r *Repository) Observe(ctx context.Context, scope tenancy.Scope, value securitysettings.Observation) error {
	if err := validate(ctx, r, scope); err != nil || !validObservation(value) {
		return securitysettings.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM settings_identity_sessions WHERE organization_id=$1 AND workspace_id=$2 AND session_ref=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.SessionRef).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = tx.ExecContext(ctx, `INSERT INTO settings_identity_sessions(organization_id,workspace_id,session_ref,subject_ref,status,client_kind,authenticated_at,first_seen_at,last_seen_at,expires_at) VALUES($1,$2,$3,$4,'active',$5,$6,$7,$7,$8)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.SessionRef, value.SubjectRef, value.ClientKind, value.AuthenticatedAt.UTC(), value.ObservedAt.UTC(), value.ExpiresAt.UTC()); err != nil {
				return fmt.Errorf("insert identity session: %w", err)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO settings_login_events(id,organization_id,workspace_id,session_ref,event_type,client_kind,occurred_at) VALUES($1,$2,$3,$4,'session_observed',$5,$6)`, value.EventID, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.SessionRef, value.ClientKind, value.ObservedAt.UTC())
			return err
		}
		if err != nil {
			return fmt.Errorf("read identity session: %w", err)
		}
		if status == "revoked" {
			return securitysettings.ErrSessionRevoked
		}
		_, err = tx.ExecContext(ctx, `UPDATE settings_identity_sessions SET last_seen_at=GREATEST(last_seen_at,$4),expires_at=GREATEST(expires_at,$5) WHERE organization_id=$1 AND workspace_id=$2 AND session_ref=$3 AND status='active'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), value.SessionRef, value.ObservedAt.UTC(), value.ExpiresAt.UTC())
		return err
	})
}

// ListSessions returns a stable tenant-scoped page using an opaque hash cursor.
func (r *Repository) ListSessions(ctx context.Context, scope tenancy.Scope, limit int, cursor string) ([]securitysettings.Session, string, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 100 || len(cursor) > 64 {
		return nil, "", securitysettings.ErrInvalid
	}
	items := make([]securitysettings.Session, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		query := `SELECT session_ref,subject_ref,status,client_kind,authenticated_at,first_seen_at,last_seen_at,expires_at,revoked_at FROM settings_identity_sessions WHERE organization_id=$1 AND workspace_id=$2`
		args := []any{scope.OrganizationID().String(), scope.WorkspaceID().String()}
		if cursor != "" {
			query += ` AND session_ref < $3`
			args = append(args, cursor)
		}
		query += fmt.Sprintf(` ORDER BY session_ref DESC LIMIT $%d`, len(args)+1)
		args = append(args, limit+1)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item securitysettings.Session
			var revoked sql.NullTime
			if err := rows.Scan(&item.Ref, &item.SubjectRef, &item.Status, &item.ClientKind, &item.AuthenticatedAt, &item.FirstSeenAt, &item.LastSeenAt, &item.ExpiresAt, &revoked); err != nil {
				return err
			}
			item.AuthenticatedAt, item.FirstSeenAt, item.LastSeenAt, item.ExpiresAt = item.AuthenticatedAt.UTC(), item.FirstSeenAt.UTC(), item.LastSeenAt.UTC(), item.ExpiresAt.UTC()
			if revoked.Valid {
				value := revoked.Time.UTC()
				item.RevokedAt = &value
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", fmt.Errorf("list identity sessions: %w", err)
	}
	next := ""
	if len(items) > limit {
		next = items[limit-1].Ref
		items = items[:limit]
	}
	return items, next, nil
}

// ListLoginEvents returns append-only application-observed login history.
func (r *Repository) ListLoginEvents(ctx context.Context, scope tenancy.Scope, limit int, cursor string) ([]securitysettings.LoginEvent, string, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 100 || len(cursor) > 64 {
		return nil, "", securitysettings.ErrInvalid
	}
	items := make([]securitysettings.LoginEvent, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		query := `SELECT id,session_ref,event_type,client_kind,occurred_at FROM settings_login_events WHERE organization_id=$1 AND workspace_id=$2`
		args := []any{scope.OrganizationID().String(), scope.WorkspaceID().String()}
		if cursor != "" {
			query += ` AND id < $3`
			args = append(args, cursor)
		}
		query += fmt.Sprintf(` ORDER BY id DESC LIMIT $%d`, len(args)+1)
		args = append(args, limit+1)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item securitysettings.LoginEvent
			if err := rows.Scan(&item.ID, &item.SessionRef, &item.EventType, &item.ClientKind, &item.OccurredAt); err != nil {
				return err
			}
			item.OccurredAt = item.OccurredAt.UTC()
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", fmt.Errorf("list login events: %w", err)
	}
	next := ""
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	return items, next, nil
}

// Revoke atomically denies a session and appends login plus audit evidence.
func (r *Repository) Revoke(ctx context.Context, scope tenancy.Scope, command securitysettings.RevokeCommand) (securitysettings.Session, error) {
	if err := validate(ctx, r, scope); err != nil || command.EventID == "" || len(command.SessionRef) != 64 || command.ActorID == "" || command.CorrelationID == "" || command.OccurredAt.IsZero() {
		return securitysettings.Session{}, securitysettings.ErrInvalid
	}
	var item securitysettings.Session
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var revoked sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT session_ref,subject_ref,status,client_kind,authenticated_at,first_seen_at,last_seen_at,expires_at,revoked_at FROM settings_identity_sessions WHERE organization_id=$1 AND workspace_id=$2 AND session_ref=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), command.SessionRef).Scan(&item.Ref, &item.SubjectRef, &item.Status, &item.ClientKind, &item.AuthenticatedAt, &item.FirstSeenAt, &item.LastSeenAt, &item.ExpiresAt, &revoked)
		if errors.Is(err, sql.ErrNoRows) {
			return securitysettings.ErrNotFound
		}
		if err != nil {
			return err
		}
		if item.Status == "revoked" {
			if revoked.Valid {
				value := revoked.Time.UTC()
				item.RevokedAt = &value
			}
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE settings_identity_sessions SET status='revoked',revoked_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND session_ref=$3 AND status='active'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), command.SessionRef, command.OccurredAt.UTC()); err != nil {
			return err
		}
		item.Status = "revoked"
		revoked = sql.NullTime{Time: command.OccurredAt.UTC(), Valid: true}
		if revoked.Valid {
			value := revoked.Time.UTC()
			item.RevokedAt = &value
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings_login_events(id,organization_id,workspace_id,session_ref,event_type,client_kind,occurred_at) VALUES($1,$2,$3,$4,'session_revoked',$5,$6) ON CONFLICT (organization_id,workspace_id,session_ref,event_type) DO NOTHING`, command.EventID, scope.OrganizationID().String(), scope.WorkspaceID().String(), command.SessionRef, item.ClientKind, command.OccurredAt.UTC()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_records(id,organization_id,workspace_id,actor_id,source,action,resource_type,resource_id,correlation_id,risk,summary,created_at) VALUES($1,$2,$3,$4,'api','settings.security.session_revoked','identity_session',$5,$6,'write_sensitive',$7::jsonb,$8)`, command.EventID, scope.OrganizationID().String(), scope.WorkspaceID().String(), command.ActorID, command.SessionRef, command.CorrelationID, `{"scope":"torgnexa_api"}`, command.OccurredAt.UTC())
		return err
	})
	if err != nil {
		return securitysettings.Session{}, err
	}
	return item, nil
}

func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err = tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return err
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func validate(ctx context.Context, r *Repository, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.database == nil || !scope.Valid() {
		return securitysettings.ErrInvalid
	}
	return ctx.Err()
}
func validObservation(v securitysettings.Observation) bool {
	return len(v.EventID) == 36 && len(v.SessionRef) == 64 && len(v.SubjectRef) == 64 && v.ClientKind != "" && !v.AuthenticatedAt.IsZero() && !v.ExpiresAt.IsZero() && !v.ObservedAt.IsZero() && v.ExpiresAt.After(v.AuthenticatedAt)
}

var _ securitysettings.Store = (*Repository)(nil)
