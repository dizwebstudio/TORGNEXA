# Task 022: Notifications

## Status
Repository-complete. Live PostgreSQL RLS execution and production identity/provider wiring remain deployment evidence, not missing repository implementation.

## Objective
Implement a tenant-scoped notification inbox and provider-neutral channel delivery boundary with deduplication, severity, recipient preferences, suppression and first Web UI/webhook providers.

## Dependencies
007, 063

## Deliverables
Canonical notification model, recipient inbox, deterministic dedupe/escalation behavior, channel preferences and delivery evidence, PostgreSQL repository/migration, authenticated inbox/preferences API, Web UI provider, Task-063 durable-webhook adapter, notification event contract, Draft 2020-12/OpenAPI schemas, architecture/governance evidence and tests.

## Acceptance
Distinct repeated conditions with the same tenant/recipient/dedupe key collapse into one inbox item and increment occurrence count without repeated fan-out; a retry of the same source occurrence keeps that count stable and may re-attempt a failed provider delivery. Severity never decreases and escalation is re-delivered. Web UI is enabled by safe default while external webhook delivery is explicit opt-in. Client-controlled tenant/recipient selectors are rejected, all PostgreSQL tables use explicit tenant predicates plus forced RLS, delivery history stores bounded machine error codes only with immutable attempts bounded to 64, credential-shaped title/body values are rejected, enabled-but-unwired channels are recorded as provider failures rather than preference suppression, and webhook egress is delegated to Task 063 rather than duplicating HTTP/retry/signing logic.
