# WMS Inventory Ledger

Task `054` implementation lives in `internal/platform/wmsledger`.

## Safety invariants

Warehouse stock is represented by an append-only movement ledger with atomic reservations, quarantine, lots, serials and expiry checks; availability is derived, never hand-edited.

## Persistence

PostgreSQL expand migration: `000033_wms_inventory_ledger.sql`. In-memory implementations in tests are reference semantics, not production durability.
