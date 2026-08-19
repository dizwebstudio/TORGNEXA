# Task 064: Connector conformance harness

## Objective
Build reusable conformance runner and machine-readable report for official/community plugins.

## Dependencies
010, 025, 029

## Status

Repository implementation: **Completed** on 2026-08-09.

Provider admission intentionally remains disabled in this change. Task 080 requires Tasks 010, 025, 029, and 064 to already be completed in the merge base before the separate admission-control change may set `provider_admission.enabled=true`.

## Implemented

- Reusable Connector SDK v1 conformance runner at `internal/platform/connectors/conformance`.
- Thirteen mandatory no-skip checks covering manifest/SDK, auth, health, normalized errors, rate-limit/retry, idempotency, webhook replay, tenant isolation, Task-029 dry-run side-effect suppression, production-credential rejection, egress grants, resource-limit failure, and sandbox isolation.
- Provider-supplied `Candidate` adapter remains inside the approved Connector SDK import prefix and exposes observable test behavior rather than provider internals.
- Machine-readable `conformance-report-v1` with ordered checks, bounded reason codes, UTC completion timestamp and SHA-256 mutation detection; raw provider errors/secrets/PII are not report fields.
- Reference conformance executable and Linux qualification script use the deterministic Task-029 emulator and external namespace/chroot probe.
- `make check` now includes the reference conformance qualification on Linux.
- Architecture checker requires every future active provider to preserve a canonical passing `docs/connectors/<id>/conformance-report.json` whose connector id matches the policy record.
- Accepted ADR-0039 and ARCH-064 classify the conformance/admission evidence change inside the existing connector-plugin runtime pillar.
- No provider implementation is added and no database migration is introduced.

## Acceptance

- [x] Auth boundary covered.
- [x] Health normalization covered.
- [x] Normalized error taxonomy covered.
- [x] Rate limit / bounded retry covered.
- [x] Idempotent repeated write covered.
- [x] Webhook replay/deduplication covered.
- [x] Tenant isolation covered.
- [x] Task-029 dry-run side-effect suppression covered.
- [x] Production-credential rejection before secret-broker use covered.
- [x] Exact egress grant deny/allow behavior covered.
- [x] Resource-limit failure covered.
- [x] Linux sandbox isolation evidence covered.
- [x] Machine-readable strict report contract and positive/negative fixtures included.
- [x] Provider admission stays fail-closed in the completion change and may only open in a later protected change whose merge base already contains completed Task 064.

## Boundary

Task 064 certifies Connector SDK/security/sandbox behavior. It does not replace provider-specific semantic tests, reconciliation correctness, Task 063 durable outbound webhook delivery, or approval/lineage requirements for sensitive writes.
