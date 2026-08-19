# Task 021 — Secrets Provider

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
- [ ] Deployment PostgreSQL smoke must run in an environment with Docker/PostgreSQL available before release qualification.
- [ ] Canonical Go 1.26.5 CI must repeat root test/vet/build; this sandbox cannot download the required toolchain.

## Repository status

Repository implementation complete. Operational/deployment qualification follows the same release-gate rules as the earlier foundation tasks and does not block starting Task 060 repository work.
