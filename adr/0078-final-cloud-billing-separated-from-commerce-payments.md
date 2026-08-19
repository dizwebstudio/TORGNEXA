# ADR 0078: Cloud billing separated from commerce payments

## Status
Accepted

## Context
Task 086 needs SaaS subscription lifecycle without coupling Community runtime or marketplace/customer payment correctness to TORGNEXA Cloud billing.

## Decision
Model versioned plans, subscriptions, immutable usage, invoices/adjustments and grace/suspension separately from commerce payments. Entitlements synchronize through an injected sink; Community mode bypasses billing.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Reusing customer order payments as subscription billing was rejected. Hard-wiring feature checks to plan names was rejected in favor of Task 028 entitlements.

## Compatibility impact
Community and existing commerce payment paths remain unchanged; Cloud billing is additive.

## Migration and data impact
Expand-only migration 000053 creates plan, subscription, usage, invoice and append-only adjustment data.

## Security and privacy impact
Raw payment credentials are never stored; only provider payment references are linked to invoices and all usage is tenant scoped.

## Operational impact
Billing outages may delay commercial state but must not mutate commerce state; grace/suspension transitions are explicit and auditable.
