# Task 045: OK connector

## Objective
Implement Odnoklassniki connector for supported publishing/media/analytics capabilities.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Current official capability spec and fixtures.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-12

Status: **repository-complete**.

- Registered provider `odnoklassniki` as the next Task-020 Social Core adapter with exact numeric group binding and fixed official REST authority `api.ok.ru`.
- Added signed OAuth REST calls using the official access-token/application-secret signature construction while keeping both secret values Task-021 callback-scoped.
- Admitted `social.post.text`, `social.post.media`, `social.post.video`, and `social.analytics.read` only; user-wall/note, comments, edit/delete, native scheduling, ads and webhooks remain undeclared.
- Added Task-088-backed photo upload through `photosV2.getUploadUrl`, MP4 upload through `video.getUploadUrl` + `video.update`, and group publication through `mediatopic.post` `GROUP_THEME`.
- Added exact group-bound publication status through `mediatopic.getByIds`, bounded topic analytics through `group.getStatTopic`, provider-error normalization, dynamic upload-authority validation and fail-closed ambiguous writes.
- Added deterministic fixtures/tests, capability/spec/reconciliation/conformance docs and architecture review `ARCH-045`.
- Task-064 provider conformance: 13/13 PASS; report SHA-256 `f5cf8ca4b8230b0ef0bdcadb38f43c7b1d749ee2a142275abfbe4c425150b50a`.

Next canonical dependency-ready task: `046 Rutube Connector`.
