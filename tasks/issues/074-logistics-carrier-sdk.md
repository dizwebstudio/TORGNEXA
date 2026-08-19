# Task 074: Logistics carrier SDK

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- additive logistics SDK covers normalized rates/SLA, shipment create/cancel, label, tracking and return.
- routing consumes normalized same-currency cost and delivery windows; provider tariff/service codes stay outside Core.
- migration `000047`, `ADR 0072`, `ARCH-074` and deterministic routing tests are included; Task `090` remains the reference-carrier proof.

## Objective
Implement rate/create shipment/label/track/cancel/return capabilities plus carrier mapping.

## Dependencies
006, 010

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Routing-ready normalized SLA/costs and status reconciliation.

Run required repository checks and report results, risks and follow-ups.
