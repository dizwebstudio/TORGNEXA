# Task 025

Design and ADR the transition from in-tree official connectors to isolated third-party plugin runtime; no arbitrary code execution yet.

## Status

Completed.

## Implemented

- Accepted ADR-0037 for the isolated third-party security boundary.
- Signed artifact identity: SHA-256 bytes plus Ed25519 publisher signature/key fingerprint.
- Least-privilege capability, secret-class and exact TLS egress permission requests/grants.
- Grants are bound to exact connector id/version/artifact digest and cannot exceed the signed request.
- Resource ceilings are versioned by Task 025 and are now enforced by the Task-029 reference sandbox runtime.
- No process launcher, dynamic loading, WASM execution, command/environment/filesystem authority or arbitrary code execution.
- Architecture gate blocks provider direct process/plugin/syscall/unsafe, filesystem/environment and network imports.
- Connector SDK v1 root surfaces are frozen by regression tests.
- Task 029 sandbox/dry-run enforcement is complete; provider admission remains disabled until Task 064 conformance completes.

## Acceptance
Implementation + tests + docs/contracts; run required checks.
