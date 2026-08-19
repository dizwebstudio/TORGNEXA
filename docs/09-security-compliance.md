# Security & Compliance

Authentication via Keycloak/OIDC; TORGNEXA enforces RBAC/scopes and later ABAC. Risk classes: READ, WRITE_SAFE, WRITE_SENSITIVE, LEGALLY_SIGNIFICANT.

Secrets use SecretProvider. Enterprise adapters may use Vault/KMS/HSM.

Signing/UKEP is isolated: generic workers/plugins/n8n/MCP create SigningRequest, never receive private key material.

MChD stores representative/powers/validity/verification metadata. EDO providers normalize document lifecycle; provider is authoritative for remote status.

Privileged mutations produce append-only audit records with actor/source/before-after summary/correlation/approval/signing refs. Audit risk uses the canonical classes `read`, `write_safe`, `write_sensitive`, and `legally_significant`.

Task 003 makes audit application writes append-only at every normal boundary: `audit.Service` sanitizes bounded JSON summaries before persistence, `audit.Repository` exposes only `Append`, the PostgreSQL adapter applies transaction-local organization/workspace RLS scope, and migration `000004_audit_base.sql` provides SELECT/INSERT-only forced RLS plus UPDATE/DELETE/TRUNCATE rejection triggers. Raw Authorization values, secret/token/password/private-key fields, and private-key PEM material must never be persisted; common credential shapes are also rejected by database constraints as defense in depth. `unclassified` is reserved only for legacy rows/writers from before Task 003 and cannot be emitted by the new service contract.

Task 021 centralizes provider credentials behind `SecretProvider`. The Community implementation encrypts with AES-256-GCM before PostgreSQL, binds ciphertext to tenant/reference/class/version via AEAD associated data, keeps master keys outside the database, rotates credentials without changing the stable reference, and makes ciphertext versions immutable. Audit/log redaction share the same credential classifier.
