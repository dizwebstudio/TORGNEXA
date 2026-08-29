# TORGNEXA OpenCart bridge v1

This directory is the OpenCart 4.x extension payload. Package it with
`scripts/package-opencart-bridge.sh`, upload the resulting
`torgnexa.ocmod.zip` in **Extensions → Installer**, and install the
`TORGNEXA OpenCart Bridge` extension.

The bridge exposes the versioned routes used by the connector:

* `health`, `products`, `product`, `product-by-sku`, `variant`, `orders`,
  `order` (GET);
* `product` (POST/PUT), `variant-price`, `variant-inventory` and
  `order-status` (PUT).

OpenCart sanitizes hyphens while resolving controller paths. The files
`productbysku.php`, `variantprice.php`, `variantinventory.php` and
`orderstatus.php` therefore intentionally back the public hyphenated routes.

## Token configuration

For a container or staging shop, set `TORGNEXA_OPENCART_BRIDGE_TOKEN` in the
OpenCart PHP process environment. The bridge compares only SHA-256 digests and
never logs or returns the token. A deployed shop may instead set the
`torgnexa_token_sha256` OpenCart setting to the lowercase SHA-256 digest of the
token; the raw token must not be stored in the database.

The first authenticated bridge request creates two store-local tables:

* `oc_torgnexa_idempotency` for replay/conflict protection;
* `oc_torgnexa_variant_meta` for the optional compare-at price that OpenCart's
  base product table does not model.

OpenCart 4 stores SKU values in `product_code.code/value` with `code = 'SKU'`;
the bridge uses that native table and falls back to the legacy `product.model`
value when no SKU row exists.

OpenCart has no separate private/archived product state. Provider-neutral
`private` and `archived` writes are therefore stored as unpublished (`draft`)
and reconciled as the same state by the connector.

The projection deliberately excludes customer names, addresses, email,
telephone, payment and shipping details from order responses.
