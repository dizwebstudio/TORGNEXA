# PrestaShop capability audit — 2026-08-12

Primary documentation reviewed:
- https://devdocs.prestashop-project.org/9/webservice/
- https://devdocs.prestashop-project.org/9/webservice/getting-started/
- https://devdocs.prestashop-project.org/9/webservice/cheat-sheet/
- https://devdocs.prestashop-project.org/9/webservice/resources/products/
- https://devdocs.prestashop-project.org/9/webservice/resources/combinations/
- https://devdocs.prestashop-project.org/9/webservice/resources/stock_availables/
- https://devdocs.prestashop-project.org/9/webservice/resources/orders/
- https://devdocs.prestashop-project.org/9/webservice/resources/order_details/
- https://devdocs.prestashop-project.org/9/webservice/resources/order_histories/

Important findings: Webservice reads can output JSON, but current documentation states JSON cannot be used as Webservice input; PATCH supports partial XML updates. StockAvailable is the authoritative available-stock entity. PrestaShop 9 also exposes a newer OAuth2 Admin API, deliberately deferred from this compatibility-first connector.

The production runtime now admits `orders.read` and `orders.status.write` in
addition to the previously qualified catalog/price/inventory surfaces. Order
status IDs are installation-specific: runtime configuration must provide a
unique mapping for all five canonical lifecycle states. Reads exclude customer
PII; status writes use `order_histories` and verify the resulting state.
