# Migration 000026 — audit realtime lookup index

The authenticated SSE invalidation channel only needs the newest audit ID for
the current tenant. This migration adds a covering lookup path ordered by
`(organization_id, workspace_id, id DESC)`, so each connected client avoids a
full audit-row/JSON-summary scan on every polling tick.

The index is additive and does not change the append-only audit contract or
tenant RLS policy.
