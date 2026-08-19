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

OpenCart option/variant authoring and distribution as a signed Marketplace `.ocmod.zip` are deliberately separate from connector admission.
