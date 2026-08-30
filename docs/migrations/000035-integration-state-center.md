# Migration 000035 — Integration state center

Migration 35 is an expand-only, high-risk change for Task 168. It adds a
rebuildable tenant-scoped projection of integration snapshots, immutable status
transitions and operator-action receipts, plus a coalescing recompute queue.
Account, health, capability, runtime, sync and reconciliation tables remain the
authoritative sources; the new rows contain only bounded normalized evidence,
digests and opaque references.

Every table uses the `(organization_id, workspace_id)` composite key and
`FORCE ROW LEVEL SECURITY`. Snapshot, transition and action evidence is
immutable at the database trigger boundary. Queue leases are short-lived,
reclaimed after expiry, retried with bounded backoff and moved to
`dead_letter` after 20 attempts. No credential bytes, raw provider response or
customer PII is copied.

Before applying on the small VPS Compose deployment, make the usual verified
PostgreSQL backup checkpoint. Deploy migration 35 before the API/worker binary:
the worker consumer writes only after the queue table exists. Rollback is
application-first: stop the center recompute component and retain the derived
evidence; rebuilding it from authoritative sources is safe after recovery.
