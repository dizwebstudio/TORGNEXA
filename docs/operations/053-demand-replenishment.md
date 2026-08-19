# Demand & Replenishment Planning

Task `053` implementation lives in `internal/platform/replenishment`.

## Safety invariants

Replenishment is advisory by default. Every recommendation pins an immutable input snapshot digest and algorithm version, and no recommendation can auto-send a purchase order.

## Persistence

PostgreSQL expand migration: `000032_replenishment_planning.sql`. In-memory implementations in tests are reference semantics, not production durability.
