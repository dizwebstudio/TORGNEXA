# Secrets Management

Task 021 establishes `internal/platform/secrets.SecretProvider` as the only application abstraction for provider credentials. Connector and business tables store an opaque `sec:v1:<random>` reference. They never store an access token, refresh token, password, client secret, signing secret, private key, or other provider credential in a normal text column.

## Secret classes

The baseline classes are connector tokens, OAuth client material, OAuth refresh material, ERP credentials, webhook signing secrets, certificate/private material, and storage credentials. New classes require an explicit contract review rather than free-form labels.

## Local Community provider

`LocalEncryptedProvider` encrypts each version with AES-256-GCM before calling the repository. The AEAD associated data binds ciphertext to the secret reference, organization, workspace, class, version, and master-key identifier. Copying ciphertext to another tenant/reference/version therefore fails authentication.

The master key is supplied through `MasterKeySource` from outside PostgreSQL. `StaticKeyring` is the local baseline for material already obtained from a secret mount, environment-injection layer, or equivalent deployment secret source. PostgreSQL stores only a non-secret `key_id`; it has no master-key column. Enterprise implementations may replace the key source/provider with Vault, KMS, HSM, or remote-signing adapters while preserving `SecretProvider`.

Master keys are exactly 32 bytes. A keyring may retain old keys by ID while selecting a new current key, which allows existing ciphertext to remain readable while new/rotated secret versions move to the new master key. Removing an old key is a separate controlled re-encryption/decommission ceremony.

## Plaintext lifetime

`SecretProvider.Use` decrypts into a provider-owned byte buffer, invokes a callback, and wipes that buffer immediately afterward. It deliberately does not return a string and converts callback failures to a fixed `ErrUseFailed` so a caller cannot accidentally propagate a credential in an error message. Consumers still must not copy material into logs, audit, events, traces, panic messages, analytics, or durable queues.

Shared `secrets.SensitiveKey`, `secrets.SensitiveString`, `secrets.RedactText`, and `secrets.RedactedValue` helpers are used by logging/audit boundaries so secret-shaped fields and common authorization/private-key values share one baseline classification.

## Persistence and rotation

Migration `000005_secrets_provider.sql` creates two tenant-scoped tables:

- `secret_references`: stable opaque metadata (`class`, `status`, `current_version`);
- `secret_versions`: immutable AES-GCM ciphertext versions (`algorithm`, external `key_id`, nonce, ciphertext).

Rotation inserts a new immutable ciphertext row and atomically advances `current_version`. The opaque reference is unchanged, so connector accounts do not need to be recreated. Reference identity/tenant/class are immutable, version movement is monotonic by one, revoked references cannot be reactivated, and ciphertext version rows reject update/delete/truncate. Revocation is idempotent and prevents future `Use`.

Both tables use forced organization/workspace RLS. The application role requires `SELECT/INSERT/UPDATE` on `secret_references` and only `SELECT/INSERT` on `secret_versions`; it must not receive delete/truncate privileges or `BYPASSRLS`.

## Rules

- Database/application contracts persist opaque references, never plaintext credential columns.
- Raw secrets are prohibited in Kafka/events, audit summaries, traces, analytics, logs, webhooks, generic backup metadata, and migration checkpoints.
- Backups may contain encrypted ciphertext/reference metadata; the master-key recovery path is backed up and tested separately from PostgreSQL.
- Rotation of provider credentials uses the stable reference; master-key rotation uses key IDs and a controlled re-encryption/decommission process.
- Error messages must describe the operation/class/reference state, never credential bytes.
- Production deployments should prefer secret mounts/Vault/KMS/HSM over long-lived environment variables when the platform supports them.

The public metadata contract is `contracts/secrets/secret-reference.schema.json`. It contains no material/ciphertext fields.
