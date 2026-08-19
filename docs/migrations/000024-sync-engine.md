# Migration 000024 — Sync Engine

Task `013` adds durable provider-neutral control state for bidirectional synchronization.

## Schema

The migration creates five tenant-scoped tables under forced RLS:

- `sync_policies` — connector account + canonical entity type, direction and source-of-truth conflict policy;
- `sync_checkpoints` — optimistic remote pull cursor advanced only after a complete page resolves;
- `sync_entity_states` — last synchronized local version, remote revision and canonical payload fingerprint;
- `sync_local_receipts` — append-only outbound event replay receipts;
- `sync_remote_receipts` — append-only inbound remote-change replay receipts.

Receipt history is immutable. Policy and entity-state identities are immutable; mutable fields use monotonic optimistic versions. The connector-account foreign key is tenant-qualified so a policy cannot bind another tenant's connector account.

## Crash/retry model

External remote writes and local domain applications cannot share the PostgreSQL transaction. Task 013 therefore does not claim exactly-once delivery. Each side-effect receives a deterministic idempotency key; after a crash, the same event/page is replayed until the durable receipt/checkpoint is committed. Receipt ID reuse with a different canonical payload fingerprint is a collision and fails closed.

## Compatibility

This is an expand-only migration. Existing readers/writers are unchanged. New binaries on schema `000023` fail closed when Task-013 persistence is unavailable; other platform capabilities remain unaffected. The migration is marked high-risk because durable sync state affects future connector propagation and therefore requires the normal backup checkpoint.
