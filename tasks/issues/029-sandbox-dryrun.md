# Task 029

Add dry-run operation result model and test connector/emulator package; prove production credential isolation.

## Status

Repository implementation: **Completed** on 2026-08-09.

## Acceptance

- [x] Provider-neutral dry-run/test operation result model with bounded change hashes, external-action intents, normalized reasons, usage and isolation evidence.
- [x] Deterministic emulator package exercises capability, secret and network boundaries without provider-specific branching.
- [x] Dry-run never calls secret providers or network transports.
- [x] Test mode rejects production-tier credentials before secret-broker use; only sandbox-tier credentials may resolve.
- [x] Exact granted egress is host-mediated; DNS is resolved on each call, special/private addresses are rejected and transport receives pinned IP + TLS server name to prevent rebinding.
- [x] Linux reference sandbox uses user/mount/network/IPC/UTS isolation, minimal chroot/environment and signed wall/CPU/RSS/output/concurrency limits.
- [x] External process probe demonstrates host production environment, host secret filesystem and direct network are not visible/reachable.
- [x] Draft 2020-12 dry-run/probe contracts and positive/negative fixtures are included.
- [x] Linux runtime qualification is part of `make check` where Linux is used; missing `unshare` fails the Linux qualification rather than silently degrading.
- [x] Architecture ADR/review register the new host sandbox modules while provider inventory remains empty and provider admission remains disabled until Task 064.

## Boundary

Task 029 does not admit arbitrary provider artifacts. It makes Task-025 grants/limits executable for dry-run/test and supplies the reference isolation runtime. Task 064 still owns provider conformance and is the remaining prerequisite before provider admission can be considered.
