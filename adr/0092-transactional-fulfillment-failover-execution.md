# ADR-0092: Transactional fulfillment failover execution

Status: Accepted

## Context

Task 116 made warehouse incidents durable and restart-safe, but its route decision was intentionally evidence-only. A `routed` decision did not prove that any existing order reservation or fulfillment ownership had moved to the destination warehouse. That gap made it unsafe to claim automatic fulfillment failover.

Inventory positions already distinguish physical on-hand from reserved quantity. Order items are immutable commerce facts. P3 needs a durable binding between those facts without inventing stock transfers or rewriting order history.

## Decision

Introduce `fulfillment_allocations` as the authoritative ownership record for an order-item reservation. An allocation exactly mirrors immutable order-item offer, quantity and unit, starts in `reserved`, and identifies one warehouse. Its warehouse identity is immutable. Normal allocation increments the matching inventory position's reserved quantity only after locking and checking warehouse state and ATP.

When a warehouse becomes `UNAVAILABLE` or `LOST`, the incident worker processes tracked allocations for each affected offer in one tenant-scoped PostgreSQL transaction. It locks source allocations and both inventory positions, selects only an explicitly configured active/degraded destination with sufficient ATP for the complete tracked quantity, releases source reservation quantity, reserves the destination quantity, marks each source allocation `released`, and creates one replacement allocation per order item with immutable `replaces_allocation_id` and `incident_id` lineage.

The transaction emits normal inventory-position events plus `commerce.fulfillment.allocation_changed.v1`. If capacity changes, an allocation is inconsistent, an order is terminal, or legacy reserved quantity cannot be mapped to a tracked order item, the system fails closed and records execution attention rather than guessing.

Physical `on_hand` is never changed by this workflow. Moving a reservation is not represented as moving goods.

P3 also makes the deployed-image runtime qualifier mandatory in the release workflow. The qualifier creates a real tracked reservation, transitions the source warehouse to `LOST`, requires a destination replacement allocation, proves source reservation release and destination reservation increase, and observes fulfillment outbox evidence after worker/Kafka/PostgreSQL recovery drills.

## Migration and data impact

Migration 000074 is expand-only. It adds durable allocation ownership and execution evidence without rewriting existing orders or inventory rows. Legacy reservations remain valid but are not auto-rerouted unless they are tracked by allocations.

## Compatibility impact

Migration 000074 is expand-only. Existing inventory/order rows remain valid. Legacy reserved quantities without fulfillment allocations remain visible but are not guessed; they produce `untracked_reservation` attention during failover.

The OpenAPI addition is additive. Existing event families remain unchanged; the new fulfillment-allocation event begins at v1 and has a dedicated `commerce.fulfillment.events.v1` transport topic.

## Security and privacy impact

`fulfillment_allocations` uses composite tenant keys, FORCE ROW LEVEL SECURITY, immutable identity guards, and restricted destructive operations. Worker job claiming remains the narrow SECURITY DEFINER boundary; execution returns immediately to tenant scope.

## Operational impact

Operators can now distinguish `routed` intent from `rerouted` execution. Restart cannot duplicate an active allocation for the same order item because the active-row uniqueness constraint and deterministic failover idempotency key reject duplicates.

A warehouse incident may still end in `needs_attention`. That is a safety outcome, not a partial success: untracked reservations, terminal orders, inconsistent accounting and insufficient fallback ATP must be resolved explicitly.

## Consequences

P3 can now claim automatic failover only for reservations whose order-item ownership is explicit and auditable. Incidents that cannot be executed safely remain visible as `needs_attention`; this deliberately favors correctness over apparent availability.

## Alternatives considered

Rewriting the warehouse on the original allocation was rejected because it destroys lineage. Copying on-hand stock to a backup was rejected because physical inventory has not moved. Rerouting only in memory was rejected because worker restart would lose fulfillment ownership. Partial quantity fallback across several warehouses was deferred because the current order-item allocation contract intentionally preserves one active allocation per immutable order item.
