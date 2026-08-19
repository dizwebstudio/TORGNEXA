# Migration 000023 — Upload Security Pipeline

Task `088b` completes the quarantine-first upload lifecycle introduced by migration `000022`.

## Schema

The migration adds append-only `upload_security_evidence` under forced tenant/workspace RLS. Every evidence row binds the immutable upload SHA-256/size to:

- policy version and attempt number;
- detected MIME and normalized extension;
- bounded machine-code validation checks;
- scanner name/engine/signature version;
- scanner status and hashed threat code where applicable;
- clean/rejected/error decision and reason code;
- UTC decision timestamp.

Raw file content, filenames, credentials and scanner logs are not evidence fields. UPDATE/DELETE/TRUNCATE are rejected by both privileges and triggers.

`uploads.security_evidence_id` gains a same-tenant foreign key to the current immutable decision. The Task-088a foundation trigger is replaced by `uploads_security_guard_update`.

## State machine

Allowed transitions are:

`RECEIVED -> QUARANTINED`

`QUARANTINED -> VALIDATED | REJECTED`

`VALIDATED -> SCANNING`

`SCANNING -> CLEAN | REJECTED`

`CLEAN -> RELEASED`

`CLEAN | REJECTED | RELEASED -> QUARANTINED` only for explicit re-scan.

Re-scan atomically clears `security_evidence_id`, `released_object_key` and `released_at` before any new validation or scanner work. This invalidates previously issued release capabilities when consumers revalidate them through `AccessGate.ValidateReleasedRef`.

## Compatibility

Old readers and Task-088a writers remain valid after the migration: `RECEIVED -> QUARANTINED` is still accepted. A new binary on schema 000022 starts fail-closed because evidence/release operations cannot succeed until migration 000023 exists; ordinary non-upload capabilities remain unaffected.

The migration is high-risk because it changes the upload state guard and release authority, so deployment requires the existing backup checkpoint policy.
