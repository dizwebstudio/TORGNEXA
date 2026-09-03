# Task 233: Connector sandbox namespace preflight

## Status

Repository implementation: **Completed** on 2026-09-04.

## Objective

Make the Task-029 Linux qualification distinguish an unavailable kernel
namespace runtime from a failed sandbox probe. A restricted developer or
container environment must report an explicit qualification skip, while an
available namespace runtime must continue to fail closed on launcher or
emulator errors.

## Acceptance

- [x] The host performs a user/mount/network/IPC/UTS namespace preflight before
  staging and launching the external emulator.
- [x] An unavailable Linux namespace runtime returns `ErrSandboxUnavailable`,
  so the existing qualification test reports an explicit kernel-runtime skip.
- [x] A namespace preflight success still executes the real chroot isolation
  probe; non-zero launcher/emulator exits remain hard probe failures.
- [x] No API, event, SDK, credential, egress or persistence contract changes.
- [x] Qualification passes in a namespace-enabled pinned runtime and remains
  truthful in a restricted runtime.

## Validation

- `make sandbox` in a namespace-enabled pinned runtime: PASS.
- `make sandbox` in a restricted runtime: explicit SKIP, process exit 0.
- Full Go test and vet suites, contract, supply-chain, architecture and package
  index checks are required before handoff.

## Boundary

This task changes only runtime availability classification for the existing
Task-029 reference sandbox. It does not weaken the isolation proof, bypass
provider conformance, or enable arbitrary provider admission.
