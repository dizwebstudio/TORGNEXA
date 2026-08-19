// Package cloudbillingrepo reads tenant-scoped TORGNEXA Cloud subscription state.
package cloudbillingrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/cloudbilling"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var ErrNotFound = errors.New("cloud billing repository: subscription not found")

// Repository reads Cloud commercial state without coupling Community runtime
// availability to the presence of a subscription.
type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, cloudbilling.ErrInvalid
	}
	return &Repository{db: db}, nil
}

// CurrentSubscription returns the most recently updated subscription in scope.
func (r *Repository) CurrentSubscription(ctx context.Context, scope tenancy.Scope) (cloudbilling.Subscription, error) {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return cloudbilling.Subscription{}, cloudbilling.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return cloudbilling.Subscription{}, fmt.Errorf("cloud billing repository: begin: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return cloudbilling.Subscription{}, fmt.Errorf("cloud billing repository: scope: %w", err)
	}
	var item cloudbilling.Subscription
	var state string
	err = tx.QueryRowContext(ctx, `SELECT subscription_id,plan_id,plan_version,state,period_start,period_end,updated_at,version
FROM cloud_subscriptions WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,subscription_id LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&item.ID, &item.PlanID, &item.PlanVersion, &state, &item.CurrentPeriodStart, &item.CurrentPeriodEnd, &item.UpdatedAt, &item.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return cloudbilling.Subscription{}, ErrNotFound
	}
	if err != nil {
		return cloudbilling.Subscription{}, fmt.Errorf("cloud billing repository: select: %w", err)
	}
	item.State = cloudbilling.SubscriptionState(state)
	item.CurrentPeriodStart, item.CurrentPeriodEnd, item.UpdatedAt = item.CurrentPeriodStart.UTC(), item.CurrentPeriodEnd.UTC(), item.UpdatedAt.UTC()
	if item.ID == "" || item.PlanID == "" || item.PlanVersion < 1 || !item.State.Valid() || item.Version < 1 || item.CurrentPeriodEnd.Before(item.CurrentPeriodStart) || item.UpdatedAt.IsZero() {
		return cloudbilling.Subscription{}, cloudbilling.ErrInvalid
	}
	if err := tx.Commit(); err != nil {
		return cloudbilling.Subscription{}, fmt.Errorf("cloud billing repository: commit: %w", err)
	}
	return item, nil
}
