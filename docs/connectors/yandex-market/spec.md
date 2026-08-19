# Yandex Market Connector Spec

## Identity

- id: `yandex-market`
- family: `marketplace`
- version: `1.0.0`
- SDK: v1
- API host: `api.partner.market.yandex.ru`

## Capabilities

Read/receive only: `products.read`, `prices.read`, `inventory.read`, `orders.read`, `notifications.receive`.

No product, price, stock, order-status, campaign or notification-setting write capability is granted by Task 033.

## Configuration and remote identities

`businessId` and `campaignId` are host-owned non-secret account configuration. `offerId`, `orderId`, `campaignId` and warehouse IDs remain remote identifiers and are joined to canonical entities through Task-010 mappings; no Yandex-specific identifiers enter Core.

Inventory mode is explicit. `partner_warehouses` uses the v3 business warehouse/stock surfaces for FBS/Express/DBS accounts without warehouse groups. `campaign_warehouses` uses the v2 campaign stock surface for grouped/FBY/LaaS cases and requires the host to resolve the bounded warehouse ID allowlist. The connector never guesses the mode.

Price mode is explicit. `campaign_unique` reads campaign prices; `business_wide` reads the basic catalog price from offer mappings.

## Pagination and exact values

Remote `pageToken` values are treated as opaque and wrapped in a bounded Base64URL cursor fingerprinted by connector configuration and surface. Cross-surface/configuration cursor reuse fails closed.

Price values are decoded from JSON number lexemes into exact decimal strings; no `float64` conversion is used. Inventory quantities are integer AVAILABLE counts and negative/duplicate/unrequested rows fail closed.

## Orders and notifications

The order projection intentionally excludes buyer contact/address data. It preserves only bounded remote identity, campaign/program/status/substatus, timestamps and line identity/count required by sync/reconciliation.

The notification decoder accepts the documented notification family, validates configured business/campaign scope, derives a deterministic dedupe key, and returns a minimal event projection. Duplicate delivery is expected and is handed to Task-009 Inbox using that key. Network-source authentication/allowlisting remains a host edge responsibility, not parser authority.

## Limits

Response bodies are capped at 12 MiB. Manifest scheduling is conservative for general Partner API calls (concurrency 2, 250 ms minimum interval, 15 s timeout, bounded retry). Endpoint-specific lower ceilings, especially warehouse discovery, must additionally be enforced by the host scheduler/cache from the current remote limit metadata; rate-limit responses (including HTTP 420/429) normalize to the SDK rate-limit category.
