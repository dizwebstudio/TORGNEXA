# OpenCart connector

Task 096 adds OpenCart through a narrow versioned TORGNEXA bridge extension
contract. This avoids scraping the admin UI, storing OpenCart DB credentials in
TORGNEXA, or coupling the Core to OpenCart internals.

Supported by bridge v1: products read/write, prices read/write, inventory
read/write, orders read and order status writes. A real OpenCart 4.x extension
payload is shipped under
`connectors/storefronts/opencart/extension/torgnexa`; build the installable
package with:

```bash
scripts/package-opencart-bridge.sh /tmp/torgnexa.ocmod.zip
```

Upload that `.ocmod.zip` through **Extensions → Installer**, install the
`TORGNEXA OpenCart Bridge` extension, and configure either the
`TORGNEXA_OPENCART_BRIDGE_TOKEN` PHP environment variable or the hashed
`torgnexa_token_sha256` setting. The extension creates only its two
store-local tables on the first authenticated bridge request and keeps
customer identity out of order responses. SKU reconciliation uses OpenCart
4's native `product_code` table (`code = 'SKU', value = <sku>`), with
`product.model` as a compatibility fallback.

Для локальной проверки без внешнего магазина используйте изолированный
Compose-стенд и smoke-скрипт: [docker-smoke.md](docker-smoke.md). Та же
инструкция опубликована на сайте в разделе
`/docs#opencart-smoke` и снабжена скриншотами демо-магазина.
