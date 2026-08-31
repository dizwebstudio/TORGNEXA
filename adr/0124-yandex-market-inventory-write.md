# ADR-0124 — Yandex Market inventory write runtime

Status: Accepted

## Context

The Yandex Market adapter already had explicit partner and grouped warehouse
read modes, and its price write had passed the repository qualification gate.
The provider documents two different stock-update request shapes. Advertising
one generic stock writer without preserving that distinction would risk
updating the wrong warehouse or claiming synchronous convergence.

## Decision

Admit `inventory.write` only for Yandex Market after mapping the canonical
`InventoryWriteRequest` at the adapter boundary. Partner mode uses the business
v3 `offers/stocks/update` endpoint with a numeric `partnerWarehouseId`.
Grouped mode uses the campaign v2 `offers/stocks` endpoint and validates the
requested warehouse against the host-owned configuration, while leaving the
provider's grouped request shape unchanged. The generic commerce-sync worker
performs policy, capability, mapping, idempotency, retry/DLQ and receipt
handling; no provider name is added to Core.

The provider's `status=OK` is recorded as `Applied=true, Reconciled=false`.
Later inventory reads and reconciliation determine whether the desired state
converged. Quantities are bounded integer values and remote transport or
malformed responses fail closed.

## Consequences

Yandex Market can receive exact available-stock updates in both supported
warehouse configurations. The integration catalog now reflects an executable
outbound inventory route. Product, order-status and other writes remain
unadmitted until their own provider-neutral contracts and qualification exist.

## Security and privacy impact

API keys remain callback-scoped through the existing SecretAccessor and the
host-mediated transport. Business, campaign, warehouse and SKU identifiers are
validated and tenant-bound by the existing runtime/mapping path. No new PII or
secret is persisted or emitted.

## Compatibility impact

The existing `InventoryWriter` interface and canonical inventory events are
reused unchanged. The manifest/runtime-support and generated catalog change is
additive for Yandex Market; no migration or public API shape change is needed.

## Operational impact

Operators must configure the inventory mode and, for grouped mode, the
allowed warehouse IDs. Updates are eventually consistent and require the
existing inventory reconciliation scan. The outbound policy or account
capability can be disabled to stop writes.

## Alternatives considered

- Keep inventory write SDK-only: rejected because the runtime catalog would
  understate the already-qualified provider path.
- Always use the partner endpoint: rejected because grouped warehouses require
  the campaign request shape and have different warehouse semantics.
- Mark an accepted response as reconciled: rejected because the provider
  applies stock updates asynchronously.
