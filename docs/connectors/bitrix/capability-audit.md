# Capability audit

The manifest declares the broader storefront contract for SDK compatibility,
but the production runtime admits only the capabilities demonstrated by the
current host bridge:

| Capability | Runtime status | Evidence / boundary |
|---|---|---|
| `products.read` | admitted | `catalog.product.list`, page cursor and response validation |
| `products.write` | admitted | idempotent `xmlId` lookup, `catalog.product.add/update`, read-after-write verification |
| `inventory.read`, `inventory.write` | not admitted | no worker inventory entity bridge |
| `prices.read`, `prices.write` | not admitted | no worker prices entity bridge |
| `orders.read`, `orders.status.write` | not admitted | no worker order entity bridge |

The REST module and webhook must be enabled on the self-hosted site. A
Bitrix24 cloud portal is not substituted for a 1С-Битрикс site: the existing
`bitrix24` connector remains a separate CRM surface and uses OAuth 2.0.

Product creation intentionally requires `catalog_iblock_id`; the connector
does not guess an information-block, property mapping, tax settings or price
model. Complex offers/variants and custom property synchronization are outside
the generic product contract. No browser automation, scraping, private
endpoints or unverified webhook receipt is used.

