# AliExpress RU Connector Spec

## Provider

- ID: `aliexpress-ru`
- Family: `marketplace`
- Version: `1.0.0`
- SDK major: `1`
- Host: `openapi.aliexpress.ru`
- Auth: JWT in `X-Auth-Token`, retrieved only through Task 021 `SecretAccessor`
- Admitted capability: `products.read`

## Product read

Endpoint baseline:

`POST /api/v1/scroll-short-product-by-filter`

Request:
- `filter`: empty bounded filter in the repository baseline;
- `last_product_id`: decoded only from the opaque connector cursor;
- `limit`: decimal string derived from the validated SDK page limit (1..100).

Response projection:
- product `id` -> `RemoteProduct.RemoteID`;
- first SKU `code` -> `SellerSKU`;
- `subject` -> title;
- `ali_updated_at` -> UTC `UpdatedAt`;
- each SKU `sku_id` (fallback internal SKU `id`) -> `RemoteVariant.RemoteID`;
- SKU `code` -> variant alias.

A full page creates a versioned Base64URL cursor bound to the fixed host/path/surface fingerprint. A short page terminates pagination.

## Remote-contract validation

The connector fails closed on:
- missing/duplicate product IDs;
- missing title/update timestamp;
- zero or excessive SKU list;
- missing/duplicate variant IDs;
- missing seller SKU code;
- malformed UTC timestamps;
- response body over 12 MiB;
- non-null/non-empty API error envelope;
- malformed/oversized/tampered cursor.

Unknown additive remote JSON fields are ignored so ordinary API expansion does not break reads, but fields required by the admitted projection remain strict.

## Credential boundary

The secret must be syntactically JWT-shaped (three non-empty Base64URL segments), bounded to 8 KiB, valid UTF-8 at header/payload level, and free of surrounding whitespace. The provider never stores or logs token bytes.

The host-mediated transport receives token bytes only for the request callback and sends them as `X-Auth-Token`. Provider code has no direct socket/DNS/HTTP client authority.

## Error model

- transport failures -> bounded `unavailable/transport_unavailable`;
- 400/405/406/415/422 -> invalid request;
- 401 -> unauthorized;
- 403 -> forbidden;
- 404 -> not found;
- 409 -> conflict;
- 429 -> rate limited with bounded retry metadata;
- 5xx -> unavailable.

Raw remote body and raw transport errors are never copied to SDK errors or health reason codes.

## Explicitly unsupported in v1.0.0

`inventory.read`, `prices.read`, `orders.read` and every write capability are denied by the manifest. The capability audit records what evidence is required to admit them later.
