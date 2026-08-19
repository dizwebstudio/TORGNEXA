# 1C capability audit

Snapshot: 2026-08-10

| Capability | Status | Evidence |
|---|---|---|
| `erp.catalog.read` | granted | Configured standard OData catalog read; bounded ordered pages; remote revision/archive projection. |
| `erp.inventory.read` | granted | Configured accumulation-register `Balance()` read; exact decimal quantity. |
| `erp.catalog.write` | denied | Task 015 is read-only. |
| `erp.orders.write` | denied | Task 015 is read-only. |
| any direct SQL/file/COM access | denied | Provider imports only standard non-executing libraries plus Connector SDK; host owns transport. |
| CommerceML | separate adapter | Not part of this manifest/runtime. |

Credentials are Basic-auth secret material behind a Task-021 opaque secret reference. Non-secret publication/mapping configuration is host-injected per account and is fingerprint-bound into cursors.
