# Capability audit

The manifest declares the broader storefront contract for SDK compatibility,
but the production runtime admits only the capabilities demonstrated by the
current host bridge:

| Capability | Runtime status | Evidence / boundary |
|---|---|---|
| `products.read` | admitted | `catalog.product.list`, page cursor and response validation |
| `products.write` | admitted | idempotent `xmlId` lookup, `catalog.product.add/update`, read-after-write verification |
| `inventory.read` | adapter-ready, runtime not admitted | SDK adapter reads active warehouses through `catalog.store.list` and integer balances through `catalog.storeproduct.list`; the generic worker still needs an inventory reconciliation source bridge |
| `inventory.write` | admitted for outbound sync | absolute integer quantities become idempotent `S` (stock receipt) or `D` (write-off) documents with `catalog.document.add`, `catalog.document.element.add`, `catalog.document.conduct` and read-after-write verification |
| `prices.read` | not admitted | generic inbound price reconciliation is not yet wired |
| `prices.write` | admitted | `catalog.price.list` lookup by configured `price_type_id`, then `catalog.price.add/update` with read-after-write verification |
| `orders.read`, `orders.status.write` | admitted | SDK adapter uses `sale.order.list`, `sale.basketitem.list`, `sale.order.get` and `sale.order.update` with read-after-write reconciliation; the runtime requires an explicit canonical-to-Bitrix `order_statuses` map and the worker routes order status events/reconciliation through it |

The REST module and webhook must be enabled on the self-hosted site. A
Bitrix24 cloud portal is not substituted for a 1С-Битрикс site: the existing
`bitrix24` connector remains a separate CRM surface and uses OAuth 2.0.

Product creation intentionally requires `catalog_iblock_id`; the connector
does not guess an information-block, property mapping, tax settings or price
model. Complex offers/variants and custom property synchronization are outside
the generic product contract. No browser automation, scraping, private
endpoints or unverified webhook receipt is used.
