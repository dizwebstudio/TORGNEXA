# Task 086: Cloud Billing Lifecycle

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- plan/subscription/usage/invoice lifecycle, Community bypass, grace/suspension and migration 000053.
- Architecture review `ARCH-086` and executable tests are included.

## Objective
Implement TORGNEXA Cloud subscription/billing lifecycle separate from commerce payments: plan versions, subscriptions, usage metering, invoices, grace/suspension/renewal and entitlement synchronization.

## Dependencies
028, 049, 058, 073, 076

## Deliverables
Billing domain/migrations/API/events/metering/invoice state machine/provider-payment reference/reconciliation and Community bypass configuration.

## Acceptance
Community does not depend on billing; billing outage does not corrupt commerce state; usage and invoice adjustments are auditable/idempotent.

Run required repository checks and report results, risks and follow-ups.