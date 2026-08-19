# Task 073: Payments/SBP provider SDK

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- additive payment SDK covers create/status/refund/reconcile/verified webhooks; raw card fields do not exist in the contract.
- `sbp` baseline requires create/refund idempotency and verified callbacks with replay evidence/body digest; remote status/commission is authoritative.
- migration `000046`, `ADR 0071`, `ARCH-073`, tests and Task-064 conformance are included.

## Objective
Implement PaymentProvider abstraction for create/status/refund/commission/reconcile and SBP adapter baseline.

## Dependencies
006, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
No raw card data storage; idempotency and webhook verification.

Run required repository checks and report results, risks and follow-ups.
