# Task 002

Create organization/workspace/store schema and repository ports. Enforce tenant-scoped lookup and test cross-tenant denial.

## Status

Completed on 2026-08-09.

## Acceptance

- additive forward migration creates stores/business units and adds lifecycle,
  timestamp, optimistic-version, sortable-ID, and composite tenant invariants;
- PostgreSQL enables and forces default-deny RLS for tenant-owned foundation
  tables using transaction-local organization/workspace settings;
- domain types accept only canonical UUIDv7/ULID identifiers and make a valid
  organization/workspace scope mandatory;
- the repository port and PostgreSQL adapter include both tenant predicates in
  every lookup and never use a process/global tenant variable;
- cross-tenant and nonexistent lookups return the same opaque `ErrNotFound`;
- scope-application failure, cancellation, corrupt rows, invalid IDs, and
  important database/migration invariants have deterministic tests;
- the tenancy JSON Schema has synthetic accepted/rejected contract fixtures;
- tenancy, threat-model, database, and migration/repair documentation is
  updated; no API endpoint is added by this task;
- format, test, race, vet, semantic contract, migration static, supply-chain,
  Compose static, and build checks pass. PostgreSQL runtime migration evidence
  is required when a daemon/test database is available; absence is reported.

## Validation evidence

- `./scripts/check.sh`, root and contract-checker race tests, `go vet`, format,
  shell syntax, Compose static validation, supply-chain policy, and all four
  command builds pass with Go 1.26.5 and `GOTOOLCHAIN=local`.
- Fifty repeated domain/adapter test runs pass, including invalid and mixed
  scopes, cross-tenant denial, cancellation, scope-application failures, and
  corrupt persisted records.
- `scripts/check-tenancy-postgres.sh` passes against the digest-pinned
  PostgreSQL 18 Alpine image. It applies both migrations, verifies validated
  tenant FKs and forced RLS using a non-owner/NOBYPASSRLS role, proves same-
  tenant access and cross-tenant/mixed-scope denial, proves transaction-local
  scope reset, and proves atomic rollback for incompatible legacy IDs.
- The semantic contract checker accepts the synthetic valid tenancy fixture
  and rejects the targeted UUIDv4 fixture.
- The pinned live source-security gate passes with no vulnerability, SAST,
  secret, license, or misconfiguration policy finding. Because this workspace
  has no usable Git metadata, the evidence explicitly records
  `source_verified:false` and does not claim hosted-source provenance.
