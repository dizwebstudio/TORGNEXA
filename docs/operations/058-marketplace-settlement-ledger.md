# Marketplace Settlement Ledger

Task `058` implementation lives in `internal/platform/settlements`.

## Safety invariants

Marketplace settlements are immutable append-oriented facts keyed by provider references. Corrections are new adjustment entries and original currency is preserved until sourced FX exists.

## Persistence

PostgreSQL expand migration: `000037_marketplace_settlement_ledger.sql`. In-memory implementations in tests are reference semantics, not production durability.
