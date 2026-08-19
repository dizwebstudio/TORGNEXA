# Migration 000003 — upgrade and resumable backfill framework

## Purpose

Add immutable applied-migration metadata and durable, fenced backfill
checkpoints without changing existing commerce/tenancy tables or application
read/write shapes.

## Phase and compatibility

This is an additive `expand` migration. Old readers/writers continue using the
000002 schema, and the new binary can inspect an older schema before the
framework is installed. It creates only `migration_history`, `backfill_jobs`,
their checks/index, comments, and default-deny privileges.

Migrations 000001–000003 are the one-time `bootstrap` catalog prefix. After the
framework schema and postconditions are verified, the upgrade controller writes
their exact catalog rows once. From 000004 onward, an `atomic` migration writes
its own history row inside its embedded transaction using runner-supplied
catalog checksum/application/execution metadata. An absent/mismatched setting
must abort instead of creating unverifiable history.

## Backfill invariants

- Job IDs are UUIDv7/ULID-compatible; job keys are stable safe identifiers.
- Scope is either explicit global or a complete organization/workspace pair
  protected by the composite workspace FK. Partial/mixed scope is rejected.
- A running job has one owner and expiry. `lease_generation` increments on each
  claim/reclaim and fences stale workers.
- Checkpoints are at most 512 bytes and contain no controls/payloads/secrets.
- Counts, attempts, versions, timestamps, completion state, and error codes are
  bounded/consistent. Global `NULL` scope uniqueness uses `NULLS NOT DISTINCT`.
- `PUBLIC` has no privileges. Application roles are not granted access; global
  migration work uses a separate reviewed operational role.

## Rehearsal and interruption

`scripts/check-postgres-upgrade.sh` starts from migrations 000001–000002 with
synthetic tenant rows, proves the old join/read shape, applies this migration,
and records the exact bootstrap history. It then:

1. commits a first bounded batch and checkpoint;
2. processes a second batch inside a transaction that is deliberately rolled
   back, proving rows and checkpoint both remain at the last commit;
3. expires/reclaims the lease and proves generation 1 cannot update generation
   2/3 state;
4. retries with idempotent upserts and completes exactly five source rows;
5. rejects duplicate global jobs and partial tenant scopes;
6. compares upgraded schema columns with a fresh all-migrations install and
   verifies every constraint is validated.

The Go runner separately tests a crash after retry-idempotent processor effects
but before checkpoint commit: the same cursor is delivered again and duplicate
effects remain collapsed.

The PostgreSQL repository adapter atomically registers/locks/claims a job and
uses explicit organization/workspace predicates plus worker, generation, state,
and lease-expiry fencing for every commit/fail. Stale or mismatched metadata
returns a closed failure rather than silently creating another checkpoint.

## Rollback / repair

The migration is transactional and additive. On failure it rolls back. After a
successful deployment do not drop history/checkpoint tables or edit history;
stop the new runner/binary and repair forward. Before any later contract phase,
retain the verified Task 027 backup/PITR checkpoint.
