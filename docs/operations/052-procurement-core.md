# Procurement Core

Task `052` implementation lives in `internal/platform/procurement`.

## Safety invariants

Suppliers reference canonical legal parties; supplier offers and purchase orders use exact Money/Quantity primitives and an explicit purchase-order lifecycle with auditable transitions.

## Persistence

PostgreSQL expand migration: `000031_procurement_core.sql`. In-memory implementations in tests are reference semantics, not production durability.
