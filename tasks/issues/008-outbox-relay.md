# Task 008 — Transactional Outbox

Status: repository-completed (2026-08-09)

Implement outbox repository/relay with same-transaction enqueue, locking, retry and duplicate-safe publication metadata.

## Acceptance

- `outbox.Enqueuer` can be bound to the caller-owned PostgreSQL transaction; the adapter never starts or commits a second transaction.
- Enqueue validates the canonical EventBus envelope, verifies the transaction-local tenant scope, and stores domain event intent atomically with the caller's domain write.
- Exact duplicate enqueue is idempotent only when the immutable event envelope is identical; event-ID collisions with different content fail closed.
- Migration `000007_transactional_outbox.sql` keeps legacy rows expand-compatible; the relay fails closed if unpublished legacy rows require an explicit migration, while adding canonical envelopes, ready time, lease metadata, publication attempts, safe error codes, forced tenant RLS, immutable body guards, and hard-delete protection.
- Concurrent relay claims use tenant-scoped `FOR UPDATE SKIP LOCKED` plus opaque expiring compare-by-lease tokens; no database transaction remains open during EventBus/Kafka publication.
- Relay uses bounded exponential retry without a silent max-attempt discard and persists only a fixed machine error code, never raw broker/client error text.
- `published_at` or retry reschedule succeeds only for the current unexpired lease. A stale worker cannot acknowledge a re-leased row.
- Event IDs/bodies remain unchanged across publication attempts. Crash after publish/before acknowledgement may duplicate delivery by design; Task 009 Inbox/Idempotency remains mandatory.
- Unit, migration, tenancy/RLS, contract, architecture, and repository regression checks are updated.

## Follow-up boundary

Task 009 supplies consumer inbox/idempotency for duplicate-safe side effects. Tasks 004–006 then use the outbox in real commerce transactions; their use is audited again by stage 076b where Money/Quantity/UTC primitives are verified end to end.
