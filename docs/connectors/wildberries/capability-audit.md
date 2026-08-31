# Wildberries capability audit — 2026-08-31

## Decision

Admit the read baseline and the bounded marketplace publication slice needed to prove the marketplace SDK against the official API:

| SDK capability | WB surface | Task 011 |
|---|---|---|
| `products.read` | Content `POST /content/v2/get/cards/list` | enabled |
| `inventory.read` | Marketplace `GET /api/v3/warehouses`; `POST /api/v3/stocks/{warehouseId}` | enabled |
| `products.write` | Content `/content/v2/cards/upload` and `/content/v2/cards/update` | enabled for bounded snapshot card create/update |
| `prices.read/write` | Prices/discounts APIs | deferred |
| `inventory.write` | `PUT/DELETE /api/v3/stocks/{warehouseId}` | denied |
| `orders.read` | FBS/DBS/DBW order APIs | deferred |
| `orders.status.write` | marketplace order mutation APIs | denied |
| returns/reviews/messages/ads/promotions/finance | dedicated WB categories | deferred |

## Current compatibility notes

- `cards/list` uses cursor pagination based on `updatedAt` + `nmID`; the cursor is connector-owned and opaque to the host.
- Seller warehouse stock methods use `chrtId` (size ID) as the current identity. Task 011 does not send the retired `sku` request parameter.
- WB API quotas are token-bucket/method/token-type dependent. The connector manifest is deliberately conservative and honors normalized retry metadata instead of hard-coding a global WB quota.
- WB token categories changed in 2026. The product-card method requires the current Content authorization; older assumptions that unrelated categories grant card access are not supported.
- WB may add response fields. Decoder projections intentionally ignore unknown remote fields while rejecting malformed types and unsafe/boundedness violations.

## Reconciliation mapping

`nmID`, `chrtID`, warehouse ID and seller SKU remain remote observations. Persistent local/remote identity is represented only by Task-010 `EntityMapping`; Task-013 Sync Engine owns propagation state/receipts and Task-014 owns drift/remediation evidence.

## Marketplace publication slice

Task 217 adds a separate `ProductPublicationWriter`. It sends only a validated
provider-neutral snapshot, forwards the host idempotency key, and returns an
accepted receipt without treating HTTP 200 as final publication. The bounded
status reader uses the existing cards projection. Released media and
provider-specific characteristic/category bridges are rejected explicitly until
their official fixtures and upload pipeline are qualified.

## Release rule

Before publishing a new Wildberries connector version, compare these methods, auth categories, schemas and method-specific limits against current official WB documentation/release notes, refresh fixtures when semantics changed, and rerun Task-064 conformance. A changed remote contract is not silently absorbed by Core.
