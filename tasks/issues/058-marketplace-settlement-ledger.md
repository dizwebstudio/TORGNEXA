# Task 058: Marketplace settlement ledger

## Status
`repository-complete` — 2026-08-12.

## Objective
Implement normalized append-oriented SettlementEntry store/import APIs.

## Dependencies
006, 049

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Schema contract, provider refs, order links, adjustment-not-rewrite semantics.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- Settlement entries are append-only, provider-reference idempotent, order-linked and original-currency preserving; corrections require a new adjustment row that points to the prior entry.
- Durable expand schema is registered in `migrations/catalog.json`; architecture review `ARCH-058` and an accepted ADR document the frozen-pillar impact.

## Qualification
- targeted unit tests: PASS; full repository qualification is recorded in `VALIDATION_REPORT.md`.
