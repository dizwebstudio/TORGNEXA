# Telegram Capability Audit

Audited 2026-08-27 against the official Telegram Bot API documentation, current Bot API 10.3 baseline released 2026-08-24.

Primary evidence:

- `https://core.telegram.org/bots/api`
- `sendMessage`, `sendPhoto`, `sendVideo`, `sendMediaGroup`
- `InlineKeyboardMarkup`
- `editMessageText`, `editMessageMedia`
- `deleteMessage`, `deleteMessages`
- `ResponseParameters.retry_after`

| Capability | Decision | Admission boundary |
|---|---|---|
| `social.post.text` | **granted** | `sendMessage`, exact configured channel. |
| `social.post.media` | **granted** | one photo via `sendPhoto`, 2–10 photo album via `sendMediaGroup`, Task-088 media revalidation. |
| `social.post.video` | **granted** | one MP4 video via `sendVideo`, Task-088 media revalidation. |
| `social.post.buttons` | **granted: URL-only** | HTTPS URL buttons only; callback-data buttons deferred with inbound-update lifecycle. |
| `social.post.edit` | **granted: single-message only** | text via `editMessageText`; one photo/video replacement via `editMessageMedia`; album edit denied. |
| `social.post.delete` | **granted: bounded receipt set** | `deleteMessage` / `deleteMessages`, max 10 IDs because the only multi-message remote receipt produced by this connector is a Telegram album. Provider time/permission restrictions remain authoritative. |
| comments/analytics | **not declared** | No Task-041 qualification. |
| inbound callbacks | **not declared** | Requires a separate webhook/update security contract. |

## Production runtime subset

Task 174 composes `social.post.text`, `social.post.media` and
`social.post.video` through the released-upload host bridge. Task 181 composes
`social.post.buttons` through the canonical variant, API and worker route;
buttons are HTTPS-only and are admitted for text, one photo and one video.
Task 192 composes `social.post.edit` for one already published message through
an approval-bound, receipt-backed API operation. Deletion still needs its own
application authorization and reconciliation flow; webhooks need a separate
inbound security contract.

## Retry decision

Telegram exposes `retry_after` for flood-control responses, but the send methods have no caller-supplied idempotency identity. The connector therefore retries explicit rate-limit/read failures only according to normalized provider evidence and returns non-retryable `write_outcome_unknown` for ambiguous write transport/5xx outcomes.

## Upload ceilings

The admitted Task-041 implementation is intentionally narrower than the broadest Bot API file surfaces: JPEG/PNG/WebP photo uploads are capped at 10 MiB and MP4 video at 50 MiB. Other media/file types remain undeclared.
