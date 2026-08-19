# Task 054 — WMS Inventory Ledger contract v1

Status: repository-qualified.

Warehouse stock is represented by an append-only movement ledger with atomic reservations, quarantine, lots, serials and expiry checks; availability is derived, never hand-edited.

All monetary values use integer minor units plus ISO-shaped currency; tenant scope is mandatory; retries preserve canonical idempotency identity.
