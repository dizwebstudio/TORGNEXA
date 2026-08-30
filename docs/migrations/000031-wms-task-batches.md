# Migration 000031 — WMS task batches

Migration `000031_wms_task_batches.sql` adds the tenant-scoped storage for
170.7 local pack handoff batches. It is an additive expand migration and does
not alter the existing task/order/allocation tables.

Only completed pick tasks from one active warehouse may be grouped. The batch
and its task membership are bounded to 50 entries; batch events are immutable,
idempotent and protected by forced RLS. A handoff changes only the local batch
state and produces audit/outbox evidence. It does not create a marketplace
shipment, label, external status write or automatic on-hand consumption.

The migration is high risk and requires the normal verified PostgreSQL backup
checkpoint before rollout. Rollback is by restore or forward remediation; an
application command never deletes batch history.
