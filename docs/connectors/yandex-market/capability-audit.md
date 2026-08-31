# Yandex Market Capability Audit — 2026-08-31

| Capability | Decision | Evidence |
|---|---|---|
| `products.read` | granted | business offer mappings |
| `prices.read` | granted | campaign prices or business basic price |
| `inventory.read` | granted | explicit partner/campaign stock mode |
| `inventory.write` | granted | documented partner-warehouse POST and grouped-warehouse PUT; asynchronous acceptance is reconciled later |
| `orders.read` | granted | business orders |
| `notifications.receive` | granted | bounded inbound notification decoder + deterministic dedupe |
| `products.write` | granted | Task 217 bounded business offer-mappings update; media and provider-specific attributes remain deferred |
| `prices.write` | granted | Task 116 exact business-wide/campaign price update with eventual reconciliation |
| `orders.status.write` | denied | separate approval/idempotency/risk review required |

No generic ERP/social/government/payment write authority is granted.

The publication adapter calls the official business offer-mappings update
surface with a validated snapshot and treats the result as asynchronous
acceptance. A bounded offer read is required before `published`; unsupported
media or category-attribute bridges fail closed.
