# Task 052: Procurement core

## Status
`repository-complete` — 2026-08-12.

## Objective
Implement Supplier, SupplierOffer and PurchaseOrder lifecycle.

## Dependencies
004, 005

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
PO state machine, money/quantity rules, audit and import/API tests.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- Supplier references canonical LegalParty identity, SupplierOffer uses exact Money/Quantity, PO transitions are explicit/audited, and strict JSON import is covered by tests.
- Durable expand schema is registered in `migrations/catalog.json`; architecture review `ARCH-052` and an accepted ADR document the frozen-pillar impact.

## Qualification
- targeted unit tests: PASS; full repository qualification is recorded in `VALIDATION_REPORT.md`.
