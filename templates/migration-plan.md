# Migration Plan

## Metadata

- Catalog version / filename / SHA-256:
- Phase (`expand`, `migrate`, or `contract`):
- Risk / owner / approver:
- Dependencies / minimum source release:
- Transaction, lock timeout, statement timeout:
- Backup required (`true` for high/critical/contract):

## Current / target state

- Authoritative tables/invariants and data classification:
- Current row/tenant/cardinality/storage profile:
- Target schema and why it is necessary:

## Compatibility

- Old readers supported:
- Old writers supported:
- New binary on old schema supported:
- API/event/webhook/plugin compatibility impact:
- Mixed-version and downgrade-block behavior:

## Expand phase

- Additive DDL and old/new application behavior:
- Lock/capacity/index analysis:
- Synthetic invalid legacy-data preflight:

## Backfill / migrate phase

- Job key / global or organization+workspace scope:
- Stable cursor / batch size / idempotent write strategy:
- Crash-after-side-effect and crash-before-checkpoint behavior:
- Attempts, lease/fencing, progress, rate, remaining-work metrics:
- Dual-read/write reconciliation and durable completion evidence:

## Contract phase

- Separate release/migration identifier:
- Required completed backfills, minimum binary, traffic, plugin, and retention gates:
- Destructive statements and lock estimate:

## Observability and abort thresholds

- Query/lock/replication/outbox/SLO signals:
- Tenant/data-integrity canaries:
- Safe error codes and alert owner:

## Rollback / forward repair

- Pre-contract binary/backfill stop procedure:
- Forward repair migration/job:
- Conditions requiring isolation and PITR:

## Backup checkpoint and rehearsal

- Task 027 evidence digest / base-WAL target / restore result:
- Old → new rehearsal result:
- Interrupted/resumed backfill result:
- Upgraded/fresh schema parity result:
- Release-note/operator handoff:
