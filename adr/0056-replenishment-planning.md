# ADR 0056: Demand & Replenishment Planning

## Status
Accepted

## Context
Task 053 extends the frozen reporting/settlements/growth/supply-planning pillar.

## Decision
Replenishment is advisory by default. Every recommendation pins an immutable input snapshot digest and algorithm version, and no recommendation can auto-send a purchase order.

## Alternatives considered
Keep policy only in provider adapters or application handlers was rejected because it would permit inconsistent bypasses and non-deterministic retries.

## Compatibility impact
Additive provider-neutral contracts only; existing connector SDK and public API remain compatible.

## Migration and data impact
Migration `000032_replenishment_planning.sql` is expand-only, tenant-scoped and preserves old readers/writers.

## Security and privacy impact
Tenant scope is mandatory; secrets and raw sensitive provider payloads are not part of these domain contracts.

## Operational impact
All write workflows are idempotent or append-oriented and have explicit failure semantics suitable for retry/reconciliation.

## Consequences
Later provider/application adapters must consume these host-side rules rather than replicate business policy.
