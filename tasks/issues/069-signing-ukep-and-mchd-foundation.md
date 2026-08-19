# Task 069: Signing, UKEP and MChD foundation

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- isolated signing port uses artifact/digest/certificate/MЧД/approval references; no private key bytes or CSP/PIN material enters generic API/event/plugin shapes.
- certificate metadata, MЧД authority metadata, idempotent sign requests and append-only signing evidence are durable in migration `000042`.
- approval/idempotency boundary is covered by tests; `ADR 0067` and `ARCH-069` are included.

## Objective
Implement isolated Signing Service ports, certificate metadata, signing requests and MChD authority references.

## Dependencies
017, 021, 025

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
No private key crosses generic API/event/plugin boundary; approval/audit tests.

Run required repository checks and report results, risks and follow-ups.
