# Wildberries Connector Spec v1

Status: repository-qualified read and bounded product-publication connector
Snapshot date: 2026-08-31
Connector ID: `wildberries`  
Connector SDK: v1

## Official API baseline

This connector is pinned to the official WB API documentation and release notes current on 2026-08-10:

- API overview / connection check: `https://dev.wildberries.ru/en/docs/openapi/api-information`
- Product cards and seller warehouse inventory: `https://dev.wildberries.ru/en/docs/openapi/work-with-products`
- Authorization system: `https://dev.wildberries.ru/en/knowledge-base/articles/019d49a1-0d73-71e9-be3e-b2c44567470c/wb-api-authorization-system`
- Rate limits: `https://dev.wildberries.ru/knowledge-base/articles/019d49a1-28ca-7735-bf2f-98210695abc7/limity-zaprosov-wb-api`
- May 2026 stock-ID migration: `https://dev.wildberries.ru/en/news/317/wb-api-digest-may-2026`
- June/July 2026 card-token category changes: `https://dev.wildberries.ru/en/news/324/wb-api-digest-june-2026` and `https://dev.wildberries.ru/en/news/333/daidzhest-wb-api-iiul-2026`

WB publishes the documentation in OpenAPI/Swagger form and may change limits or fields. Before each connector release, rerun the capability audit against the official portal and release notes.

## Authentication

The connector declares one required bearer credential class: `marketplace.token`. Plaintext exists only inside `Runtime.Secrets().UseSecret` and is passed synchronously to the host-injected transport. The connector never receives a secret repository, persists a token, logs it, or includes it in normalized errors/evidence.

The seller token must have the official WB categories required by the enabled read methods. In particular, product card reads require the Content category; seller warehouse inventory requires Marketplace access. The August 3, 2026 token-category change for `POST /content/v2/get/cards/list` is treated as part of the current compatibility baseline.

## Network allowlist

The read-only implementation needs TLS egress only to:

- `content-api.wildberries.ru:443`
- `marketplace-api.wildberries.ru:443`

Provider code does not import `net/http`, DNS, socket, filesystem, process, database, Core, or App packages. HTTP is represented by a bounded host-injected `Transport`; the Task-025/029 host boundary remains responsible for DNS/rebinding checks, TLS, egress grants and resource isolation.

## Health

Health checks `GET /ping` on both Content and Marketplace API domains using the configured scoped token. A successful check returns only SDK `healthy`. Authentication/authorization and rate-limit failures become bounded SDK health states/reason codes; raw WB response bodies never enter health evidence.

## Read capabilities

### `products.read`

Remote method:

`POST https://content-api.wildberries.ru/content/v2/get/cards/list`

The connector sends the official cursor request with a maximum page size of 100 and `withPhoto=-1`. The WB cursor (`updatedAt`, `nmID`) is encoded as an opaque connector cursor; the host never interprets it.

Projection:

- WB `nmID` -> `RemoteProduct.remote_id`
- `vendorCode` -> `seller_sku`
- `title` -> `title`
- `brand` -> `brand`
- `updatedAt` -> UTC `updated_at`
- size `chrtID` -> `RemoteVariant.remote_id`
- size `skus[]` -> variant SKU/barcode list

WB identities are never added to Core Product/Offer structs. Task-010 EntityMapping is the only local/remote identity bridge used by Tasks 013/014.

### `inventory.read`

Warehouse discovery:

`GET https://marketplace-api.wildberries.ru/api/v3/warehouses`

Stock read:

`POST https://marketplace-api.wildberries.ru/api/v3/stocks/{warehouseId}`

Task 011 uses the current `chrtId` size identity, not the retired SKU request parameter. Reads are bounded to 1,000 variant IDs per call. Responses are rejected if they contain negative quantities, duplicate/unrequested `chrtId` values, invalid warehouse IDs, oversized collections, trailing/malformed JSON, or an oversized body.

## Pagination, quotas and retries

The manifest uses a conservative `200 ms` minimum interval, concurrency `2`, 15-second request timeout and bounded five-attempt retry policy. This is host scheduling guidance, not a claim that every WB endpoint has the same quota. WB documents token-bucket limits and token-type-specific quotas; method-specific limits and `Retry-After` take precedence. HTTP 429 is normalized as `rate_limited` with bounded retry metadata.

## Error normalization

The connector maps status classes into SDK `RemoteError` categories only. Response bodies, URLs, headers other than pre-normalized request/retry metadata, tokens and remote text are discarded. Transport failures become `unavailable/transport_unavailable` without propagating raw transport error strings.

## Writes

Task 217 admits card create and update through the official Content API
`POST /content/v2/cards/upload` and `POST /content/v2/cards/update`. The
adapter maps title, description, brand, seller SKU, numeric subject/category,
variants and barcodes from a validated snapshot. The host-supplied
`Idempotency-Key` is forwarded as retry metadata.

WB card writes are asynchronous from the operator's point of view. The receipt
is `accepted`; a later bounded cards read is required before local state becomes
`published`. The adapter deliberately rejects snapshot media and
provider-specific characteristic/category bridges until a released-upload
bridge and current category fixtures are qualified. Inventory, order-status
and other mutation capabilities remain unchanged.
