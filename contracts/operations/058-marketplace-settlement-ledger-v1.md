# Task 058 — Marketplace Settlement Ledger contract v1

Status: repository-qualified.

Marketplace settlements are immutable append-oriented facts keyed by provider references. Corrections are new adjustment entries and the original provider amount/currency is always preserved. After Task 089b an `FXRateRef` may reference immutable conversion evidence for a derived view; it never authorizes mutation of the source settlement entry.

All monetary values use integer minor units plus ISO-shaped currency; tenant scope is mandatory; retries preserve canonical idempotency identity.
