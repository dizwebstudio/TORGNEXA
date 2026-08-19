# Task 111: Production API adapters

## Status

`repository-complete` — 2026-08-16.

## Objective

Replace the nine authenticated OpenAPI compatibility placeholders with
production application adapters backed by the existing canonical domain and
persistence boundaries.

## Dependencies

014, 058, 061, 078, 081, 086, 088, 089, 092b, 107

## Acceptance

- all nine operations pass the mandatory authn/tenant/authz composition and no
  longer return a hard-coded `501`;
- reads are bounded and tenant-scoped where the underlying data is tenant data;
- retryable POST operations require an idempotency key and derive stable scoped
  identities;
- reconciliation rechecks the enabled sync policy and current cabinet
  capability grant;
- upload bytes enter S3-compatible tenant-derived quarantine and never become a
  released object through this endpoint;
- Community mode does not depend on a Cloud Billing subscription row;
- OpenAPI, route-parity tests, architecture inventory and operator docs match
  runtime behavior.

## Implementation evidence

- `internal/app/api/contract_operations.go` owns the nine adapters and their
  bounded public projections.
- PostgreSQL readers were completed for settlements, Cloud subscription state,
  immutable FX facts and tenant-visible plugin marketplace listings.
- upload admission uses the existing Task-088 state/outbox repository plus a
  bounded AWS SigV4 S3-compatible quarantine adapter.
- privacy intake uses Task-061 durable workflow/audit semantics; execution
  remains fail-closed behind the configured workflow target topology.
- OpenAPI `0.11.0` removes all nine `501` responses and types the inputs and
  outputs.
