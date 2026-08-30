# WMS Execution

Task `055` reference semantics live in `internal/platform/wmsexecution`.
Production operator execution lives in
`internal/platform/postgres/inventoryrepo/wms.go` and is exposed under
`/api/v1/warehouse-tasks`. The current Task 170 slice also exposes local
`pack_handoff` batches under `/api/v1/warehouse-task-batches`.

## Safety invariants

Warehouse execution is an idempotent task/event state machine driven by scanner-friendly commands. Repeated scan idempotency keys do not duplicate physical work.

## Persistence

PostgreSQL expand migrations `000030_wms_operator_tasks.sql` and
`000031_wms_task_batches.sql` provide durable task/batch state. In-memory
implementations in tests are reference semantics, not production durability.

The UI never persists barcode, access token or tenant data in browser storage.
Production qualification still requires the real Compose/VPS topology,
backup/restore checkpoint and authenticated operator evidence.
