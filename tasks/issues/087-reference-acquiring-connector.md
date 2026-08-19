# Task 087: Reference Acquiring Connector

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- YooKassa PaymentProvider reference with idempotent create/status/refund/webhook/reconciliation and conformance.
- Architecture review `ARCH-087` and executable tests are included.

## Objective
Implement one production reference acquiring/card payment connector (default candidate YooKassa after fresh API audit) to prove PaymentProvider beyond SBP.

## Dependencies
010, 021, 024, 059, 064, 073

## Deliverables
Current Connector Spec, manifest/capabilities, payment/status/webhook/refund/idempotency/reconciliation fixtures and conformance report.

## Acceptance
No PAN/CVV storage; duplicate callbacks safe; full/partial refund scenarios tested; provider specifics stay inside connector.

Run required repository checks and report results, risks and follow-ups.