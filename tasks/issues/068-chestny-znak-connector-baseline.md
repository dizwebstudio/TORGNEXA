# Task 068: Chestny ZNAK connector baseline

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- `chestny-znak` is admitted as a read/status-first Government provider with National Catalog/product reference lookup, marking status and reconciliation; no marking write capability is declared.
- raw marking codes are request-scoped only; durable facts store SHA-256 fingerprints and remote authoritative status.
- migration `000041`, `ADR 0066`, `ARCH-068`, deterministic tests and Task-064 conformance evidence are included.

## Objective
Implement official read/status/National Catalog integration and marking reconciliation; phase writes separately.

## Dependencies
010, 014, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Official status authoritative, sensitive code data protected, fixtures/audit.

Run required repository checks and report results, risks and follow-ups.
