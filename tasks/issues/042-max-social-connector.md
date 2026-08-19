# Task 042: MAX social connector

## Objective
Implement MAX connector from current official API for supported channel publishing/webhook capabilities.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Webhook verification/deduplication and channel capability tests.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

- Registered provider `max-messenger` against Connector SDK v1 and canonical Task-020 Social Core using the current `platform-api2.max.ru` authority.
- Admitted text, released image/gallery, one released video, HTTPS URL buttons and verified/deduplicated production Webhook updates; destructive edit/delete, comments, analytics, callback actions and Long Polling production use remain fail-closed undeclared.
- Bot/channel authorization is exact: `GET /me -> GET /chats/{chatId} -> GET /chats/{chatId}/members/me`, requiring an active channel and bot administrator/owner with current `write` permission. Bot token and webhook secret remain behind Task-021 SecretAccessor.
- Media is re-opened through Task-088 immediately before upload; dynamic upload egress is restricted to official type-specific hosts for the qualified image/video baseline.
- Message send has no admitted caller idempotency token, so ambiguous write transport/HTTP-5xx is non-retryable `write_outcome_unknown` rather than risking a duplicate post.
- Added provider-neutral `social.webhooks`, `SocialWebhookReceiver` and host-owned `SocialWebhookDeduplicator`; MAX secret verification, exact-channel/type/timestamp validation and canonical-JSON SHA-256 identity occur before Task-009-backed durable dedup.
- Added deterministic fixtures/tests, capability audit, reconciliation/conformance evidence and `ARCH-042`.

Next canonical dependency-ready task: `043 Instagram Connector`.
