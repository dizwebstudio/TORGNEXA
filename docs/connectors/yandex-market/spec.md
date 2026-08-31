# Yandex Market Connector Spec

## Identity

- id: `yandex-market`
- family: `marketplace`
- version: `1.0.0`
- SDK: v1
- API host: `api.partner.market.yandex.ru`

## Capabilities

Read/receive: `products.read`, `prices.read`, `inventory.read`, `orders.read`, `notifications.receive`.

Write: `prices.write` for an exact desired price and `inventory.write` for one
exact non-negative available quantity, admitted by Tasks 116 and 172.

No product, order-status, campaign or notification-setting write capability is
granted.

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

## Exact price writes

Task 116 maps the provider-neutral `PriceWriteRequest` to the business-wide or
campaign-specific exact-price surface selected by account configuration. RUB is
translated to the provider's RUR currency code, and a crossed-out price is
accepted only when it satisfies the provider's integer contract.

The request expresses desired state and is safe to retry after transport
ambiguity. A successful remote acceptance returns `Applied=true` and
`Reconciled=false`: catalogue propagation is eventual, so a later price read and
Task-014 reconciliation must confirm the observed remote value. The host still
owns authorization, policy/risk checks, audit and any required approval before
dispatch.

## Exact inventory writes

For `partner_warehouses`, the adapter sends one SKU item to the business
`POST /v3/businesses/{businessId}/offers/stocks/update` endpoint and includes
the configured numeric `partnerWarehouseId`. For `campaign_warehouses`, it
sends one SKU item to `PUT /v2/campaigns/{campaignId}/offers/stocks`; the host
must first validate the requested warehouse against its configured allowlist,
while the provider's grouped endpoint carries no warehouse field.

Stock quantity is an integer from zero through the provider's documented
maximum of 2,000,000,000. The provider acknowledges the request
asynchronously, so the receipt is `Applied=true`, `Reconciled=false`; the
normal inventory read/reconciliation path confirms eventual convergence.

## Limits

Response bodies are capped at 12 MiB. Manifest scheduling is conservative for general Partner API calls (concurrency 2, 250 ms minimum interval, 15 s timeout, bounded retry). Endpoint-specific lower ceilings, especially warehouse discovery, must additionally be enforced by the host scheduler/cache from the current remote limit metadata; rate-limit responses (including HTTP 420/429) normalize to the SDK rate-limit category.
