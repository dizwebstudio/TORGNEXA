# Megamarket Connector Spec v1

## Identity and authority

Provider ID: `megamarket`; family: `marketplace`; SDK major: 1. Credentials are resolved only through Task 021 as a `marketplace.api-key` and sent as `X-Merchant-Token` by the host-owned transport to fixed host `api.megamarket.tech`.

## Configuration

Host-owned non-secret configuration contains:
- positive `merchant_id`;
- explicit fulfillment scheme: `dbs` or `fbo`;
- one or more `(warehouse_id, warehouse_name)` pairs known from the seller account.

Provider code has no SQL/filesystem/process/Core/App/network authority.

## Products

`POST /api/merchantIntegration/assortment/v1/card/getAttributes` with bounded `limit`, deterministic `offerId` sort and opaque `searchAfter`. `goodsId` is the remote product ID; `offerId` is seller SKU and remote variant ID. The cursor is SHA-256 bound to configuration + surface.

## Inventory

For each requested seller offer, call `POST /api/merchantIntegration/assortment/v1/stock/getByOfferId`. Only host-configured warehouse IDs are addressable. Missing warehouse rows normalize to zero; duplicates, negative quantities, unexpected offer IDs and malformed responses fail closed.

## Orders

`POST /api/market/v1/orderService/order/search` with bounded `limit/offset`, positive configured `merchantId`, and deterministic status-date sorting. `shipmentId` is the remote order identity; `offerId` remains the variant mapping key. Buyer/contact/address payload is not projected.

## Errors and limits

Remote HTTP/body/transport text is never exposed. Known auth/rate/service statuses normalize to Connector SDK `RemoteError`. Response bodies are capped at 12 MiB; manifest concurrency and retry are conservative host scheduler guidance.
