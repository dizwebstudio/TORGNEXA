// Package integrationcenterrepo persists rebuildable integration-state
// snapshots and durable recompute work. Source domain tables remain the
// authoritative owners of account, health, capability and sync state.
package integrationcenterrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/integrationcenter"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrInvalid  = errors.New("integration center repository: invalid input")
	ErrNotFound = errors.New("integration center repository: not found")
	ErrConflict = errors.New("integration center repository: conflict")
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// SaveSnapshot stores one immutable account projection. Replays of the same
// digest are accepted and do not create duplicate rows.
func (r *Repository) SaveSnapshot(ctx context.Context, scope tenancy.Scope, snapshot integrationcenter.Snapshot, watermarks []string) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || snapshot.Validate() != nil || len(watermarks) > 32 {
		return ErrInvalid
	}
	if len(snapshot.Dimensions.Runtime.Evidence.EvidenceDigest) > 0 && len(snapshot.Dimensions.Runtime.Evidence.EvidenceDigest) != 64 {
		return ErrInvalid
	}
	wm, err := json.Marshal(watermarks)
	if err != nil || len(wm) > 65536 {
		return ErrInvalid
	}
	dimensions, err := json.Marshal(snapshot.Dimensions)
	if err != nil || len(dimensions) > 65536 {
		return ErrInvalid
	}
	capabilities, err := json.Marshal(snapshot.Capabilities)
	if err != nil || len(capabilities) > 32768 {
		return ErrInvalid
	}
	issues, err := json.Marshal(snapshot.Issues)
	if err != nil || len(issues) > 32768 {
		return ErrInvalid
	}
	actions, err := json.Marshal(snapshot.Actions)
	if err != nil || len(actions) > 16384 {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO integration_center_snapshots(organization_id,workspace_id,snapshot_id,snapshot_version,snapshot_digest,generated_at,consistency,partial,source_watermarks,account_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1) ON CONFLICT (organization_id,workspace_id,snapshot_id) DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), snapshot.SnapshotID, snapshot.SnapshotVersion, snapshot.SnapshotDigest, snapshot.GeneratedAt, snapshot.Consistency, snapshot.Partial, wm)
		if err != nil {
			return fmt.Errorf("save snapshot: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO integration_center_snapshot_accounts(organization_id,workspace_id,snapshot_id,account_id,connector_id,family,surface,display_name,overall,account_version,dimensions,capabilities,issues,actions,row_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT (organization_id,workspace_id,snapshot_id,account_id) DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), snapshot.SnapshotID, snapshot.AccountID, snapshot.ConnectorID, snapshot.Family, snapshot.Surface, snapshot.DisplayName, snapshot.Overall, snapshot.Version, dimensions, capabilities, issues, actions, snapshot.SnapshotDigest)
		if err != nil {
			return fmt.Errorf("save snapshot row: %w", err)
		}
		return nil
	})
}

// EnqueueRecompute coalesces work by tenant and account, making source events
// safe to replay and bounding fan-out during a provider outage.
func (r *Repository) EnqueueRecompute(ctx context.Context, scope tenancy.Scope, accountID, reasonCode, eventID string, now time.Time) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || accountID == "" || reasonCode == "" || eventID == "" || now.IsZero() || now.Location() != time.UTC {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		return EnqueueRecomputeInTransaction(ctx, tx, scope, accountID, reasonCode, eventID, now)
	})
}

// EnqueueRecomputeInTransaction is the inbox/outbox bridge used by the Kafka
// consumer. The caller owns the transaction (and must already have applied the
// tenant RLS settings), so a committed inbox receipt and its coalesced work
// item are atomic. It intentionally accepts only the tiny ExecContext surface
// rather than leaking database/sql into the event handler.
func EnqueueRecomputeInTransaction(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, scope tenancy.Scope, accountID, reasonCode, eventID string, now time.Time) error {
	if ctx == nil || tx == nil || !scope.Valid() || accountID == "" || reasonCode == "" || eventID == "" || now.IsZero() || now.Location() != time.UTC || !safeQueueCode(reasonCode) {
		return ErrInvalid
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO integration_center_recompute_queue(organization_id,workspace_id,account_id,reason_code,event_id,available_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (organization_id,workspace_id,account_id) DO UPDATE SET reason_code=EXCLUDED.reason_code,event_id=EXCLUDED.event_id,available_at=LEAST(integration_center_recompute_queue.available_at,EXCLUDED.available_at),status='pending',lease_token=NULL,lease_expires_at=NULL,updated_at=clock_timestamp()`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, reasonCode, eventID, now)
	return err
}

type QueueItem struct {
	AccountID, ReasonCode, EventID, Status string
	Attempts                               int
	AvailableAt                            time.Time
}

// ClaimRecompute atomically leases the next coalesced account for a worker.
func (r *Repository) ClaimRecompute(ctx context.Context, scope tenancy.Scope, workerID string, now time.Time, lease time.Duration) (QueueItem, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || workerID == "" || now.IsZero() || now.Location() != time.UTC || lease <= 0 || lease > time.Hour {
		return QueueItem{}, ErrInvalid
	}
	var out QueueItem
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `WITH expired AS (
  UPDATE integration_center_recompute_queue
     SET status='pending', lease_token=NULL, lease_expires_at=NULL,
         available_at=LEAST(available_at,$3), last_error_code='lease_expired', updated_at=$3
   WHERE organization_id=$1 AND workspace_id=$2 AND status='leased' AND lease_expires_at <= $3
), candidate AS (
  SELECT account_id FROM integration_center_recompute_queue
   WHERE organization_id=$1 AND workspace_id=$2 AND status='pending' AND available_at <= $3
   ORDER BY available_at,account_id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE integration_center_recompute_queue q
   SET status='leased', lease_token=$4, lease_expires_at=$5,
       attempts=q.attempts+1, updated_at=$3
  FROM candidate
 WHERE q.organization_id=$1 AND q.workspace_id=$2 AND q.account_id=candidate.account_id
RETURNING q.account_id,q.reason_code,q.event_id,q.status,q.attempts,q.available_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), now, workerID, now.Add(lease))
		if err := row.Scan(&out.AccountID, &out.ReasonCode, &out.EventID, &out.Status, &out.Attempts, &out.AvailableAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return nil
	})
	return out, err
}

// RetryRecompute releases a leased item with bounded exponential backoff.
// Once the queue attempt budget is exhausted it is moved to dead_letter so a
// provider outage cannot create an unbounded retry storm. The returned error
// distinguishes a lost lease from a database failure and is safe to retry.
func (r *Repository) RetryRecompute(ctx context.Context, scope tenancy.Scope, accountID, workerID, errorCode string, now time.Time, delay time.Duration) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || accountID == "" || workerID == "" || !safeQueueCode(errorCode) || now.IsZero() || now.Location() != time.UTC || delay < 0 || delay > 15*time.Minute {
		return ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE integration_center_recompute_queue
SET status=CASE WHEN attempts >= 20 THEN 'dead_letter' ELSE 'pending' END,
    lease_token=NULL, lease_expires_at=NULL,
    available_at=CASE WHEN attempts >= 20 THEN available_at ELSE $4 END,
    last_error_code=$5, updated_at=$4
WHERE organization_id=$1 AND workspace_id=$2 AND account_id=$3
  AND status='leased' AND lease_token=$6`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, now.Add(delay), errorCode, workerID)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return ErrConflict
		}
		return nil
	})
}

func (r *Repository) CompleteRecompute(ctx context.Context, scope tenancy.Scope, accountID, workerID string, now time.Time, failed bool) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || accountID == "" || workerID == "" || now.IsZero() || now.Location() != time.UTC {
		return ErrInvalid
	}
	status := "completed"
	if failed {
		status = "dead_letter"
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE integration_center_recompute_queue SET status=$5,lease_token=NULL,lease_expires_at=NULL,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND account_id=$3 AND status='leased' AND lease_token=$6`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, now, status, workerID)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return ErrConflict
		}
		return nil
	})
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return err
	}
	if org != scope.OrganizationID().String() || ws != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func safeQueueCode(value string) bool {
	if len(value) == 0 || len(value) > 95 {
		return false
	}
	for i, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return false
		}
		if i == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}
