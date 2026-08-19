# Task 027

Write executable dev/prod backup/restore runbooks and automated PostgreSQL restore smoke in CI or local script.

## Status

Completed repository implementation on 2026-08-09. Environment-specific
staging/production restore qualification remains mandatory before release.

## Acceptance

- development logical backup/restore and production physical backup/PITR are
  executable, fail closed, and never restore over an in-use target;
- provisional tier RPO/RTO, backup/WAL cadence, encryption/immutability,
  separate-failure-domain, retention, credential, privacy, and evidence rules
  are documented without claiming that a synthetic drill qualifies production;
- the automated smoke uses the digest-pinned PostgreSQL image with no network
  or published port, applies all migrations, and uses synthetic tenant data;
- the smoke performs custom-format logical restore and verifies row counts and
  tenant RLS in a new database;
- the smoke takes a physical base backup with streamed WAL and SHA-256 manifest,
  runs `pg_verifybackup`, restores to an inclusive LSN, includes the target
  transaction, excludes a post-target transaction, and preserves tenant RLS;
- a deliberately corrupted base-backup copy is rejected;
- sanitized drill evidence has a strict JSON Schema and accepted/rejected
  fixture, with no credentials, storage URLs, tenant content, or raw logs;
- main CI and release candidates execute the drill; real staging/production
  storage, KMS, credentials, and provider recovery require separately retained
  operational evidence;
- documentation covers PostgreSQL as recovery anchor and records the recovery
  order/ownership of Kafka, ClickHouse, S3/media, identity, secret metadata,
  Valkey, and search without implementing unselected provider tooling;
- format, tests, race, vet, semantic contracts, supply-chain policy, shell
  syntax, Compose static, build, tenancy migration, and restore/PITR runtime
  checks pass.

## Validation evidence

- The final digest-pinned PostgreSQL 18.4 drill passed logical restore,
  `pg_verifybackup`, physical WAL recovery to an inclusive LSN, target/post-
  target boundary checks, RLS checks, and deliberate-corruption rejection.
- The sanitized evidence document passed its strict schema and has SHA-256
  `db1a2fdafb968e8be5ef8ee03154f53e931a873cd2ecd8d9cd475e3ae9cd56d5`.
- `./scripts/check.sh`, both race suites, 100 repeated backup-policy tests,
  semantic contracts, supply-chain policy, shell syntax, Compose static, the
  two-tenant migration/RLS smoke, and all command builds pass with Go 1.26.5.
- The pinned source-security gate passes. This workspace has no usable Git
  metadata, so that evidence records `source_verified:false` and is not hosted
  provenance.
- Synthetic tmpfs evidence proves the repository recovery procedure only. A
  release/deployment still requires a retained isolated restore using its real
  encrypted immutable store, KMS, credentials, WAL archive, and topology.
