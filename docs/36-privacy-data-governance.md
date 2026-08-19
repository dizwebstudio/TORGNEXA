# Privacy & Data Governance

TORGNEXA processes customer, employee, counterparty, messaging, order and operational data. Privacy is a platform concern, not a connector concern.

## Task 060 foundation

The repository now has one canonical privacy boundary in `internal/platform/privacy` and one PostgreSQL adapter in `internal/platform/postgres/privacyrepo`.

### Data classes

| Class | PII | Secret | Logs | Events | Analytics | Support |
|---|---:|---:|---|---|---|---|
| `public` | no | no | allow | allow | allow | allow |
| `internal` | no | no | minimize | minimize | allow | minimize |
| `confidential` | no | no | redact | minimize | minimize | redact |
| `personal` | yes | no | redact | minimize | minimize | redact |
| `sensitive_operational` | yes | no | redact | minimize | minimize | redact |
| `secret` | no | yes | forbid | forbid | forbid | forbid |

`contracts/privacy/data-classification.yaml` is the portable classification contract. Provider connectors may only narrow these rules, never weaken them.

### Processing-purpose registry

Every tenant purpose records:

- stable `purpose_key`;
- description;
- legal basis;
- notice reference;
- consent reference when the legal basis is consent;
- explicit allowed data classes;
- active/retired state and monotonic version.

A purpose that allows `personal` or `sensitive_operational` data is invalid without a notice reference. A consent-based purpose is invalid without a consent reference. Registry rows are tenant scoped and contain policy evidence references only, not subject PII.

### Retention registry

Retention is keyed by tenant + purpose + data class and records:

- retention period in days;
- disposition: `delete`, `anonymize`, `archive_then_delete`, or `manual_review`;
- whether a later legal-hold workflow may override normal expiry;
- active/retired state and monotonic version.

A retention policy is rejected unless the referenced active purpose allows that data class. This invariant exists in both the Go service and PostgreSQL trigger boundary.

Task 060 does **not** execute deletion. Task 061 consumes this registry to coordinate access/export/correction/deletion/anonymization/legal-hold workflows across stores.

## Default redaction boundary

`privacy.RedactString`, `privacy.FieldClass`, and `privacy.ValueClass` are fail-safe helpers for untrusted operational metadata. They redact:

- all Task-021 credential/secret fields and credential-shaped values;
- common personal field names such as email/name/phone/address/passport/SNILS;
- sensitive-operational fields such as IP/device/user-agent/geolocation;
- high-confidence email-shaped and IP-address values even under an unknown field name.

The helper deliberately does not attempt arbitrary free-text identity inference. Domain schemas remain authoritative and must attach explicit class/purpose/retention metadata as those domains are implemented.

Structured logging and append-only audit sanitization consume this shared privacy boundary. Raw maps/structs are still not accepted as normal log contracts.

## PostgreSQL invariants

Migration `000006_privacy_foundation.sql` creates `privacy_purposes` and `privacy_retention_policies` with:

- organization/workspace foreign-key scope;
- forced RLS and transaction-local tenant GUC policies;
- SELECT/INSERT/UPDATE only in the application model;
- immutable identity fields;
- exact `version = old + 1` update semantics;
- irreversible purpose retirement;
- database enforcement that retention class is allowed by its active purpose;
- hard-delete and truncate rejection triggers.

The registry intentionally stores no customer email, phone, name, passport, subject payload or other raw PII columns.

## Architecture rules

1. PII must be minimized in Kafka events and ClickHouse.
2. Raw secrets and payment credentials are never analytics data.
3. Data deletion is a coordinated workflow across PostgreSQL, ClickHouse, object storage, search indexes and derived caches.
4. Legal retention may override normal deletion; the reason must be recorded.
5. Production datasets must not be copied into dev/test without approved anonymization.
6. Tests use synthetic reserved-domain/address fixtures; production PII fixtures are forbidden.
7. Provider connectors may not bypass classification, purpose/retention metadata, redaction, audit, or tenant isolation.

See `contracts/privacy/`, `docs/52-data-retention-archival.md`, Task 060, and Task 061.
