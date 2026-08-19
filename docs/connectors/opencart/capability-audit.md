# OpenCart capability audit — 2026-08-12

Primary sources reviewed:
- https://docs.opencart.com/developer-guide/extensions
- https://github.com/opencart/opencart/tree/master/upload/catalog/controller/api
- https://github.com/opencart/opencart/blob/master/upload/admin/controller/sale/order.php

Finding: OpenCart 4.x supports extension-based APIs and has native API controllers centered on storefront checkout/order behavior, but TORGNEXA needs a stable external catalog/inventory/write contract across OpenCart versions. A dedicated extension boundary is therefore safer than remote DB access or admin-session automation.
