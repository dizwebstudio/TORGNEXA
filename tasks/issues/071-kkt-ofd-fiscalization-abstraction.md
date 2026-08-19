# Task 071: KKT/OFD fiscalization abstraction

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- provider-neutral sale/refund/correction models use exact Money, immutable external/idempotency refs and marking-code fingerprints; Connector SDK v1 now exposes additive fiscal receipt/status capability interfaces without changing its root Connector/Runtime.
- correction requests reference the original calculation; provider reconciliation returns authoritative fiscal state/document refs.
- migration `000044`, `ADR 0069`, `ARCH-071` and idempotency/marking tests are included.

## Objective
Implement fiscal receipt/refund/correction models and provider SDK without embedding vendor specifics.

## Dependencies
006, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Idempotent fiscal request refs, marking links and reconciliation status.

Run required repository checks and report results, risks and follow-ups.
