# ADR 0055: Procurement Core

## Status
Accepted

## Context
Task 052 extends the frozen reporting/settlements/growth/supply-planning pillar.

## Decision
Suppliers reference canonical legal parties; supplier offers and purchase orders use exact Money/Quantity primitives and an explicit purchase-order lifecycle with auditable transitions.

## Alternatives considered
Keep policy only in provider adapters or application handlers was rejected because it would permit inconsistent bypasses and non-deterministic retries.

## Compatibility impact
Additive provider-neutral contracts only; existing connector SDK and public API remain compatible.

## Migration and data impact
Migration `000031_procurement_core.sql` is expand-only, tenant-scoped and preserves old readers/writers.

## Security and privacy impact
Tenant scope is mandatory; secrets and raw sensitive provider payloads are not part of these domain contracts.

## Operational impact
All write workflows are idempotent or append-oriented and have explicit failure semantics suitable for retry/reconciliation.

## Consequences
Later provider/application adapters must consume these host-side rules rather than replicate business policy.
