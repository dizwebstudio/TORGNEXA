# ADR 0054: Advertising Engine

## Status
Accepted

## Context
Task 050 extends the frozen reporting/settlements/growth/supply-planning pillar.

## Decision
Advertising mutations are provider-neutral commands with hard action/budget ceilings, explicit attribution metadata, dry-run preview and approval references; connector transports cannot bypass host guards.

## Alternatives considered
Keep policy only in provider adapters or application handlers was rejected because it would permit inconsistent bypasses and non-deterministic retries.

## Compatibility impact
Additive provider-neutral contracts only; existing connector SDK and public API remain compatible.

## Migration and data impact
Migration `000030_advertising_engine.sql` is expand-only, tenant-scoped and preserves old readers/writers.

## Security and privacy impact
Tenant scope is mandatory; secrets and raw sensitive provider payloads are not part of these domain contracts.

## Operational impact
All write workflows are idempotent or append-oriented and have explicit failure semantics suitable for retry/reconciliation.

## Consequences
Later provider/application adapters must consume these host-side rules rather than replicate business policy.
