# Task 010

Implement manifest, capability registry, account model, health, normalized remote errors, secret references.

## Status

Completed.

## Repository implementation status

Implementation and Task-025 stabilization are complete; SDK major v1 is dependency-closed. Task 029 sandbox/dry-run enforcement is now complete. Provider admission remains fail-closed until Task 064 conformance completes.

Task 025 is complete and SDK major v1 is stabilized/dependency-closed. Task 029 sandbox/dry-run enforcement is complete. Provider
admission remains blocked until Task 064 conformance completes.

## Implemented

- Connector SDK major v1 root contract and strict manifest-v2 validation.
- Eleven provider-neutral families, including first-class FX and Notification.
- Executable capability registry kept in lockstep with `connector-capabilities.yaml`.
- Structured non-secret auth requirements and bounded rate-limit/retry policy.
- Tenant-owned connector Account model with opaque Task-021 secret references.
- PostgreSQL account repository with optimistic status/health updates.
- Normalized health status/reason codes; unnormalized provider errors are discarded.
- Safe normalized RemoteError taxonomy and deterministic retry delay semantics.
- Tenant-scoped host runtime bridge from SDK `SecretAccessor` to Task-021 SecretProvider.
- Additive migration `000012_connector_sdk.sql`, contracts, fixtures, docs and tests.
- Provider admission remains fail-closed; no provider branch or arbitrary plugin execution is enabled.

## Acceptance

Implementation + tests + updated contracts/docs; run required checks.
