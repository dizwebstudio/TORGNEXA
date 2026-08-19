# Magnit Market Reconciliation

Task 035 uses the existing Task 010/013/014 primitives without provider-specific Core state.

## Mapping keys

| TORGNEXA concept | Magnit Market remote key |
|---|---|
| Product projection | `<product_id>:<sku_id>` |
| Variant | `sku_id` |
| Seller alias | `seller_sku_id` |
| Order | `order_id` |
| Inventory aggregate | `shop:<shop_id>:stock-type:<type>` |

No `shop_id`, `product_id`, `sku_id`, order ID or stock-type branch is added to Core domain models.

## Drift strategy

- Product content drift: Task 014 compares the normalized payload fingerprint; `/price/info.timestamp` is observation metadata, not a substitute for content fingerprinting.
- Price drift: exact decimal strings and official price timestamp are compared.
- Inventory drift: compare aggregate available quantity for the configured stock type only.
- Order drift: compare order status and item quantities; buyer fields are intentionally excluded.
- Missing/duplicate mapping: existing Task 014 mapping drift classes apply unchanged.

Task 035 is read-only, so reconciliation can notify/approval/ignore but cannot execute a Magnit Market mutation through this provider version.
