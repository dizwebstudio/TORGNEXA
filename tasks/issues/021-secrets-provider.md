# Task 021 — Secrets Provider

## Status

`complete` — repository implementation and canonical CI/PostgreSQL
qualification are complete. Target-production secret-backend, rotation and
recovery evidence remains part of the P4 deployment gate rather than this
foundation task.

Define `SecretProvider`, opaque secret references, shared redaction helpers, rotation/revocation model, and the local encrypted Community implementation. No plaintext token/credential columns.

## Acceptance

- [x] `internal/platform/secrets.SecretProvider` is the only application abstraction for secret material.
- [x] Stable opaque `sec:v1:<random>` references are safe to persist/log/audit and are tenant scoped.
- [x] Local Community provider encrypts at rest with AES-256-GCM before repository persistence.
- [x] AEAD associated data binds organization, workspace, reference, class, version, and external key ID.
- [x] Master keys are supplied only through `MasterKeySource`; PostgreSQL stores `key_id`, never master-key material.
- [x] `Use` bounds plaintext lifetime, wipes provider-owned plaintext memory, and does not propagate consumer error text.
- [x] Provider credential rotation preserves the stable reference and advances immutable ciphertext versions atomically.
- [x] Revocation is idempotent and irreversible through the normal application/database lifecycle.
- [x] Shared redaction helpers are consumed by structured logging and audit sanitization.
- [x] Migration `000005_secrets_provider.sql` adds tenant-scoped `secret_references`, immutable `secret_versions`, and a tenant-bound optional `connector_accounts.secret_reference`.
- [x] No plaintext password/token/access-token/refresh-token/client-secret/secret-value/master-key columns are introduced.
- [x] Forced RLS prevents cross-tenant reference/ciphertext access; version mutation/delete/truncate is blocked.
- [x] JSON Schema contract and valid/invalid fixtures cover safe reference metadata.
- [x] Architecture policy/review registers the secrets capability and PostgreSQL adapter without changing frozen pillars.
- [x] Unit/static migration checks pass in the repository-compatible local toolchain run.
- [x] Canonical CI runs the disposable digest-pinned PostgreSQL smoke, including
  full migrations, forced tenant RLS and secret rotation/revocation invariants.
- [x] Canonical Go 1.26.7 CI repeats root test/vet/build through
  `scripts/check.sh` with `GOTOOLCHAIN=local`.

## Repository status

Repository implementation and its required CI qualification are complete. The
2026-09-13 canonical CI run passed repository checks, PostgreSQL tenancy/RLS,
logical restore and physical PITR. Live production secret-management posture
is still verified by Task 118/P4 and is not inferred from this result.
