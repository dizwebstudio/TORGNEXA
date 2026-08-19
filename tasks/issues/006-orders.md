# Task 006

Normalized Order/OrderItem lifecycle and external mappings; provider status translation stays in connector.

## Repository status

Completed.

## Implemented

- Canonical immutable `Order` and `OrderItem` commercial snapshots.
- Normalized lifecycle: `pending`, `confirmed`, `processing`, `fulfilled`, `cancelled`.
- Canonical Task-076 Money, Decimal/Quantity and TaxCategory primitives plus explicit TaxSnapshot facts; no floating-point commerce state.
- PostgreSQL `ordersrepo`, optimistic versions, forced RLS, tenant FKs, lifecycle and totals guards.
- Atomic Order state + Audit + Transactional Outbox event intent.
- Additive `commerce.orders.order_changed.v1`; legacy `order_created.v1` left compatible.
- Generic connector entity mapping broadened to `order`; provider statuses/remote IDs remain outside Core.
- Contracts, migration, tests and restore/tenancy rehearsal coverage.

## Acceptance

Implementation + tests + updated contracts/docs; run required checks.
