# Task 005: Price + Inventory

## Status
Repository implementation complete. Runtime PostgreSQL/Kafka qualification remains a CI/staging obligation.

## Objective
Implement provider-neutral, money-safe Price and exact InventoryPosition aggregates with optimistic concurrency, tenant isolation, audit records, and Transactional-Outbox event intents.

## Implemented
- [x] Canonical Price mirrors the Task-076 Money representation (`int64` minor units + currency); Task 076b adds parity enforcement and binary float money remains absent.
- [x] Price identity is tenant + Offer + kind + currency, with immutable identity and optimistic version updates.
- [x] Canonical Warehouse and InventoryPosition use provider-neutral IDs; provider warehouse/stock identifiers remain connector projections.
- [x] Inventory quantities mirror canonical Task-076 Decimal/Quantity/UnitCode invariants; Task 076b adds parity enforcement and persistence uses coefficient + scale + unit, never float.
- [x] `reserved <= on_hand`, non-negative quantities and matching units are enforced in Core and PostgreSQL.
- [x] Reserve, release, set-on-hand and consume-reserved mutations are optimistic and fail closed on insufficient stock/reservation.
- [x] Parent Offer/Warehouse lifecycle is checked; disabled/archived parents permit reservation release only, not new stock commitments.
- [x] Every Price/Inventory mutation writes an immutable Task-003 audit row and Task-008 outbox event in the same SQL transaction.
- [x] Versioned events: `commerce.pricing.price_changed.v1`, `commerce.inventory.position_changed.v1`, `commerce.inventory.warehouse_changed.v1`.
- [x] Forced tenant RLS, no application hard-delete/truncate, immutable identities and monotonic versions.
- [x] Draft 2020-12 contracts/fixtures and migration/repository tests added.
- [x] Architecture freeze inventory/review updated without introducing provider-specific Core fields.

## Boundaries
- Promotions/floor-price/margin policy remains Task 051.
- Append-only warehouse movement ledger/WMS workflows remain Task 054.
- Provider price/stock IDs and channel-specific behavior remain connector projections/adapters.
- Orders and reservation orchestration remain Task 006.
- Parent Task 076 is repository-complete after the Task-076b audit of Tasks 004–006.

## Acceptance
Implementation + tests + updated contracts/docs; run required checks.
