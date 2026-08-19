# Task 067: Upgrade and migration framework

## Objective
Implement migration metadata, expand/migrate/contract conventions, resumable backfill runner and upgrade rehearsal.

## Dependencies
002, 024, 027

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance

- strict checksum catalog covers every SQL migration exactly once and rejects
  gaps, unsafe paths/symlinks, drift, invalid dependencies, and unknown fields;
- phase metadata enforces rolling-compatible expand, bounded/idempotent migrate,
  and separately gated/backup-verified contract behavior;
- v1 SQL policy requires one explicit transaction and bounded lock/statement
  timeouts, rejects dynamic/unsafe/destructive pre-contract constructs, and
  permits only the explicit migration-1 legacy exception;
- database migration history records exact catalog identity and blocks checksum
  drift, history gaps, unknown newer versions, and implicit downgrade/repair;
- future migrations record history atomically inside their SQL transaction;
  the explicitly bounded 000001–000003 adoption prefix is rehearsed and
  bootstrapped only after postcondition validation;
- backfill metadata stores explicit global or organization/workspace scope,
  stable bounded cursor, state, attempts/counts, safe error code, expiry,
  monotonic fencing generation, timestamps, and optimistic version;
- runner processes at most one bounded batch, requires retry-idempotent
  processors, sanitizes error/panic content, rejects invalid progress, and
  prevents stale-lease commit;
- PostgreSQL adapter atomically registers/claims jobs, validates immutable
  job/scope/batch metadata, and predicates commit/fail on exact tenant scope,
  worker, generation, state, and unexpired lease without adding a DB driver;
- deterministic tests cover interruption after effects/before checkpoint,
  same-cursor retry, transaction rollback, expired-lease reclaim, completion,
  cancellation, invalid scope/cursor/bounds, and duplicate jobs;
- digest-pinned PostgreSQL rehearsal upgrades 000002→000003 without breaking the
  old read shape, validates history/access, resumes a backfill, and proves
  upgraded/fresh schema parity with all constraints validated;
- migration catalog JSON Schema/fixtures, ADR, database/testing/upgrade docs,
  migration notes, and the required plan template are updated;
- main CI/release candidates run catalog validation and the runtime rehearsal;
  format, tests/race/vet, semantic contracts, supply-chain policy, shell syntax,
  Compose, migration/restore/upgrade runtime, and build checks pass.

## Status

Completed at repository level on 2026-08-09. The immutable catalog, migration
history, resumable/fenced backfill runner and PostgreSQL adapter, contracts,
documentation, CI gates, and digest-pinned upgrade rehearsal satisfy the task
acceptance criteria. Each release environment must still rehearse every
supported source version with its real topology, extensions, backup evidence,
data invariants, and mixed-version fleet; the repository rehearsal is not a
deployment qualification.
