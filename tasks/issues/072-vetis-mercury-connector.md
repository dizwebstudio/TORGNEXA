# Task 072: VetIS/Mercury connector

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- `vetis-mercury` implements document/status read, inventory read, reconciliation and regulated writes through Government Connector SDK ports.
- every regulated write requires explicit `ApprovalRef`; remote VetIS/Mercury state is authoritative.
- migration `000045`, append-only reconciliation evidence, `ADR 0070`, `ARCH-072`, tests and Task-064 conformance are included.

## Objective
Implement GovernmentConnector baseline for supported VetIS/Mercury docs/status/stock reconciliation.

## Dependencies
010, 014, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Official status authoritative and regulated writes approval-gated.

Run required repository checks and report results, risks and follow-ups.
