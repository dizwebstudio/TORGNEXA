# Advertising Engine

Task `050` implementation lives in `internal/platform/advertising`.

## Safety invariants

Advertising mutations are provider-neutral commands with hard action/budget ceilings, explicit attribution metadata, dry-run preview and approval references; connector transports cannot bypass host guards.

## Persistence

PostgreSQL expand migration: `000030_advertising_engine.sql`. In-memory implementations in tests are reference semantics, not production durability.
