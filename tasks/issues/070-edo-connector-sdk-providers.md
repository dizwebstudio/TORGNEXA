# Task 070: EDO connector SDK + providers

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- additive Connector SDK v1 EDO reader/sender/sign-workflow interfaces keep root `Connector`/`Runtime` frozen.
- baseline `diadoc` and `saby-edo` providers consume already signed artifact/signature references and treat remote status as authoritative.
- migration `000043`, `ADR 0068`, `ARCH-070`/`ARCH-070B`, provider fixtures/docs and 13-check conformance reports are included.

## Objective
Implement provider-neutral EDO SDK and baseline Diadoc/Saby adapters for supported doc/status flows.

## Dependencies
069, 010, 014

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Remote provider status authoritative; signed-doc workflow and conformance fixtures.

Run required repository checks and report results, risks and follow-ups.
