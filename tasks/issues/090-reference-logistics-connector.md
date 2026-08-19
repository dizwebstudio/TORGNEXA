# Task 090: Reference Logistics Connector

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- CDEK Logistics reference with rates/shipment/track/cancel/label/PVZ/return and conformance.
- Architecture review `ARCH-090` and executable tests are included.

## Objective
Implement one production reference carrier connector (default candidate CDEK after fresh API audit) to prove Logistics SDK/conformance.

## Dependencies
010, 014, 021, 024, 064, 074, 075

## Deliverables
Connector Spec, rates/shipment/cancel/track/label/PUDO/return capabilities as supported, webhooks/polling, mappings, fixtures and conformance.

## Acceptance
Async status updates are idempotent; remote/local drift reconciles; provider tariff/service IDs do not leak into Core fields.

Run required repository checks and report results, risks and follow-ups.