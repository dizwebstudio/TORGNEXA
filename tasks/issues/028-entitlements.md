# Task 028: Entitlements and quotas

## Objective
Implement a tenant/workspace feature entitlement interface and strict quotas without hard-coded subscription-plan branches. Community runtime must remain independent from Cloud Billing.

## Dependencies
082

## Deliverables
Versioned entitlement rules, quota policies, atomic/idempotent quota consumption, host guard, PostgreSQL RLS storage, API/contracts/events, audit/outbox/lineage evidence and migration/rehearsal coverage.

## Acceptance
Missing rules/policies fail closed; quota concurrency cannot exceed the effective limit; retries with the same usage id are idempotent and collisions fail; no `if plan ==` branching exists; Community does not depend on Task 086; required repository checks pass.
