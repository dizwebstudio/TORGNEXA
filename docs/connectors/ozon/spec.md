# Ozon Connector Spec v1

Status: repository-qualified read and bounded product-publication connector
Snapshot date: 2026-08-31
Connector ID: `ozon`  
Connector SDK: v1

## Official API baseline

The connector is pinned to the official Ozon Seller API documentation/release stream current on 2026-08-10:

- Seller API overview: `https://docs.ozon.ru/global/en/api/intro/`
- Seller API reference: `https://docs.ozon.ru/api/seller/`
- Ozon developer news: `https://dev.ozon.ru/news/`
- Official Seller API change notifications: `https://t.me/s/OzonSellerAPI`

Current read baseline used by Task 012:

- products: `POST /v3/product/list` and `POST /v3/product/info/list`;
- FBS warehouses: `POST /v2/warehouse/list`;
- FBS stock by warehouse: `POST /v2/product/info/stocks-by-warehouse/fbs`.

Ozon announced the `/v1/warehouse/list` retirement in February 2026 and directed sellers to `/v2/warehouse/list`. It also made `limit` mandatory for `/v2/product/info/stocks-by-warehouse/fbs`; the v2 stock method accepts seller `offer_id` or SKU selection. `/v3/product/info/list` remains active in the July 2026 official change feed.

Before every connector release, rerun the capability audit against the live official Seller API docs/change feed.

## Authentication and secret boundary

Seller API requests use `Client-Id` and `Api-Key`. TORGNEXA stores them as one opaque secret reference with a two-line secret payload (`Client-Id`, then `Api-Key`). Plaintext exists only inside `Runtime.Secrets().UseSecret`; provider code never receives the secret repository and never persists/logs credentials. The slices passed to the host transport are valid only during that callback.

## Network boundary

Task 012 requires TLS egress only to `api-seller.ozon.ru:443`. Provider code does not import `net/http`, DNS/socket APIs, SQL/database, filesystem/process, Core or App packages. A bounded host-injected transport owns actual HTTP/TLS/egress enforcement.

## Health

Health performs a bounded `POST /v3/product/list` with `visibility=ALL` and `limit=1`. Authentication/rate-limit/service failures become SDK machine states. Remote response bodies and raw transport errors are never propagated.

## `products.read`

The connector obtains the page identity set from `/v3/product/list`, retaining Ozon `last_id` only inside an opaque connector cursor, then fetches bounded product details from `/v3/product/info/list`.

Projection:

- Ozon product ID -> `RemoteProduct.remote_id`;
- seller `offer_id` -> `seller_sku` and `RemoteVariant.remote_id`;
- name -> title;
- barcodes -> variant SKU/barcode aliases;
- `updated_at` -> UTC update time.

Product/offer IDs remain remote identities and are joined to TORGNEXA entities only by Task-010 mappings.

## `inventory.read`

Warehouse discovery uses `/v2/warehouse/list`. Because the current Connector SDK location method is deliberately non-paginated, a non-empty Ozon warehouse cursor fails closed rather than returning a partial warehouse universe.

Stock uses `/v2/product/info/stocks-by-warehouse/fbs`, selecting by seller `offer_id`. For a requested warehouse, canonical available quantity is `present - reserved`; negative values, `reserved > present`, duplicate/unrequested offers or duplicate warehouse rows fail closed. A requested offer with no row for the selected warehouse is explicitly returned as zero.

## Pagination, retries and API drift

Product `last_id` is opaque and bounded. Reusing the same continuation value for a non-empty page is rejected. Collection sizes and bodies are bounded. The manifest provides conservative host scheduling guidance (concurrency 2, 250 ms minimum interval, 15 s timeout, five bounded attempts); endpoint-specific Ozon limits and normalized retry metadata take precedence.

Unknown additive response fields are ignored, while identity/type/boundedness mismatches are treated as remote-contract failure.

## `products.write`

Task 217 admits a bounded import slice through `POST /v2/product/import` and
status lookup through `POST /v1/product/import/info`. The request contains
validated offer/SKU, name, description, numeric category, barcode, exact minor
unit price, VAT and integer dimensions. Ozon's task ID is returned as
`remote_operation_id`; only a later status response can produce `published`.

The adapter rejects media and non-empty canonical attributes until their
provider-specific mapping and released-upload bridge pass qualification. Raw
Ozon responses, credentials and arbitrary URLs never enter the host receipt.

The bounded price writer uses `POST /v1/product/import/prices` for one offer at
a time, validates the per-offer `updated` result and keeps the receipt
unreconciled until a subsequent price read confirms the remote value. Ozon
inventory writes remain deferred because the current host contract does not yet
carry the product identity required by the verified stock mutation API.

## `orders.read`

The bounded FBS order projection uses `POST /v3/posting/fbs/list` with an
explicit UTC time window and an opaque offset cursor. The adapter validates
posting identity, dates, items and quantities, rejects duplicate rows and
never returns the raw posting payload. Provider status is retained as
`status_remote_id`; canonical status mapping is performed by the runtime
composition layer. Outbound order mutation, returns and settlement remain
separate capabilities.
