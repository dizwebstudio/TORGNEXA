# AliExpress RU Conformance Plan

Task-064 admission uses the common 13-check Connector SDK v1 suite with the `aliexpress-ru` manifest.

Provider-specific repository evidence additionally covers:
- manifest contains only `products.read`;
- JWT-shaped `X-Auth-Token` secret boundary;
- fixed `openapi.aliexpress.ru` host and host-injected transport;
- bounded product cursor and `last_product_id` continuation;
- duplicate product/variant rejection;
- non-null API error envelope rejection without body leakage;
- deprecated stock fields ignored and unable to grant inventory authority;
- rate-limit response normalization without token/body leakage.

Live qualification must use a non-production seller credential in the staging secret store. Production credentials remain forbidden by the Task-064 test harness.
