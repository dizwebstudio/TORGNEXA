# Yandex Market Capability Audit

| Capability | Decision | Evidence |
|---|---|---|
| `products.read` | granted | business offer mappings |
| `prices.read` | granted | campaign prices or business basic price |
| `inventory.read` | granted | explicit partner/campaign stock mode |
| `orders.read` | granted | business orders |
| `notifications.receive` | granted | bounded inbound notification decoder + deterministic dedupe |
| `products.write` | denied | no faithful provider-neutral desired-state contract is admitted |
| `prices.write` | granted | Task 116 exact business-wide/campaign price update with eventual reconciliation |
| `inventory.write` | denied | separate risk-gated task required |
| `orders.status.write` | denied | separate approval/idempotency/risk review required |

No generic ERP/social/government/payment write authority is granted.
