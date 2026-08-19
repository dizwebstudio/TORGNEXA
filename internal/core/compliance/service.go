package compliance

import (
	"context"
	"time"
)

// EmitExpiryAlerts bridges the compliance registry to Task 022 Notification Center.
// Alert IDs are deterministic so a notifier can enforce idempotent delivery.
func EmitExpiryAlerts(ctx context.Context, repo Repository, scope Scope, notifier ExpiryNotifier, at time.Time, leadHours, limit int) (int, error) {
	if repo == nil || notifier == nil || !scope.Valid() || !utc(at) {
		return 0, ErrInvalid
	}
	docs, err := repo.ExpiryDue(ctx, scope, at, leadHours, limit)
	if err != nil {
		return 0, err
	}
	for _, document := range docs {
		alert, err := NewExpiryAlert(document, leadHours, at)
		if err != nil {
			return 0, err
		}
		if err := notifier.Notify(ctx, scope, alert); err != nil {
			return 0, err
		}
	}
	return len(docs), nil
}
