# MoySklad Capability Audit

| Capability | Decision | Evidence |
|---|---|---|
| `erp.catalog.read` | granted | `/entity/assortment`, product-only grouping |
| `erp.inventory.read` | granted | `/report/stock/bystore` |
| `erp.orders.read` | granted | `/entity/customerorder` |
| `erp.catalog.write` | denied | Task 016 is read-only |
| `erp.orders.write` | denied | Task 016 is read-only |

No generic marketplace/social/government/payment capability is granted.
