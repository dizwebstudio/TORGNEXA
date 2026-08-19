# Migration 000007 — transactional outbox

`000007_transactional_outbox.sql` is an atomic high-risk expand migration for Task 008.

It extends the bootstrap `outbox_events` table without rewriting prior migrations or making legacy rows invalid. New Task-008 writers populate `event_envelope`; legacy rows may keep it NULL and are excluded from the new relay until an explicit future backfill/contract decision.

The migration adds ready/lease/publication metadata, tenant-scoped SELECT/INSERT/UPDATE RLS policies, immutable event-body guards, hard-delete/TRUNCATE rejection, safe machine error-code constraints, and relay indexes. Publication state is intentionally at-least-once: a Kafka publish that succeeds before the database acknowledgement can be repeated with the same immutable event ID after lease expiry.

Rollout order is expand migration first, then enable the new enqueuer/relay. Rollback disables the new relay binary but does not delete outbox rows or columns. A release must preserve pending outbox rows through backup/restore and reconcile their publication state after PostgreSQL recovery.
