# Megamarket Capability Audit

Official sources audited 2026-08-10:
- https://openapi.megamarket.ru/authorization/description
- https://openapi.megamarket.ru/assortment/description
- https://openapi.megamarket.ru/assortment/openapi
- https://openapi.megamarket.ru/dbs/openapi
- https://openapi.megamarket.ru/fbo/openapi

| Capability | Decision | Baseline |
|---|---|---|
| `products.read` | granted | Assortment `POST /api/merchantIntegration/assortment/v1/card/getAttributes`; bounded `searchAfter` cursor |
| `inventory.read` | granted | Assortment `POST /api/merchantIntegration/assortment/v1/stock/getByOfferId`; warehouses are host-configured per seller topology |
| `orders.read` | granted | common `POST /api/market/v1/orderService/order/search`; scheme is explicit `dbs` or `fbo` |
| `prices.read` | deferred | official read methods exist, but Task 034 acceptance is catalog/order/stock; add only with its own fixtures and qualification |
| `products.write` | denied | no write admission in Task 034 |
| `prices.write` | denied | no write admission in Task 034 |
| `inventory.write` | denied | no write admission in Task 034 |
| `orders.status.write` | denied | requires approval/idempotency/risk review |

## Gaps / boundaries

Megamarket exposes scheme-specific DBS/FBO/C&C/rDBS surfaces. Task 034 does not infer a scheme from response shape. Seller configuration must name `dbs` or `fbo`; unsupported scheme-specific mutations remain denied. The provider does not scrape Partner UI and does not use undocumented endpoints.
