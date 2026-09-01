# Promotions & Pricing Guards

Task `051` implementation lives in `internal/platform/promotions`.

## Safety invariants

Promotion bulk writes are previewed before execution and fail closed on floor-price or minimum-margin violations; approval is required above policy-defined blast-radius thresholds.

Task `221` adds the operator-facing `/pricing` dry-run. It accepts up to 1,000
normalized candidate rows and returns a stable digest, exact minor-unit delta,
floor-price/max-step decision and a Russian explanation per row. The API never
calls a marketplace and does not change the canonical price table. Existing
catalog price edits remain the only internal price mutation surface.

## Persistence

PostgreSQL expand migration: `000029_promotions_pricing_guards.sql`. In-memory implementations in tests are reference semantics, not production durability.

Marketplace apply, Buy Box/competitor observations, promotions and advertising
management remain `qualification_required` until an official connector route,
approval evidence, idempotency receipt and read-after-write check are present.
