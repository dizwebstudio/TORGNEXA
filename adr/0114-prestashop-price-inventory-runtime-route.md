# ADR-0114 — PrestaShop price and inventory outbound runtime route

Status: Accepted

## Context

The PrestaShop connector already implements safe Webservice XML writes for
prices and `stock_available` quantities, but the worker previously accepted
only the generic `products` reconciliation entity. Advertising those writes in
the SDK without a worker route made the integration matrix misleading.

## Decision

Add a dedicated `torgnexa.commerce-sync.v1` Kafka consumer group. It consumes
the canonical `commerce.pricing.price_changed.v1` and
`commerce.inventory.position_changed.v1` events, selects enabled outbound
policies, resolves the tenant `offer` mapping, and calls the built-in
PriceWriter or InventoryWriter. The route is admitted only for PrestaShop in
the generated runtime-support contract. It derives deterministic idempotency
keys from policy and event identity and records applied/duplicate outcomes in
`sync_local_receipts` after the remote receipt is validated.

Malformed events, unsupported units, missing mappings and non-retryable remote
responses are dead-lettered. Rate limits, timeouts and transient connector
errors use the existing Kafka retry path. No provider IDs or credentials enter
canonical events.

## Consequences

PrestaShop can now synchronize regular prices and discrete available stock from
TORGNEXA through the production worker. The route intentionally does not claim
price reads, compare-at/cost prices, fractional stock units, order status or
multi-warehouse aggregation; those require separate provider-neutral contracts.

The support contract and generated frontend catalog expose `prices` and
`inventory` as outbound sync entities, while other connectors remain unchanged.

## Security and privacy impact

The existing callback-scoped SecretProvider and host-mediated PrestaShop
transport remain the only credential and network boundaries. Account
capability snapshots are rechecked before every event, and mappings are
tenant-scoped. Event payloads contain only bounded IDs, exact money/quantity
values and versions.

## Compatibility impact

The SDK interfaces and event envelope are unchanged. The sync capability map is
extended additively for the existing `prices` entity. Existing policies and
accounts remain valid; no migration is required.

## Operational impact

Workers with reconciliation enabled start one additional consumer group. The
group uses the normal retry and DLQ topics, so remote outages do not lose local
events. Operators must enable `prices.write`/`inventory.write`, create outbound
policies and maintain `offer` mappings before writes can occur.

## Migration and data impact

No migration is required. The route reuses existing sync policy, connector
mapping and local receipt tables. Generated support projections are rebuilt from
the versioned runtime-support contract during the normal release build.

## Alternatives considered

- Leave the capabilities SDK-only: rejected because the integration catalog
  would continue to promise writes that the production worker cannot execute.
- Broaden the existing product reconciliation source to prices and inventory:
  rejected because those domains have different payloads, mapping semantics and
  write acknowledgements than product snapshots.
- Add provider-specific branches to the generic sync engine: rejected by the
  connector boundary; provider construction remains confined to
  `internal/platform/builtinruntime` and the route uses provider-neutral SDK
  interfaces.
