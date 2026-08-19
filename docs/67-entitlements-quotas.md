# Entitlements and quotas

Task 028 provides a provider-neutral feature and quota service. It is deliberately separate from Task 086 Cloud Billing.

## Feature evaluation

A feature is identified by a stable key such as `reports.export` or `connectors.marketplace.write`. Rules are tenant/workspace scoped, append-only versions with `effective_from` / optional `effective_until`, source and enabled state. The latest effective version wins. No effective rule means deny.

Never branch application behavior on a commercial plan name. Cloud/Enterprise/Community provisioning produces the same entitlement records.

## Quota enforcement

A quota policy binds a metric key to a non-negative limit and one explicit window:

- `lifetime`;
- `calendar_day_utc`;
- `calendar_month_utc`.

Consumers submit a positive amount plus an immutable usage ID and correlation ID. PostgreSQL serializes the same usage ID and locks the bucket counter before applying the increment. A retry with identical usage evidence is idempotent; reuse of the same usage ID with different evidence fails closed. A request that would exceed the current effective limit is rejected atomically.

Quota policy changes are versioned. Existing usage remains immutable and records the exact policy version used at consumption time.

## Host guard

`internal/platform/entitlementguard` evaluates the feature before touching quota state. If feature access is denied, quota is not consumed. Callers must not catch an entitlement/quota error and continue with the protected operation.

## Billing separation

Task 086 may later synchronize subscription state to entitlement/quota records. The entitlements package contains no subscription plan type and Community self-hosted operation does not require Cloud Billing availability.
