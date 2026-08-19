# Task 044: Threads connector

## Objective
Implement Threads publishing/account capabilities through Social Connector SDK.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Capability and token lifecycle tests; no Core changes.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

- Registered provider `threads` on Connector SDK v1 using official `graph.threads.net` `v1.0`; frozen Connector/Runtime roots and Core remain unchanged.
- Exact Threads user binding is health-checked; admitted `social.post.text`, `social.post.media` and `social.post.video` with current 500-code-point text limit, JPEG/PNG <=8 MiB, MP4 <=1 GiB and image carousel <=20 items.
- Task-088 media is reopened immediately before host-owned short-lived HTTPS staging; internal object keys do not become Meta parameters.
- Container creation/status and `threads_publish` are implemented with exact remote-ID binding; ambiguous POST transport/5xx is non-retryable `write_outcome_unknown`.
- Added provider-local short-to-long token exchange (`th_exchange_token`) and long-lived refresh (`th_refresh_token`). Current user token and app secret remain byte-scoped transport credentials; replacement token is rotated only through host `TokenSink`, zeroed after use and never returned as result data.
- Added deterministic fixtures/tests, capability audit, reconciliation/conformance evidence and `ARCH-044`.
- Task-064 provider conformance: 13/13 PASS; report SHA-256 `7cff0b48a14b0f95708c769d6226a038158cfd5d718d70c30490a3c335359daa`.

Next canonical dependency-ready task: `045 OK Connector`.
