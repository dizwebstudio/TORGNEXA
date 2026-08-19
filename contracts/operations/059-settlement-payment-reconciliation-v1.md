# Task 059 — Settlement / Payment Reconciliation contract v1

Status: repository-qualified.

Reconciliation classifies differences instead of rewriting financial facts. Same-currency matching remains the default. Cross-currency matching is permitted only through a Task-089b historical converter that returns an immutable persisted conversion-record reference; missing/stale/unevidenced conversions fail explicitly.

All monetary values use integer minor units plus ISO-shaped currency; tenant scope is mandatory; retries preserve canonical idempotency identity.
