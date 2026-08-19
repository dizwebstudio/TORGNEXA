# Task 060 — Privacy and Data Governance Foundation

Implement the frozen privacy/data-governance capability before domain PII is introduced: canonical data classes, processing-purpose/legal-basis metadata, tenant-scoped retention policy metadata, and default PII redaction for audit/log/support surfaces.

## Dependencies

002, 003, 021

## Acceptance

- [x] Canonical classes are implemented in code/contracts: `public`, `internal`, `confidential`, `personal`, `sensitive_operational`, `secret`.
- [x] Each class carries explicit handling metadata for logs, events, analytics and support tooling; `secret` is forbidden on all four surfaces.
- [x] A tenant-scoped processing-purpose registry records description, legal basis, notice reference, consent reference where applicable, allowed data classes, lifecycle status and optimistic version.
- [x] PII-bearing purposes require a notice reference; `consent` purposes additionally require a consent reference.
- [x] A tenant-scoped retention registry records purpose, data class, retention days, disposition, legal-hold eligibility, lifecycle status and optimistic version.
- [x] Retention metadata cannot be created for a data class not allowed by the active purpose, both in the service and PostgreSQL trigger boundary.
- [x] Registry rows contain policy metadata only and no subject payload/raw PII columns.
- [x] Purpose/retention rows use forced organization/workspace RLS and expose no normal DELETE policy.
- [x] Purpose/retention identity fields are immutable, versions advance exactly by one, and retired purposes cannot be reactivated.
- [x] Retention policies can be retired without deletion; active retention classes cannot be removed from a purpose until the dependent policy is retired.
- [x] Hard-delete/truncate is rejected at the database trigger boundary; normal lifecycle is append/revise/retire.
- [x] Shared privacy redaction recognizes secret fields, common PII field names, email-shaped values and IP addresses without attempting arbitrary identity inference.
- [x] Structured logging and audit summary sanitization consume the privacy redaction boundary; synthetic no-PII-leak tests cover name/email/IP/phone and secret fixtures.
- [x] Migration `000006_privacy_foundation.sql` and migration catalog metadata are present with tenant isolation and security-shape tests.
- [x] Draft 2020-12 contracts and valid/invalid fixtures cover processing-purpose and retention-policy metadata.
- [x] Backup/restore and PostgreSQL runtime rehearsals include privacy-registry preservation and RLS checks.
- [x] Architecture policy/review registers the privacy capability and PostgreSQL adapter without changing frozen pillars.
- [ ] Deployment PostgreSQL smoke must run in an environment with Docker/PostgreSQL available before release qualification.
- [ ] Canonical Go 1.26.5 CI must repeat root test/vet/build and semantic contract checks; this sandbox cannot download the required toolchain/dependencies.

## Scope boundary

Task 060 stores and enforces policy metadata only. Task 061 owns subject access/export/correction/deletion/restriction, legal-hold execution, anonymization, and coordinated propagation across PostgreSQL, ClickHouse, object storage, search and caches.

## Repository status

Repository implementation complete once the listed deterministic checks pass. Operational/deployment qualification remains subject to the existing foundation release gates.
