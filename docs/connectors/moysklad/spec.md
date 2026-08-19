# MoySklad Connector Spec

## Identity

- id: `moysklad`
- family: `erp`
- version: `1.0.0`
- SDK: v1
- host: `api.moysklad.ru`
- API base: `/api/remap/1.2`

## Capabilities

Read only: `erp.catalog.read`, `erp.inventory.read`, `erp.orders.read`.

## Product mapping

Assortment product `id` remains a remote identity and is joined to canonical entities through Task-010 EntityMapping. `article` maps to the optional ERP SKU, `name` to title, `updated` to opaque remote revision, and `archived` to archive state. Missing MoySklad `code` is legal and remains empty; the SDK does not invent an identifier.

## Inventory mapping

`/report/stock/bystore` is paginated by remote product rows. The connector flattens each `stockByStore` row to `(store remote id, product remote id, exact stock decimal)`. If a host page limit is reached in the middle of one remote product row, the opaque cursor stores only bounded offset/row/inner indexes and re-fetches the same remote page on resume. Signed stock balances are preserved exactly because the official stock report can legitimately return negative values; exponent/malformed values fail closed rather than being rounded or clamped.

## Order mapping

Customer-order `id` is the remote order identity, `name` is the remote order number, `updated` is revision evidence, and optional state/store links become remote mapping keys. A present `deleted` timestamp maps to `Deleted=true`. Money and line-item semantics are intentionally not copied into this minimal read baseline.

## Pagination and limits

All list cursors are Base64URL opaque values. Product/order cursors carry a bounded offset; inventory cursors additionally carry bounded row/inner positions. Cursors are fingerprinted by connector/surface/version and are rejected across surfaces or malformed input.

Manifest throttling is deliberately stricter than the documented service ceiling: concurrency 4 and a 250 ms minimum interval, with bounded retry/backoff. Response bodies are capped at 20 MiB and page limit at 1000.
