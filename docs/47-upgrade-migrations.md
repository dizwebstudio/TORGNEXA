# Upgrade, Migration & Compatibility

TORGNEXA Community, Cloud and Enterprise deployments must be upgradeable without manual data surgery.

## Repository gate and metadata

`migrations/catalog.json` is the exact, ordered inventory. Each entry binds the
six-digit filename and SHA-256 to its phase, risk, dependencies, embedded
transaction, policy version, history mode, backup requirement, rolling
compatibility, and optional backfill plan. `make migrations` rejects:

- a missing, extra, symlinked, oversized, renamed, reordered, or gapped SQL
  file;
- checksum drift, unknown fields, unsafe dependencies, duplicate job keys, or
  an unbounded batch;
- non-transactional files, missing lock/statement timeouts, nested/dynamic SQL,
  concurrent indexes inside a transaction, cascades, and destructive SQL before
  contract;
- high/critical risk without a backup checkpoint, migrate without an
  idempotent backfill plan, or contract without explicit completion gates.

The pre-v1 repository is now baselined before the first production release. The active runtime catalog contains **11 files** rather than the original 74-file development chain. `000001`–`000003` remain byte-for-byte bootstrap migrations because they precede/create `migration_history`; `000004`–`000011` are deterministic statement-order-preserving baseline components and use `history_mode: atomic`.

The original `000001`–`000074` chain is immutable evidence under `migrations_legacy_pre_v1/` and is never used for a fresh install. `migrations/baseline-manifest.json` binds the compact files to the archived source versions and checksums. `make migration-baseline` rejects source or generated drift.

An existing **development** database already at legacy head 74 must use the reviewed one-time pre-v1 rebaseline procedure described in `docs/75-pre-v1-migration-baseline.md`. It first proves all 74 applied version/name/checksum rows, archives them to `migration_history_legacy_pre_v1`, and only then stamps the 11 active baseline entries plus `migration_baseline_evidence`. This is the only authorized pre-v1 rewrite; after production v1, applied migration history is immutable.

Every active atomic migration inserts version/name/file/phase/risk/checksum, application version, execution ID, and duration into `migration_history` inside the same transaction. Those values come from reviewed non-secret session settings supplied by the migrator; missing settings make the migration fail. New post-baseline files start from `templates/migration.sql.tmpl`; do not edit an applied baseline file or hard-code its checksum into itself.

At startup/upgrade, `migration.Plan` compares database history to the active catalog. History must be a contiguous exact prefix after any required pre-v1 rebaseline. Changed checksums, renamed entries, gaps, or a database version newer than the binary block startup and downgrade.

## Expand / migrate / contract

### Expand

- Add nullable/default-safe columns, new tables/indexes, versioned contracts,
  and dual-compatible code paths.
- Old readers and writers continue working; the new binary can run against the
  previous schema during rolling deployment.
- Do not remove/rename old columns, tighten a populated constraint in one step,
  invent business values, or start an unbounded backfill.

### Migrate

- Run only after the expand binary/schema is stable. Old readers/writers remain
  compatible throughout the window.
- Declare one bounded backfill job with stable lexicographic/monotonic cursor,
  batch size, tenant scope, and `idempotency: required`.
- Observe checkpoint, attempts, processed count, lease generation/expiry,
  sanitized error code, row/lag rate, and remaining work. Pause rather than
  overwhelming foreground traffic.
- Reconcile all tenants and dual-read/dual-write comparisons before declaring
  completion. Completion is a durable precondition, not an operator memory.

### Contract

- Use a separate release/migration after every named backfill, traffic, minimum
  binary, plugin/connector, and retention precondition has been verified.
- Old-reader, old-writer, and new-on-old compatibility flags are false, so a
  contract migration cannot be deployed in a mixed-version fleet.
- A recent tested backup is mandatory. Destructive operations remain minimal,
  lock-bounded, explicitly reviewed, and forward-repairable.

## Resumable backfill runner

`migration.Runner` claims at most one batch per scheduler invocation. The
`backfill_jobs` row contains an explicit global or organization/workspace scope,
stable checkpoint, bounded batch size, state, lease owner/expiry, monotonic
fencing generation, attempts, processed count, safe error code, optimistic
version, and timestamps.

`postgres/backfillrepo.Repository` is the driver-neutral `database/sql`
adapter. Registration and claim share a row-locking transaction; immutable job
key/scope/batch metadata must match. Commit/fail updates include job ID/key,
both tenant IDs (or explicit global `NULL` scope), batch size, worker,
generation, state, and unexpired lease predicates. Any affected-row count other
than one is `ErrLeaseLost`.

Commit/fail operations compare job, worker, and fencing generation. A worker
whose lease was reclaimed cannot commit. A process crash before checkpoint
commit repeats the last batch, so processors must use idempotent writes/upserts;
remote or externally visible side effects still require their normal durable
outbox/inbox/idempotency mechanism. Checkpoints contain only stable IDs/cursors,
never payloads, PII, credentials, tokens, or raw errors. Processor errors and
panic values are reduced to fixed codes.

Tenant backfills carry both organization and workspace and use explicit
predicates/tenant context. Global jobs run only under a separate reviewed
migration identity. Application roles receive no rights to `migration_history`
or `backfill_jobs` by default.

## Upgrade procedure

1. Complete the migration plan template, compatibility/contract impact, privacy
   classification, capacity/lock estimate, observability, and repair steps.
2. Run `make migrations`, contracts, tests/race/vet, and the static supply-chain
   gate. Review the immutable checksum change.
3. For high/critical or contract work, create and restore-verify the Task 027
   environment backup checkpoint; record its evidence digest and target.
4. Rehearse oldest-supported schema/application → new expand schema, the
   interrupted/retried backfill, mixed-version reads/writes, fresh install, and
   (when applicable) later contract. Compare schema and domain invariants.
5. Deploy expand, monitor, then run migrate in bounded batches. Stop on tenant
   leakage, checksum/history drift, lock/SLO breach, invalid rows, or an
   unclassified failure.
6. Ship contract separately only after durable gates pass. Release notes state
   minimum source version, expected duration/capacity, incompatibilities,
   backup evidence, and rollback/forward-repair decision.

`make upgrade-runtime` performs the repository rehearsal on a digest-pinned,
no-network PostgreSQL container. It validates the compact baseline/bootstrap path and atomic post-framework migration behavior,
preserves the old read/write shape across the expand boundary, verifies immutable history/access controls,
rolls back an interrupted batch, rejects a stale fencing generation, resumes to
completion, and compares the upgraded schema to a clean install.

That deterministic rehearsal is a repository gate, not deployment evidence.
Before a release, repeat it from every supported deployed source version with
the target environment's extensions, representative synthetic/anonymized data,
real backup checkpoint, capacity limits, and mixed-version fleet. Retain the
catalog and backup evidence digests with the release record.

## Failure and recovery

- Before contract, prefer stopping the new binary/backfill and repairing
  forward while the old compatible path remains available.
- Never silently coerce invalid tenant/financial/compliance data or use a down
  migration that drops evidence. A repair is a new reviewed migration/job.
- If correctness cannot be preserved, isolate traffic and use the verified
  Task 027 restore/PITR procedure. Reconcile outbox/derived systems afterward.
- An unavailable backup, checksum/history mismatch, unknown applied version,
  failed rehearsal, incomplete backfill, unvalidated constraint, or missed
  compatibility gate blocks release.

Events, REST, webhooks, protobuf, and plugins independently follow
`contracts/sdk/compatibility-policy.md`; a database plan cannot authorize a
breaking public-contract mutation.
