// Package webhookrepo implements PostgreSQL persistence for durable outbound webhooks.
package webhookrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/webhooks"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

// Transaction is the commit-less SQL surface accepted from inboxrepo. Keeping
// this interface local avoids a package cycle while allowing webhook projection
// and the immutable inbox receipt to commit atomically.
type Transaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var _ webhooks.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("webhook repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) CreateSubscription(ctx context.Context, scope tenancy.Scope, s webhooks.Subscription) error {
	if err := validate(ctx, scope, r); err != nil {
		return err
	}
	if s.Validate() != nil {
		return webhooks.ErrInvalid
	}
	events, err := encodeEvents(s.EventTypes)
	if err != nil {
		return err
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO webhook_subscriptions
(id,organization_id,workspace_id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at)
VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13)`, s.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), s.Endpoint, string(events), string(s.Status), s.SigningSecret.String(), nullableRef(s.PreviousSigningSecret), s.PreviousValidUntil, s.ConsecutiveFailures, s.Version, s.CreatedAt, s.UpdatedAt)
		if err != nil {
			return fmt.Errorf("webhook repository: create subscription: %w", err)
		}
		return nil
	})
}

func (r *Repository) Subscription(ctx context.Context, scope tenancy.Scope, id string) (webhooks.Subscription, error) {
	if err := validate(ctx, scope, r); err != nil {
		return webhooks.Subscription{}, err
	}
	var out webhooks.Subscription
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at FROM webhook_subscriptions WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		return scanSubscription(row, &out)
	})
	if err != nil {
		return webhooks.Subscription{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) ListSubscriptions(ctx context.Context, scope tenancy.Scope) ([]webhooks.Subscription, error) {
	return r.listSubscriptions(ctx, scope, "")
}
func (r *Repository) MatchingSubscriptions(ctx context.Context, scope tenancy.Scope, typ eventbus.EventType) ([]webhooks.Subscription, error) {
	if typ.Validate() != nil {
		return nil, webhooks.ErrInvalid
	}
	return r.listSubscriptions(ctx, scope, typ.String())
}
func (r *Repository) listSubscriptions(ctx context.Context, scope tenancy.Scope, eventType string) ([]webhooks.Subscription, error) {
	if err := validate(ctx, scope, r); err != nil {
		return nil, err
	}
	out := []webhooks.Subscription{}
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		query := `SELECT id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at FROM webhook_subscriptions WHERE organization_id=$1 AND workspace_id=$2`
		args := []any{scope.OrganizationID().String(), scope.WorkspaceID().String()}
		if eventType != "" {
			query += ` AND status='active' AND event_types ? $3`
			args = append(args, eventType)
		}
		query += ` ORDER BY created_at,id`
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s webhooks.Subscription
			if err := scanSubscription(rows, &s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, normalize(err)
	}
	return out, nil
}

func (r *Repository) DisableSubscription(ctx context.Context, scope tenancy.Scope, id string, now time.Time) (webhooks.Subscription, error) {
	if err := validate(ctx, scope, r); err != nil {
		return webhooks.Subscription{}, err
	}
	if id == "" || now.Location() != time.UTC {
		return webhooks.Subscription{}, webhooks.ErrInvalid
	}
	var out webhooks.Subscription
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `WITH changed AS (
UPDATE webhook_subscriptions SET status='disabled',version=version+1,updated_at=$4
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='active'
RETURNING id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at
)
SELECT * FROM changed
UNION ALL
SELECT id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at
FROM webhook_subscriptions WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='disabled'
LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, now)
		return scanSubscription(row, &out)
	})
	if err != nil {
		return webhooks.Subscription{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) RotateSubscription(ctx context.Context, scope tenancy.Scope, id string, current, previous secrets.Reference, until, now time.Time) (webhooks.Subscription, error) {
	if err := validate(ctx, scope, r); err != nil {
		return webhooks.Subscription{}, err
	}
	if !current.Valid() || !previous.Valid() || !until.After(now) || now.Location() != time.UTC || until.Location() != time.UTC {
		return webhooks.Subscription{}, webhooks.ErrInvalid
	}
	var out webhooks.Subscription
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE webhook_subscriptions SET signing_secret_reference=$4,previous_signing_secret_reference=$5,previous_valid_until=$6,version=version+1,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='active' AND signing_secret_reference=$5 AND previous_signing_secret_reference IS NULL RETURNING id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, current.String(), previous.String(), until, now)
		return scanSubscription(row, &out)
	})
	if err != nil {
		return webhooks.Subscription{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) ClearPreviousSecret(ctx context.Context, scope tenancy.Scope, id string, previous secrets.Reference, now time.Time) error {
	if err := validate(ctx, scope, r); err != nil {
		return err
	}
	if !previous.Valid() || now.Location() != time.UTC {
		return webhooks.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET previous_signing_secret_reference=NULL,previous_valid_until=NULL,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND previous_signing_secret_reference=$4 AND previous_valid_until<=$5`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, previous.String(), now)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return webhooks.ErrConflict
		}
		return nil
	})
}

func (r *Repository) Enqueue(ctx context.Context, scope tenancy.Scope, d webhooks.Delivery) (bool, error) {
	if err := validate(ctx, scope, r); err != nil {
		return false, err
	}
	if d.Validate() != nil || d.Status != webhooks.DeliveryPending || d.Attempt != 0 {
		return false, webhooks.ErrInvalid
	}
	var inserted bool
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		var writeErr error
		inserted, writeErr = enqueueTransaction(ctx, tx, scope, d)
		return writeErr
	})
	return inserted, normalize(err)
}

func enqueueTransaction(ctx context.Context, tx Transaction, scope tenancy.Scope, d webhooks.Delivery) (bool, error) {
	if ctx == nil || tx == nil || !scope.Valid() || d.Validate() != nil || d.Status != webhooks.DeliveryPending || d.Attempt != 0 {
		return false, webhooks.ErrInvalid
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO webhook_deliveries
(id,organization_id,workspace_id,subscription_id,event_id,event_type,endpoint_url,signing_secret_reference,body,status,attempt,available_at,replay_of,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,'pending',0,$10,$11,$12,$12)
ON CONFLICT (organization_id,workspace_id,subscription_id,event_id) WHERE replay_of IS NULL DO NOTHING`, d.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), d.SubscriptionID, d.EventID, d.EventType.String(), d.Endpoint, d.SigningSecret.String(), string(d.Body), d.AvailableAt, nullableString(d.ReplayOf), d.CreatedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ProjectEventTransaction performs the Kafka-to-webhook durable projection on
// the caller-owned inbox transaction. It MUST only be called after inboxrepo
// has applied tenant scope to tx; this method neither commits nor rolls back.
func (r *Repository) ProjectEventTransaction(ctx context.Context, scope tenancy.Scope, tx Transaction, incoming eventbus.Delivery, ids webhooks.IDGenerator, now time.Time) error {
	if ctx == nil || r == nil || r.db == nil || tx == nil || !scope.Valid() || incoming.Validate() != nil || incoming.Event.OrganizationID != scope.OrganizationID().String() || incoming.Event.WorkspaceID != scope.WorkspaceID().String() || now.IsZero() || now.Location() != time.UTC {
		return webhooks.ErrInvalid
	}
	if ids == nil {
		ids = webhooks.RandomIDs{}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,endpoint_url,event_types,status,signing_secret_reference,previous_signing_secret_reference,previous_valid_until,consecutive_failures,version,created_at,updated_at FROM webhook_subscriptions WHERE organization_id=$1 AND workspace_id=$2 AND status='active' AND event_types ? $3 ORDER BY created_at,id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), incoming.Event.Type.String())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sub webhooks.Subscription
		if err := scanSubscription(rows, &sub); err != nil {
			return err
		}
		if !sub.Accepts(incoming.Event.Type) {
			continue
		}
		id, err := ids.NewID("whd_")
		if err != nil {
			return err
		}
		envelope := webhooks.Envelope{DeliveryID: id, EventID: incoming.Event.ID, EventType: incoming.Event.Type, OccurredAt: incoming.Event.OccurredAt.Time(), OrganizationID: incoming.Event.OrganizationID, WorkspaceID: incoming.Event.WorkspaceID, Data: append(json.RawMessage(nil), incoming.Event.Data...)}
		body, err := envelope.Marshal()
		if err != nil {
			return err
		}
		delivery := webhooks.Delivery{ID: id, SubscriptionID: sub.ID, EventID: incoming.Event.ID, EventType: incoming.Event.Type, Endpoint: sub.Endpoint, SigningSecret: sub.SigningSecret, Body: body, Status: webhooks.DeliveryPending, AvailableAt: now, CreatedAt: now}
		if _, err := enqueueTransaction(ctx, tx, scope, delivery); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Repository) Claim(ctx context.Context, scope tenancy.Scope, workerID string, now time.Time, lease time.Duration) (webhooks.Delivery, error) {
	if err := validate(ctx, scope, r); err != nil {
		return webhooks.Delivery{}, err
	}
	if workerID == "" || now.Location() != time.UTC || lease <= 0 {
		return webhooks.Delivery{}, webhooks.ErrInvalid
	}
	token, err := claimToken(workerID)
	if err != nil {
		return webhooks.Delivery{}, err
	}
	expires := now.Add(lease)
	var out webhooks.Delivery
	err = r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `WITH candidate AS (
SELECT d.id FROM webhook_deliveries d WHERE d.organization_id=$1 AND d.workspace_id=$2 AND ((d.status='pending' AND d.available_at<=$3) OR (d.status='inflight' AND d.lease_expires_at<=$3)) ORDER BY d.available_at,d.created_at,d.id FOR UPDATE SKIP LOCKED LIMIT 1)
UPDATE webhook_deliveries d SET status='inflight',attempt=d.attempt+1,lease_token=$4,lease_expires_at=$5,updated_at=$3 FROM candidate c,webhook_subscriptions s WHERE d.id=c.id AND s.id=d.subscription_id AND s.organization_id=d.organization_id AND s.workspace_id=d.workspace_id
RETURNING d.id,d.subscription_id,d.event_id,d.event_type,d.endpoint_url,d.signing_secret_reference,d.body,d.status,d.attempt,d.available_at,d.lease_token,d.lease_expires_at,d.replay_of,d.created_at,s.consecutive_failures`, scope.OrganizationID().String(), scope.WorkspaceID().String(), now, token, expires)
		return scanDelivery(row, &out)
	})
	if err != nil {
		return webhooks.Delivery{}, normalizeClaim(err)
	}
	return out, nil
}

func (r *Repository) Complete(ctx context.Context, scope tenancy.Scope, a webhooks.AttemptResult) error {
	if err := validate(ctx, scope, r); err != nil {
		return err
	}
	if a.DeliveryID == "" || a.LeaseToken == "" || a.Attempt < 1 || a.CompletedAt.Location() != time.UTC || a.Duration < 0 || a.Duration > time.Hour {
		return webhooks.ErrInvalid
	}
	if a.Outcome != webhooks.OutcomeSucceeded && a.Outcome != webhooks.OutcomeRetry && a.Outcome != webhooks.OutcomeDLQ {
		return webhooks.ErrInvalid
	}
	if a.Outcome == webhooks.OutcomeRetry && (a.NextAvailableAt == nil || a.NextAvailableAt.Location() != time.UTC || a.NextAvailableAt.Before(a.CompletedAt)) {
		return webhooks.ErrInvalid
	}
	if a.Outcome != webhooks.OutcomeRetry && a.NextAvailableAt != nil {
		return webhooks.ErrInvalid
	}
	code := webhooks.SafeErrorCode(a.ErrorCode)
	if code == "internal_error" && a.ErrorCode != "internal_error" {
		return webhooks.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		var subscription string
		var state string
		var attempt int
		var lease string
		if err := tx.QueryRowContext(ctx, `SELECT subscription_id,status,attempt,COALESCE(lease_token,'') FROM webhook_deliveries WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), a.DeliveryID).Scan(&subscription, &state, &attempt, &lease); err != nil {
			return err
		}
		if state != "inflight" || attempt != a.Attempt || lease != a.LeaseToken {
			return webhooks.ErrConflict
		}
		var status string
		var available any = a.CompletedAt
		var succeeded, dlq any
		switch a.Outcome {
		case webhooks.OutcomeSucceeded:
			status = "succeeded"
			succeeded = a.CompletedAt
		case webhooks.OutcomeRetry:
			status = "pending"
			available = *a.NextAvailableAt
		case webhooks.OutcomeDLQ:
			status = "dlq"
			dlq = a.CompletedAt
		}
		var httpStatus any
		if a.HTTPStatus > 0 {
			httpStatus = a.HTTPStatus
		}
		var errorCode any
		if code != "" {
			errorCode = code
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO webhook_delivery_attempts(delivery_id,organization_id,workspace_id,attempt,outcome,http_status,duration_ms,error_code,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.DeliveryID, scope.OrganizationID().String(), scope.WorkspaceID().String(), a.Attempt, string(a.Outcome), httpStatus, a.Duration.Milliseconds(), errorCode, a.CompletedAt)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET status=$4,available_at=$5,lease_token=NULL,lease_expires_at=NULL,updated_at=$6,succeeded_at=$7,dlq_at=$8,last_http_status=$9,last_error_code=$10 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='inflight' AND attempt=$11`, scope.OrganizationID().String(), scope.WorkspaceID().String(), a.DeliveryID, status, available, a.CompletedAt, succeeded, dlq, httpStatus, errorCode, a.Attempt)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return webhooks.ErrConflict
		}
		if a.Outcome == webhooks.OutcomeSucceeded {
			_, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET consecutive_failures=0,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), subscription, a.CompletedAt)
		} else if a.Outcome == webhooks.OutcomeDLQ && (code == "http_permanent" || code == "endpoint_unsafe") {
			if a.DisableSubscription {
				_, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET consecutive_failures=consecutive_failures+1,status='disabled',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), subscription, a.CompletedAt)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET consecutive_failures=consecutive_failures+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), subscription, a.CompletedAt)
			}
		}
		return err
	})
}

func (r *Repository) Delivery(ctx context.Context, scope tenancy.Scope, id string) (webhooks.Delivery, error) {
	if err := validate(ctx, scope, r); err != nil {
		return webhooks.Delivery{}, err
	}
	var out webhooks.Delivery
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		return scanDelivery(tx.QueryRowContext(ctx, `SELECT d.id,d.subscription_id,d.event_id,d.event_type,d.endpoint_url,d.signing_secret_reference,d.body,d.status,d.attempt,d.available_at,COALESCE(d.lease_token,''),d.lease_expires_at,COALESCE(d.replay_of,''),d.created_at,s.consecutive_failures FROM webhook_deliveries d JOIN webhook_subscriptions s ON s.id=d.subscription_id AND s.organization_id=d.organization_id AND s.workspace_id=d.workspace_id WHERE d.organization_id=$1 AND d.workspace_id=$2 AND d.id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id), &out)
	})
	if err != nil {
		return webhooks.Delivery{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) Replay(ctx context.Context, scope tenancy.Scope, sourceID, newID string, now time.Time) (webhooks.Delivery, error) {
	if err := validate(ctx, scope, r); err != nil {
		return webhooks.Delivery{}, err
	}
	if sourceID == "" || newID == "" || now.Location() != time.UTC {
		return webhooks.Delivery{}, webhooks.ErrInvalid
	}
	var out webhooks.Delivery
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `WITH source AS (SELECT d.*,s.endpoint_url AS current_endpoint,s.signing_secret_reference AS current_secret,s.consecutive_failures FROM webhook_deliveries d JOIN webhook_subscriptions s ON s.id=d.subscription_id AND s.organization_id=d.organization_id AND s.workspace_id=d.workspace_id WHERE d.organization_id=$1 AND d.workspace_id=$2 AND d.id=$3)
INSERT INTO webhook_deliveries(id,organization_id,workspace_id,subscription_id,event_id,event_type,endpoint_url,signing_secret_reference,body,status,attempt,available_at,replay_of,created_at,updated_at)
SELECT $4,organization_id,workspace_id,subscription_id,event_id,event_type,current_endpoint,current_secret,jsonb_set(body,'{delivery_id}',to_jsonb($4::text),false),'pending',0,$5,id,$5,$5 FROM source
RETURNING id,subscription_id,event_id,event_type,endpoint_url,signing_secret_reference,body,status,attempt,available_at,COALESCE(lease_token,''),lease_expires_at,COALESCE(replay_of,''),created_at,0`, scope.OrganizationID().String(), scope.WorkspaceID().String(), sourceID, newID, now)
		return scanDelivery(row, &out)
	})
	if err != nil {
		return webhooks.Delivery{}, normalize(err)
	}
	return out, nil
}

func (r *Repository) History(ctx context.Context, scope tenancy.Scope, deliveryID string, limit int) ([]webhooks.HistoryEntry, error) {
	if err := validate(ctx, scope, r); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, webhooks.ErrInvalid
	}
	out := []webhooks.HistoryEntry{}
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT delivery_id,attempt,outcome,COALESCE(http_status,0),duration_ms,COALESCE(error_code,''),completed_at FROM webhook_delivery_attempts WHERE organization_id=$1 AND workspace_id=$2 AND delivery_id=$3 ORDER BY attempt DESC LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), deliveryID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h webhooks.HistoryEntry
			if err := rows.Scan(&h.DeliveryID, &h.Attempt, &h.Outcome, &h.HTTPStatus, &h.DurationMS, &h.ErrorCode, &h.CompletedAt); err != nil {
				return err
			}
			h.CompletedAt = h.CompletedAt.UTC()
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, normalize(err)
}

type rowScanner interface{ Scan(...any) error }

func scanSubscription(row rowScanner, out *webhooks.Subscription) error {
	var rawEvents []byte
	var status string
	var current string
	var previous sql.NullString
	var previousUntil sql.NullTime
	if err := row.Scan(&out.ID, &out.Endpoint, &rawEvents, &status, &current, &previous, &previousUntil, &out.ConsecutiveFailures, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return err
	}
	events, err := decodeEvents(rawEvents)
	if err != nil {
		return err
	}
	ref, err := secrets.ParseReference(current)
	if err != nil {
		return err
	}
	out.EventTypes = events
	out.Status = webhooks.SubscriptionStatus(status)
	out.SigningSecret = ref
	if previous.Valid {
		ref, err := secrets.ParseReference(previous.String)
		if err != nil {
			return err
		}
		out.PreviousSigningSecret = ref
	}
	if previousUntil.Valid {
		v := previousUntil.Time.UTC()
		out.PreviousValidUntil = &v
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out.Validate()
}
func scanDelivery(row rowScanner, out *webhooks.Delivery) error {
	var eventType, status, secret string
	var body []byte
	var leaseExpiry sql.NullTime
	var replay string
	if err := row.Scan(&out.ID, &out.SubscriptionID, &out.EventID, &eventType, &out.Endpoint, &secret, &body, &status, &out.Attempt, &out.AvailableAt, &out.LeaseToken, &leaseExpiry, &replay, &out.CreatedAt, &out.ConsecutivePermanentFailures); err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return err
	}
	ref, err := secrets.ParseReference(secret)
	if err != nil {
		return err
	}
	out.EventType = typ
	out.SigningSecret = ref
	out.Body = append([]byte(nil), body...)
	out.Status = webhooks.DeliveryStatus(status)
	out.ReplayOf = replay
	out.AvailableAt = out.AvailableAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	if leaseExpiry.Valid {
		v := leaseExpiry.Time.UTC()
		out.LeaseExpiresAt = &v
	}
	return out.Validate()
}
func encodeEvents(events []eventbus.EventType) ([]byte, error) {
	values := make([]string, len(events))
	for i, e := range events {
		if e.Validate() != nil {
			return nil, webhooks.ErrInvalid
		}
		values[i] = e.String()
	}
	return json.Marshal(values)
}
func decodeEvents(raw []byte) ([]eventbus.EventType, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, webhooks.ErrInvalid
	}
	out := make([]eventbus.EventType, len(values))
	for i, v := range values {
		e, err := eventbus.ParseEventType(v)
		if err != nil {
			return nil, webhooks.ErrInvalid
		}
		out[i] = e
	}
	return out, nil
}
func nullableRef(ref secrets.Reference) any {
	if ref == "" {
		return nil
	}
	return ref.String()
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func claimToken(worker string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return worker + ":" + hex.EncodeToString(b), nil
}
func validate(ctx context.Context, scope tenancy.Scope, r *Repository) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return webhooks.ErrInvalid
	}
	return ctx.Err()
}
func (r *Repository) read(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, true, fn)
}
func (r *Repository) write(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, false, fn)
}
func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	opts := &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly}
	tx, err := r.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func normalize(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return webhooks.ErrNotFound
	}
	return err
}
func normalizeClaim(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return webhooks.ErrNoDelivery
	}
	return normalize(err)
}
