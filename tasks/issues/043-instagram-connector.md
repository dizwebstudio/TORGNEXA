# Task 043: Instagram connector

## Objective
Implement professional-account publishing capabilities from current official Meta APIs.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Auth/account constraints documented; media validation and error normalization.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

- Registered provider `instagram` on Connector SDK v1 / Task-020 Social Core using `graph.instagram.com` `v26.0` and Instagram Login for professional Business/Creator accounts.
- Exact numeric Instagram user binding is health-checked; caller input cannot override account identity.
- Admitted `social.post.media` and `social.post.video`; text-only, Stories, comments, insights, edit/delete and mixed media carousel remain undeclared.
- Task-088 media is reopened immediately before staging. Current qualified baseline is JPEG <=8 MiB, MP4 Reel <=300 MiB, caption <=2200 code points and image carousel <=10 items.
- Meta public-URL fetch is isolated behind a host-owned short-lived HTTPS `MediaStager`; internal object keys and secret-bearing provider storage paths never cross the adapter.
- Container creation/readiness and `media_publish` are implemented with exact remote-ID binding. Ambiguous POST transport/5xx fails closed as non-retryable `write_outcome_unknown`.
- Added deterministic fixtures/tests, capability audit, reconciliation/conformance evidence and `ARCH-043`.
- Task-064 provider conformance: 13/13 PASS; report SHA-256 `bd5199ef54e6c9a7bd9eab9f80364db5499dca27331d0001b1c4f9074bfe3d96`.

Next canonical dependency-ready task: `044 Threads Connector`.
