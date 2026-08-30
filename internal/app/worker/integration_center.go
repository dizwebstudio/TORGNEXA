package worker

import (
	"context"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/integrationcenterrepo"
)

const integrationCenterKafkaConsumerGroup = "torgnexa.integration-center.v1"

// integrationCenterEventHandler turns canonical account status transitions
// into coalesced tenant-scoped recompute work. The queue has a unique tenant /
// account key, so duplicate and out-of-order deliveries converge safely.
type integrationCenterEventHandler struct {
	queue *integrationcenterrepo.Repository
}

func (h integrationCenterEventHandler) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	if h.queue == nil {
		return eventbus.Permanent("integration_center_queue_unavailable")
	}
	event := delivery.Event
	if !strings.HasPrefix(event.Type.String(), "commerce.integration.") {
		return nil
	}
	scope, err := tenancy.ParseScope(event.OrganizationID, event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("integration_center_invalid_scope")
	}
	if event.EntityID == "" {
		return eventbus.Permanent("integration_center_missing_account")
	}
	reason := "integration_event"
	if event.Type.String() == "commerce.integration.account_status_changed.v1" {
		reason = "account_status_changed"
	}
	if err := h.queue.EnqueueRecompute(ctx, scope, event.EntityID, reason, event.ID, event.OccurredAt.Time().UTC()); err != nil {
		return eventbus.Retryable("integration_center_recompute_enqueue_failed")
	}
	return nil
}

func integrationCenterNow() time.Time { return time.Now().UTC() }
