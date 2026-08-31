# Yandex Market Capability Audit

| Capability | Decision | Evidence |
|---|---|---|
| `products.read` | granted | business offer mappings |
| `prices.read` | granted | campaign prices or business basic price |
| `inventory.read` | granted | explicit partner/campaign stock mode |
| `inventory.write` | granted | documented partner-warehouse POST and grouped-warehouse PUT; asynchronous acceptance is reconciled later |
| `orders.read` | granted | business orders |
| `notifications.receive` | granted | bounded inbound notification decoder + deterministic dedupe |
| `products.write` | denied | no faithful provider-neutral desired-state contract is admitted |
| `prices.write` | granted | Task 116 exact business-wide/campaign price update with eventual reconciliation |
| `orders.status.write` | denied | separate approval/idempotency/risk review required |

No generic ERP/social/government/payment write authority is granted.
