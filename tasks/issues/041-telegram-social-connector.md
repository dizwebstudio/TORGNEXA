# Task 041: Telegram social connector

## Objective
Implement Telegram channel connector with text/media/album/button/edit/delete capabilities as supported.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Bot/channel auth isolation, rate-limit handling and fixtures.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

- Registered provider `telegram` against Connector SDK v1 and canonical Task-020 Social Core.
- Admitted text, photo/gallery, MP4 video, HTTPS URL-buttons, bounded single-message edit and bounded delete capabilities; comments/analytics/inbound callbacks remain fail-closed undeclared.
- Bot/channel auth is exact: one negative numeric channel ID, `getMe -> getChatMember` health proof, bot token only behind Task-021 `SecretAccessor` and no token in normalized provider params.
- Media is re-opened through Task-088 immediately before each upload; the qualified support ceiling is 10 MiB for JPEG/PNG/WebP photos and 50 MiB for MP4 video.
- Telegram albums are 2–10 images, carry one canonical multi-message receipt and deliberately reject URL buttons/atomic edit.
- Added provider-neutral `social.post.buttons`, `SocialButton`, `SocialEditor` and `SocialDeleter` without changing frozen Connector/Runtime roots.
- Explicit HTTP 429 `retry_after` is retryable; ambiguous write transport/HTTP-5xx is non-retryable `write_outcome_unknown` because Telegram send methods provide no caller idempotency token.
- Added deterministic fixtures/tests, capability audit, reconciliation/conformance evidence and `ARCH-041`.

Next canonical dependency-ready task: `042 MAX Connector`.
