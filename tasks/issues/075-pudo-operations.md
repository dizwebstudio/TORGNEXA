# Task 075: PUDO operations

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- own/external pickup registry, capacity and lifecycle `created→arrived→ready→issued` / `ready→expired→return_pending→returned` are implemented.
- issue/return expose payment/fiscal/report hooks and expiry reconciliation; event evidence is append-only in migration `000048`.
- `ADR 0073`, `ARCH-075`, invalid-transition/capacity/hook tests are included.

## Objective
Implement external/own pickup-point registry, capacity, arrival/ready/issue/expiry/return workflows.

## Dependencies
074, 054

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
PUDO state machine, reconciliation, fiscal/payment hooks and reports.

Run required repository checks and report results, risks and follow-ups.
