# Change scope

- Task/issue:
- Architecture review record (when triggered):
- Change class: implementation / new domain / new provider / pillar / mixed

## Architecture and compatibility

- [ ] Every changed Core/Platform/process or protected policy path is listed
  exactly in a newly added `architecture/reviews/NNN-*.json` record.
- [ ] The gap audit covers tenancy, privacy/data governance, API/events,
  plugin SDK, migrations, security, approvals, audit/lineage,
  reconciliation/idempotency, webhooks/egress, SLO, operations/rollback, and
  tests/conformance with meaningful evidence.
- [ ] A frozen-pillar, architecture-gate, or mixed provider/Core change adds a
  new or superseding ADR with compatibility, migration/data,
  security/privacy, and operational impact sections.
- [ ] Public contract changes pass Task 024; database changes have Task 067
  catalog/plan/rehearsal evidence.

## Provider changes

- [ ] Not applicable, or provider admission is enabled by the reviewed policy
  after Tasks 010, 025, 029, and 064.
- [ ] Provider code lives only under a registered connector/plugin root and
  routes through Connector SDK/approved ports, never Core/DB internals.
- [ ] Manifest, Connector Spec, capability audit, and conformance plan are
  present and referenced by the review record.

## Verification

- [ ] `./scripts/check.sh` passes.
- [ ] Relevant race, migration/runtime, contract, security, and conformance
  checks pass; evidence and residual risks are reported.

Unchecked boxes guide reviewers but are not acceptance evidence. The
machine-readable record and required CI checks are authoritative.
