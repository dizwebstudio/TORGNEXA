# Task 046: Rutube connector

## Objective
Implement video-platform connector mapping VideoContent, metadata, scheduling/comments/analytics where supported.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Media upload/status state machine and quota/error tests.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-12

Status: **repository-complete**.

- Registered provider `rutube` with the deliberately narrow `social.post.video` capability and exact `ChannelID` binding.
- Added an endpoint-free typed `PartnerTransport`; production transport must be backed by the current official account-specific RUTUBE partner contract. Studio cookies, browser automation, private/guessed endpoints and reverse-engineered RPCs are forbidden.
- Added Task-088 released MP4 validation and the explicit create-session -> upload -> commit -> processing/published/failed state machine.
- Propagated canonical PublicationID as partner external ID for reconciliation; remote identities are `rutube:<channel-id>:<video-id>` and reject foreign channels.
- Added bounded metadata mapping, upload-session size/expiry checks, quota/rate-limit normalization and fail-closed `upload_outcome_unknown` / `write_outcome_unknown` semantics.
- Added deterministic fixtures/tests, capability/spec/reconciliation/conformance docs and architecture review `ARCH-046`.
- Task-064 provider conformance: 13/13 PASS; report digest `bc56983857c6872347f27f2111d564ee919069cdec99feef0e652bfbe055fc4f`.

Next canonical dependency-ready task: `048 YouTube Connector` (Task `047` Dzen is already repository-complete).
