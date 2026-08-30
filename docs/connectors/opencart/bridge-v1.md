# TORGNEXA OpenCart bridge v1

The bridge is an OpenCart 4.x extension boundary, not a Core TORGNEXA service.

Required routes:
- GET health
- GET products?page=&limit=
- GET product?id=
- GET product-by-sku?sku=
- POST/PUT product
- GET variant?remote_id=
- PUT variant-price
- PUT variant-inventory
- GET orders?page=&limit=
- GET order?id=
- PUT order-status

Every write receives an idempotency key. The bridge must persist/reject conflicting replays in store-local extension storage and must not return customer billing/shipping PII in order JSON.

The OpenCart connector runtime requires a tenant-scoped `order_statuses` map
with unique positive numeric IDs for `pending`, `confirmed`, `processing`,
`fulfilled` and `cancelled`. Status IDs are installation-specific and are sent
to the bridge only after this explicit configuration is validated.

OpenCart option/variant authoring and distribution as a signed Marketplace `.ocmod.zip` are deliberately separate from connector admission.

## Reference extension

This repository includes a reference OpenCart 4.x extension in
`connectors/storefronts/opencart/extension/torgnexa`. It is packaged with
`scripts/package-opencart-bridge.sh` and installed through the native OpenCart
Extension Installer. The extension uses the namespaced MVC-L structure
documented by OpenCart and performs all catalog/stock/order queries through
OpenCart's own database layer.

OpenCart sanitizes `-` characters when resolving controller paths. The public
routes `product-by-sku`, `variant-price`, `variant-inventory` and `order-status`
are consequently backed by the files `productbysku.php`, `variantprice.php`,
`variantinventory.php` and `orderstatus.php`.

The bridge accepts a bearer token from `TORGNEXA_OPENCART_BRIDGE_TOKEN` (or
`TORGNEXA_BRIDGE_TOKEN` for compatibility) and compares only its SHA-256 digest.
For a persistent store configuration, set the lowercase digest in
`torgnexa_token_sha256`; never store the raw token in `oc_setting`.
