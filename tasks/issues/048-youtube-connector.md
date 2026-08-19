# Task 048: YouTube connector

## Status
`repository-complete` — 2026-08-12.

## Objective
Implement YouTube Data API connector for video upload/metadata/schedule/comments/analytics where permitted.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Resumable upload behavior, quota handling and fixtures.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- provider `youtube` is admitted through Connector SDK v1 with `social.post.video` and `social.comments.read` only;
- exact authenticated-channel binding rejects a foreign configured ChannelID;
- OAuth material is consumed only through Task-021 `SecretAccessor`;
- Task-088 released video is reopened immediately before upload and remains bounded by the frozen 10 GiB `MediaDescriptor` contract;
- resumable upload uses opaque host-owned session identity, 256 KiB-aligned chunks, explicit status probes after ambiguous chunk outcomes, and exact confirmed-offset resume;
- upload/session ambiguity, quota exhaustion, rejection and processing failures normalize to bounded provider-neutral errors without raw tokens, URLs or response bodies;
- video status reconciliation is account/channel bound and maps upload/processing/rejection state without inventing provider state;
- comments are bounded top-level reads with opaque pagination; comment writes are deliberately not admitted because the frozen caller surface cannot prove durable provider-side dedup after an ambiguous write;
- native YouTube scheduling and analytics are deliberately not admitted: Task-020 remains the canonical scheduler and the frozen analytics projection cannot represent the YouTube reporting window without semantic loss;
- deterministic fixtures/tests, `ARCH-048`, capability audit/spec/reconciliation docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `746271122440b10f7316de830ef3368f31fb99345709ca213626c2fd8b10632d`.
