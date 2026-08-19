# Migration 000008 — consumer inbox / idempotency

`000008_inbox_idempotency.sql` is an atomic high-risk expand migration for Task 009.

## Expand strategy

The bootstrap `inbox_events` table predates the real tenant contract and was forced-RLS deny-all. This migration deliberately left that placeholder in place for old-reader/writer compatibility and created a new `inbox_receipts` table for Task-009 runtime code. After supported fleets stopped referencing it, contract migration `000064_retire_legacy_inbox_events.sql` retired the empty placeholder with a fail-closed validation guard.

`inbox_receipts` contains only organization/workspace, stable logical consumer, event ID/type, a lowercase SHA-256 fingerprint of the canonical EventBus envelope, first-observed/processed timestamps, and the committed delivery attempt. The event payload and arbitrary handler errors are not copied into inbox storage.

## Idempotency invariant

Task-009 processing binds tenant GUCs and acquires a transaction-scoped PostgreSQL advisory lock derived from `(organization, workspace, consumer, event_id)`. Existing matching receipt means duplicate success. Different type/fingerprint for the same identity is a collision. With no receipt, the handler performs PostgreSQL side effects and the final receipt is inserted in the same transaction.

No durable processing lease or `in_progress` row exists. Rollback, process death, or connection loss releases the advisory lock automatically and leaves no committed receipt. After commit, the immutable receipt makes redelivery duplicate-safe.

This guarantee covers only effects inside that PostgreSQL transaction. Direct provider/HTTP side effects require provider idempotency or another transactional outbox event.

## Security and tenancy

- composite workspace FK prevents mixed organization/workspace ownership;
- forced RLS exposes only tenant-matching `SELECT` and `INSERT`;
- application runtime has no update/delete policy;
- UPDATE/DELETE/TRUNCATE are additionally rejected by trigger;
- receipt identity is `(organization_id, workspace_id, consumer, event_id)`;
- fingerprint validates as 64 lowercase hexadecimal characters;
- consumer/event/type/attempt metadata is bounded.

## Verification

Run the PostgreSQL tenancy and backup/restore rehearsals after migration. Verify two synthetic tenants see only their own receipts, application mutation fails, logical restore retains both receipts and RLS, and PITR retains receipts. The exact Go consumer crash/redelivery behavior is covered by Task-009 unit tests and must also be exercised in staging with the real PostgreSQL/Kafka composition.
