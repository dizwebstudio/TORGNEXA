# Ozon capability audit — 2026-08-31

## Decision

Admit Ozon as the second marketplace reference provider and reuse the same additive Connector SDK v1 interfaces already proven by Wildberries.

| SDK capability | Ozon Seller API | Task 012 |
|---|---|---|
| `products.read` | `/v3/product/list`; `/v3/product/info/list` | enabled |
| `inventory.read` | `/v2/warehouse/list`; `/v2/product/info/stocks-by-warehouse/fbs` | enabled |
| `products.write` | `/v2/product/import`; `/v1/product/import/info` | enabled for bounded import/status slice; media and field-level attributes deferred |
| orders/returns/finance/ads/chats | additional Seller API surfaces | deferred |

## Provider-neutral proof

Task 012 adds no new Connector SDK read interface and no Ozon branch to Core, Sync or Reconciliation. Ozon product ID, `offer_id`, warehouse ID and SKU remain remote observations. Task 010 owns mapping, Task 013 owns propagation receipts/checkpoints and Task 014 owns drift/remediation.

The different identity/pagination model versus Wildberries is intentional proof: WB uses `nmID/chrtID` and an `updatedAt+nmID` cursor, while Ozon uses product ID/`offer_id` and `last_id`; both fit the same `ProductReader`/`InventoryReader` host contracts.

Task 217 adds a separate `ProductPublicationWriter`. It returns the Ozon import
task ID as an asynchronous receipt and never interprets a partial import as a
published product. Media upload and category-specific attribute mapping remain
explicitly unsupported until the current provider contracts are qualified.

## Current compatibility notes

- use v2 warehouse/stock endpoints; the former v1 warehouse method was retired in 2026;
- stock v2 requires a bounded `limit` and accepts seller offer IDs or SKUs;
- product detail remains `/v3/product/info/list` in the July 2026 official change stream;
- Ozon API keys have lifecycle/role policy outside this connector; credentials remain host-secret material and are never durable provider evidence.

## Release rule

Before publishing a new Ozon connector version, verify endpoint versions, required fields, auth rules and quotas against current official Ozon documentation, refresh deterministic fixtures for semantic changes, and rerun Task-064 conformance. Remote schema changes must not be patched into Core.
