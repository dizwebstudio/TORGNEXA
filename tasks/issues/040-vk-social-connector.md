# Task 040: VK social connector

## Objective
Implement VK social connector for declared content/media/comment/analytics capabilities.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Publish flow uses universal Content/Publication; retries and remote status mapping tested.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

- Registered provider `vk` against Connector SDK v1 and canonical Task-020 Social Core.
- Admitted `social.post.text`, image/gallery `social.post.media`, `social.comments.read`, `social.comments.reply`, and `social.analytics.read`; video/edit/delete remain fail-closed undeclared.
- Publishing reuses canonical `PublicationID` as VK `wall.post.guid`; comment replies use the caller idempotency key as `wall.createComment.guid`.
- Media is re-opened through Task-088 `MediaAccessor`; remote upload URLs are validated before host-mediated egress.
- Remote status maps exact present post to `published` and exact absence to `failed/remote_missing`.
- Added provider-neutral additive SDK-v1 comment/analytics interfaces without changing frozen `Connector` or `Runtime` roots.
- Added deterministic fixtures/tests, capability audit, conformance evidence and `ARCH-040`.

Next canonical dependency-ready task: `041 Telegram Connector`.
