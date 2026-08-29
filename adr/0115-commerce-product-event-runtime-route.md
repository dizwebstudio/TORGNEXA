# ADR-0115 — Canonical product event outbound runtime route

Status: Accepted

## Context

The catalog repository already publishes
`commerce.catalog.product_changed.v1` through the transactional outbox. The
`commerce-sync` worker consumed only price and inventory events, while the
runtime-support contract and storefront cards already admitted outbound
`products` synchronization for qualified connectors. As a result, product
changes were silently ignored even though product writers and policies
existed.

## Decision

The `torgnexa.commerce-sync.v1` consumer also handles
`commerce.catalog.product_changed.v1` for enabled outbound `products`
policies. It loads the current tenant-scoped canonical product, resolves a
tenant-scoped `product` mapping, and calls the provider-neutral `ProductWriter`
with a deterministic policy/event idempotency key. If no mapping exists, the
writer receives an empty remote ID for a find-or-create operation; the mapping
is persisted only after the connector returns a validated applied or
duplicate receipt.

Canonical catalog lifecycle values are translated to provider-native status
values inside the reviewed built-in runtime composition boundary. The worker
does not branch on provider names or place provider identifiers in catalog
events. The existing policy, account capability, manifest, runtime admission,
retry/DLQ and local receipt checks remain mandatory.

## Consequences

Qualified storefronts can receive product creates and updates from canonical
catalog events. A crash after a remote create and before mapping/receipt
commit safely retries through the connector's SKU/idempotency reconciliation.
Connectors without an admitted `products.write` route, a valid status mapping,
or the required account capability continue to fail closed and do not receive
the event.

## Compatibility impact

The event envelope and `product_changed.v1` payload are unchanged. The route
is additive for already-admitted outbound `products` policies; existing price
and inventory policies retain their mandatory `offer` mapping behavior.

## Migration and data impact

No migration is required; existing `connector_entity_mappings` and
`sync_local_receipts` tables are reused. A new product mapping is written only
after a validated remote receipt and is safe to replay.

## Security and privacy impact

Product reads and mapping writes are tenant scoped. Secrets remain callback
scoped to the connector runtime. No new personal data enters events or
persistence.

## Operational impact

Operators can stop propagation by disabling the outbound policy or account
capability. Retryable provider failures use the existing Kafka retry topics;
malformed payloads and permanent connector failures use the DLQ. Product
creates require a connector with a safe SKU/idempotency reconciliation path.

## Alternatives considered

- Keep ignoring product events: rejected because the runtime-support contract
  already admits outbound product synchronization and the catalog would remain
  misleading.
- Put provider status branches in the worker: rejected because provider
  composition belongs in the reviewed built-in registry.
- Add product fields to the event payload: rejected because the canonical event
  contract intentionally carries identity/version/status and the worker can
  read the authoritative tenant-scoped snapshot.
