# Task 051: Promotions and pricing guards

## Status
`repository-complete` — 2026-08-12.

## Objective
Implement Promotion/Coupon/Discount participation plus floor-price/margin guard rules.

## Dependencies
005, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Mass writes preview affected SKUs and block floor/margin violations by policy.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- Promotion/Coupon guard preview returns the exact affected SKU set and blocks floor-price/minimum-margin violations before mass writes; policy can require approval by blast radius.
- Durable expand schema is registered in `migrations/catalog.json`; architecture review `ARCH-051` and an accepted ADR document the frozen-pillar impact.

## Qualification
- targeted unit tests: PASS; full repository qualification is recorded in `VALIDATION_REPORT.md`.
