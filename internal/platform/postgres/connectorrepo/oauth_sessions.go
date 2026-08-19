package connectorrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	authflow "github.com/torgnexa/torgnexa/internal/platform/connectorauth"
)

const insertOAuthSessionStatement = `INSERT INTO connector_oauth_sessions
 (id,organization_id,workspace_id,connector_account_id,account_version,actor_id,state_sha256,pending_secret_reference,callback_url,correlation_id,status,created_at,expires_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',$11,$12)
 ON CONFLICT (organization_id,workspace_id,actor_id,correlation_id) DO NOTHING`

const selectOAuthSessionByCorrelationStatement = `SELECT id,connector_account_id,account_version,actor_id,state_sha256,pending_secret_reference,callback_url,correlation_id,status,created_at,expires_at,consumed_at
 FROM connector_oauth_sessions WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND correlation_id=$4`

const consumeOAuthSessionStatement = `UPDATE connector_oauth_sessions s
 SET status='consumed',consumed_at=$6
 WHERE s.organization_id=$1 AND s.workspace_id=$2 AND s.state_sha256=$3 AND s.actor_id=$4 AND s.callback_url=$5
   AND s.status='pending' AND s.expires_at >= $6
   AND EXISTS (SELECT 1 FROM connector_accounts a WHERE a.organization_id=s.organization_id AND a.workspace_id=s.workspace_id
     AND a.id=s.connector_account_id AND a.version=s.account_version AND a.status='disabled')
 RETURNING id,connector_account_id,account_version,actor_id,state_sha256,pending_secret_reference,callback_url,correlation_id,status,created_at,expires_at,consumed_at`

var _ authflow.SessionStore = (*Repository)(nil)

// CreateOrReplay stores a pending session or returns the exact existing
// idempotency-key result without creating a second valid state.
func (repository *Repository) CreateOrReplay(ctx context.Context, scope tenancy.Scope, proposed authflow.Session) (authflow.Session, bool, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil || !scope.Valid() || proposed.Validate() != nil || proposed.Status != "pending" {
		return authflow.Session{}, false, authflow.ErrInvalid
	}
	var stored authflow.Session
	inserted := false
	err := repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, insertOAuthSessionStatement, proposed.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), proposed.AccountID, proposed.AccountVersion, proposed.ActorID, proposed.StateDigest, proposed.PendingSecretRef, proposed.CallbackURL, proposed.CorrelationID, proposed.CreatedAt.UTC(), proposed.ExpiresAt.UTC())
		if err != nil {
			return fmt.Errorf("connector oauth repository: insert: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("connector oauth repository: result: %w", err)
		}
		inserted = count == 1
		stored, err = scanOAuthSession(tx.QueryRowContext(ctx, selectOAuthSessionByCorrelationStatement, scope.OrganizationID().String(), scope.WorkspaceID().String(), proposed.ActorID, proposed.CorrelationID))
		return err
	})
	if err != nil {
		return authflow.Session{}, false, err
	}
	if !inserted && !sameOAuthStart(stored, proposed) {
		return authflow.Session{}, false, authflow.ErrSessionConflict
	}
	return stored, !inserted, nil
}

// Consume atomically binds state to tenant, actor, callback, account version and
// expiry. A second callback can never obtain the session again.
func (repository *Repository) Consume(ctx context.Context, scope tenancy.Scope, stateDigest, actorID, callbackURL string, now time.Time) (authflow.Session, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil || !scope.Valid() || len(stateDigest) != 64 || actorID == "" || callbackURL == "" || now.IsZero() {
		return authflow.Session{}, authflow.ErrInvalid
	}
	var session authflow.Session
	err := repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		session, err = scanOAuthSession(tx.QueryRowContext(ctx, consumeOAuthSessionStatement, scope.OrganizationID().String(), scope.WorkspaceID().String(), stateDigest, actorID, callbackURL, now.UTC()))
		return err
	})
	if errors.Is(err, authflow.ErrSessionNotFound) {
		return authflow.Session{}, authflow.ErrSessionConflict
	}
	return session, err
}

func scanOAuthSession(scanner interface{ Scan(...any) error }) (authflow.Session, error) {
	var value authflow.Session
	err := scanner.Scan(&value.ID, &value.AccountID, &value.AccountVersion, &value.ActorID, &value.StateDigest, &value.PendingSecretRef, &value.CallbackURL, &value.CorrelationID, &value.Status, &value.CreatedAt, &value.ExpiresAt, &value.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return authflow.Session{}, authflow.ErrSessionNotFound
	}
	if err != nil {
		return authflow.Session{}, fmt.Errorf("connector oauth repository: scan: %w", err)
	}
	if value.Validate() != nil {
		return authflow.Session{}, authflow.ErrInvalid
	}
	return value, nil
}

func sameOAuthStart(left, right authflow.Session) bool {
	return left.AccountID == right.AccountID && left.AccountVersion == right.AccountVersion && left.ActorID == right.ActorID && left.CallbackURL == right.CallbackURL && left.Status == "pending"
}
