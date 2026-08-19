# Migration 000074 — fulfillment failover execution

Phase: `expand`
Risk: `high`
Backup required: yes

Adds tenant-scoped `fulfillment_allocations` as durable order-item reservation ownership and extends warehouse incident evidence with execution status/counters. Allocation identity, order item, offer, quantity, unit and warehouse are immutable; a failover releases the source allocation and creates a replacement allocation instead of rewriting history.

The table uses FORCE ROW LEVEL SECURITY, composite tenant foreign keys and a partial uniqueness constraint that permits at most one active `reserved` allocation per order item. Insert/update guards verify the allocation exactly matches the immutable order item and forbid new allocations on terminal orders.

The migration does **not** transfer physical `on_hand`. P3 execution only moves tracked `reserved` quantity atomically between existing inventory positions after destination ATP is locked and rechecked.

Mixed-version rollout is safe because old binaries ignore the additive table/columns, while migration 000072 continues to block reservation increases on unavailable/lost warehouses. Rollback is application-first; retain allocation/incident lineage and use a reviewed contract migration only after all P3 writers are stopped.
