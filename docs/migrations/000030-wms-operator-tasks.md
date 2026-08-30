# Migration 000030 — WMS operator tasks

Migration `000030_wms_operator_tasks.sql` adds the production WMS execution
tables for Task 170. It is an additive expand migration. The legacy
`wms_tasks`/`wms_task_events` tables remain untouched for compatibility with
the pre-v1 reference implementation.

The new tables are forced-RLS and keyed by organization/workspace. A task may
link an order item and its fulfillment allocation; expected and processed
quantities use exact coefficient/scale/unit fields. Task events are immutable,
idempotent and retain only a SHA-256 barcode digest, not the scanned value.

Before applying the migration, the normal production rollout must create the
backup checkpoint required by migration catalog risk `high`. Rollback is by
restoring the verified PostgreSQL backup or by forward remediation; the
append-only event history is never deleted by an application command.
