# WooCommerce

Task 094 storefront connector for the current WooCommerce WP REST API v3 (`/wp-json/wc/v3`). It is a bidirectional marketplace-family adapter covering products/variations, prices, managed stock, orders, order refunds and signed webhooks. Provider-specific identities stay behind Connector SDK mappings.

Official documentation: https://developer.woocommerce.com/docs/apis/rest-api/v3/

Локальная проверка официального REST API на синтетическом WordPress +
WooCommerce стенде: [docker-smoke.md](docker-smoke.md). В ней описаны Docker
Compose, Basic Auth по TLS, smoke-команды, демо-данные и очистка.
