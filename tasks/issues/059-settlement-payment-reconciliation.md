# Task 059: Settlement/payment reconciliation

## Status
`repository-complete` — 2026-08-12.

## Objective
Match expected commerce facts, marketplace settlement entries and bank/acquirer receipts.

## Dependencies
058, 014

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Classify timing/known-fee/unmatched/duplicate/disputed differences with reports.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- Expected commerce facts, settlement entries and receipts are classified into timing/known_fee/unmatched/duplicate/disputed differences; currency mismatch fails closed without FX.
- Durable expand schema is registered in `migrations/catalog.json`; architecture review `ARCH-059` and an accepted ADR document the frozen-pillar impact.

## Qualification
- targeted unit tests: PASS; full repository qualification is recorded in `VALIDATION_REPORT.md`.
