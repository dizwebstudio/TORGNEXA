# Promotions & Pricing Guards

Task `051` implementation lives in `internal/platform/promotions`.

## Safety invariants

Promotion bulk writes are previewed before execution and fail closed on floor-price or minimum-margin violations; approval is required above policy-defined blast-radius thresholds.

## Persistence

PostgreSQL expand migration: `000029_promotions_pricing_guards.sql`. In-memory implementations in tests are reference semantics, not production durability.
