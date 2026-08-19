# WMS Execution

Task `055` implementation lives in `internal/platform/wmsexecution`.

## Safety invariants

Warehouse execution is an idempotent task/event state machine driven by scanner-friendly commands. Repeated scan idempotency keys do not duplicate physical work.

## Persistence

PostgreSQL expand migration: `000034_wms_execution.sql`. In-memory implementations in tests are reference semantics, not production durability.
