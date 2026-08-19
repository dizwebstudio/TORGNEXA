# Migration 000010 — Price + Inventory

Task 005 is an additive high-risk `expand` migration. It creates canonical `prices`, `warehouses`, and `inventory_positions` after Task 004 introduced Product/Offer.

## Exact values

Prices use `minor_units bigint` plus an uppercase three-letter currency. Inventory does not use `float`, `real`, or `double precision`: each quantity stores an `int64` coefficient, scale `0..9`, and a provider-neutral unit. Database checks enforce canonical normalized decimals and compare reserved/on-hand exactly by cross-scaling with PostgreSQL numeric arithmetic.

## Integrity

- Price identity `(tenant, offer, kind, currency)` is immutable and versioned.
- Warehouses start `active` at version 1; Task 005 permits only status changes after creation.
- InventoryPosition identity `(tenant, offer, warehouse, unit)` is immutable and begins at zero/version 1.
- `reserved <= on_hand` and both values are non-negative.
- New price/position state cannot be created against archived Offers; new positions require active Warehouses.
- An inactive parent permits reservation release only, preventing new commitments while allowing outstanding reservations to unwind.
- All three tables use forced tenant RLS and reject application hard delete/truncate.

## Rollback / upgrade

The migration is expand-only and does not rewrite Task-004 rows. Rollback means disabling the Task-005 binary paths while retaining canonical price/inventory state and audit/outbox history. Production rollout must satisfy the Task-067 backup/rehearsal gate before applying this high-risk migration.
