# Task 050: Advertising engine

## Status
`repository-complete` — 2026-08-12.

## Objective
Implement provider-neutral Campaign/AdGroup/Bid/Budget/Creative core plus connector capability ports.

## Dependencies
017, 049

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Budget/action limits, approval/dry-run and attribution source metadata.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- Provider-neutral Campaign/AdGroup/Creative action planning enforces daily/total/action budget ceilings, explicit attribution metadata, dry-run and approval thresholds before Connector.Apply.
- Durable expand schema is registered in `migrations/catalog.json`; architecture review `ARCH-050` and an accepted ADR document the frozen-pillar impact.

## Qualification
- targeted unit tests: PASS; full repository qualification is recorded in `VALIDATION_REPORT.md`.
