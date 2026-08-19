# Task 052 — Procurement Core contract v1

Status: repository-qualified.

Suppliers reference canonical legal parties; supplier offers and purchase orders use exact Money/Quantity primitives and an explicit purchase-order lifecycle with auditable transitions.

All monetary values use integer minor units plus ISO-shaped currency; tenant scope is mandatory; retries preserve canonical idempotency identity.
