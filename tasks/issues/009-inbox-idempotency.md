# Task 009 — Inbox / Idempotency

Status: repository-completed (2026-08-09)

Implement consumer inbox helper; test duplicate delivery and crash/retry.

## Acceptance

- `inbox` defines stable logical-consumer validation and a deterministic SHA-256 fingerprint of the canonical immutable EventBus envelope; retry/backoff metadata is excluded from identity.
- `inboxrepo.Processor` owns one PostgreSQL transaction containing tenant scope, duplicate serialization/check, business PostgreSQL side effects, and final receipt insert.
- Duplicate delivery of the same tenant/consumer/event ID and identical envelope returns success without invoking business code.
- Reuse of the same tenant/consumer/event ID with different event type/content fails closed as an event-ID collision and is mapped to a permanent EventBus failure.
- Concurrent deliveries serialize with a transaction-scoped advisory lock; no durable in-progress row, lease, or cleanup state is required.
- Crash/handler failure before commit leaves neither committed PostgreSQL side effects nor a receipt; retry can execute again. Crash after commit leaves an immutable receipt so redelivery skips side effects.
- `inbox_receipts` is tenant scoped with forced RLS, immutable after insert, payload-minimal, backup/restore covered, and introduced by additive migration `000008_inbox_idempotency.sql`; Task 112 later retires the qualified empty `inbox_events` compatibility placeholder through contract migration `000064`.
- The inbox guarantee applies only to side effects inside the owned PostgreSQL transaction. Direct external HTTP/provider effects require their own idempotency key or an outbox and are not described as distributed exactly-once.
- Unit, migration, RLS/tenancy, contract, architecture, crash/retry, and repository regression checks are updated.

## Follow-up boundary

Tasks 004–006 can now implement mutating commerce consumers on top of the completed `EventBus → Transactional Outbox → Inbox/Idempotency` correctness chain. Task 014 later adds business reconciliation for remote/provider drift; it does not replace inbox deduplication.
