# Telegram Connector Spec

## Provider

- ID: `telegram`
- family: `social`
- connector version: `1.0.0`
- Connector SDK: v1
- Telegram Bot API baseline: `10.3` (2026-08-24)
- API authority: `api.telegram.org`
- account configuration: one immutable numeric negative channel `ChatID`
- authentication: bot token behind Task-021 `SecretAccessor`

Admitted capabilities:

- `social.post.text`
- `social.post.media` — one photo or 2–10 photo album
- `social.post.video` — one MP4 video
- `social.post.buttons` — HTTPS URL buttons only
- `social.post.edit` — one remote message only
- `social.post.delete` — one message or the 2–10 message set emitted for one album

Production composition in Task 174 activates `social.post.text`,
`social.post.media` and `social.post.video` through the worker's released-upload
bridge. Task 181 additionally activates HTTPS URL buttons for text, single
photo and single video publications. Edit/delete and webhooks remain qualified
SDK ceilings, not claims about currently reachable application workflows.

Not admitted: provider scheduling, inbound updates/callback queries, comments, analytics, arbitrary files, live/video processing status reads or atomic album edit.

## Authentication and channel isolation

A connector account binds one negative numeric `ChatID`. Usernames are not accepted as configuration because rename/reassignment would weaken channel identity. Health resolves the bot with `getMe`, then calls `getChatMember` for the exact configured channel and requires administrator status plus `can_post_messages`.

The bot token is available only inside a SecretAccessor callback. The provider request model keeps `BotToken` separate from method parameters so host transport can construct the provider-specific authorization path without placing the token in normalized params, logs or errors.

## Publish flow

The host dispatches only an already-authorized canonical Task-020 READY Publication.

- text -> `sendMessage`;
- one image -> `sendPhoto`;
- 2–10 images -> `sendMediaGroup`;
- one video -> `sendVideo`.

Task-088 `MediaAccessor.OpenReleased` is called immediately before every upload read. The connector accepts JPEG/PNG/WebP images up to 10 MiB and MP4 video up to 50 MiB in this qualified baseline.

Remote identity is `tg:<chat_id>:<message_id>[,<message_id>...]`. Every later edit/delete parses that exact canonical form and rejects a different channel before egress.

## Buttons

Task 041 adds provider-neutral `SocialButton` and capability `social.post.buttons`. Only HTTPS URL buttons are admitted. Up to eight buttons are accepted and laid out two per row. Callback-data buttons are intentionally excluded because they require a separate inbound update/webhook lifecycle and authorization contract.

Albums do not accept buttons because `sendMediaGroup` does not expose the same per-publication reply-markup surface as single-message sends.

## Edit and delete

Task 041 adds additive SDK-v1 `SocialEditor` and `SocialDeleter` interfaces without modifying frozen `Connector` or `Runtime` roots.

- text edit -> `editMessageText`;
- single image/video replacement -> `editMessageMedia` after Task-088 revalidation;
- album edit -> fail closed as `album_edit_unsupported`;
- one message delete -> `deleteMessage`;
- album receipt delete -> `deleteMessages`.

Telegram deletion remains subject to provider restrictions such as its deletion time window and channel admin permissions. The connector never treats a provider refusal as deletion of canonical Task-020 evidence.

The architecture review for Task 041 admits these remote mutations only as host-dispatched operations against a tenant-bound connector account. Task 020 remains the canonical authorization/audit/outbox boundary; the provider is not a public mutation API.

## Retry and rate limits

Telegram has no provider idempotency field equivalent to VK `wall.post.guid`. Therefore:

- an explicit Telegram flood-control response is normalized to `rate_limited` and preserves bounded `retry_after` guidance;
- read-side transport/5xx errors may be retryable under the host policy;
- any transport failure after a write was attempted, and write-side HTTP 5xx, become non-retryable `write_outcome_unknown`.

This deliberately prefers manual/reconciliation recovery over an automatic duplicate post after an ambiguous write result.

The manifest uses a conservative single concurrent request, 40 ms minimum interval, 30 s timeout and bounded five-attempt host retry policy. Provider `retry_after` remains authoritative when present.

## Reconciliation boundary

The successful Telegram response is the publication receipt. Bot API does not expose a general `getMessage` status operation, so this connector does not implement `SocialPublicationStatusReader` or invent remote status polling. Task 014 owns later reconciliation policy using stored remote receipts/evidence and explicit operator actions where needed.

Task 132 stores that receipt outside canonical Social Core in
`social_publication_receipts`. A reclaimed `publishing` lease with a receipt is
finalized without another provider call; a reclaimed lease without a receipt is
failed as `write_outcome_unknown`. Automatic duplicate-prone resend is forbidden.
