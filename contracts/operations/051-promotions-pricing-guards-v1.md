# Task 051 — Promotions & Pricing Guards contract v1

Status: repository-qualified.

Promotion bulk writes are previewed before execution and fail closed on floor-price or minimum-margin violations; approval is required above policy-defined blast-radius thresholds.

All monetary values use integer minor units plus ISO-shaped currency; tenant scope is mandatory; retries preserve canonical idempotency identity.
