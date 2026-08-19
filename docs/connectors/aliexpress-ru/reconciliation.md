# AliExpress RU Reconciliation Notes

Task 036 contributes only the product side of the remote state.

- Product remote identity: AliExpress RU product `id`.
- Variant remote identity: `sku_id`, falling back to the internal SKU `id` only when the Russia product response omits `sku_id`.
- Seller SKU: `code`.
- Remote observation timestamp: `ali_updated_at` in UTC.

Task 013 Sync Engine owns propagation/checkpoints/loop prevention and Task 014 owns drift evidence/actions. The connector itself never writes canonical catalog state directly.

Because `inventory.read`, `prices.read` and `orders.read` are not admitted, reconciliation must not infer those domains from product payload fields. In particular, deprecated stock fields are ignored.
