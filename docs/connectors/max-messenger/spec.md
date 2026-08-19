# MAX Connector Spec

## Provider

- ID: `max-messenger`
- family: `social`
- connector version: `1.0.0`
- Connector SDK: v1
- official API audit date: 2026-08-11
- API authority: `platform-api2.max.ru`
- account configuration: one exact numeric channel `ChatID` plus a separate webhook-secret reference
- authentication: bot token behind Task-021 `SecretAccessor`, sent by host transport in the Authorization header

Admitted capabilities:

- `social.post.text`
- `social.post.media` — released image or image gallery
- `social.post.video` — one released video
- `social.post.buttons` — HTTPS link buttons only
- `social.webhooks` — verified/deduplicated `message_created`, `message_edited`, `message_removed`

Not admitted: provider scheduling, Long Polling in production, edit/delete, comments, analytics, callback buttons, arbitrary files/audio, user messaging, or provider-native workflow state.

## Account and channel isolation

One connector account binds one numeric `ChatID`. Health proves the token with `GET /me`, reads the exact configured channel with `GET /chats/{chatId}`, requires `type=channel` and `status=active`, then verifies the same bot is an administrator/owner with the current `write` permission (accepting the documented legacy response name only for compatibility).

A caller cannot override ChatID. Publish receipts and status reads are matched to the configured channel before becoming normalized results. Remote publication identity is `max:<chat_id>:<mid>`.

## Publishing

Only a Task-020 READY Publication is dispatched by the host. Text is bounded to 4000 Unicode code points. MAX attachments are constructed from Task-088 released media plus an optional inline keyboard; the connector conservatively caps the combined attachment list at 12.

For media, `MediaAccessor.OpenReleased` is called immediately before each upload. The qualified baseline accepts official image formats up to 50 MiB and MP4/MOV/MKV/WebM video up to 250 MiB. The connector obtains an upload URL from `POST /uploads`, then permits upload egress only to the official type-specific HTTPS hosts used in this baseline (`iu.oneme.ru` for image and `vu.okcdn.ru` for video). Userinfo, non-443 ports, encoded authorities, fragments and host-suffix tricks are rejected.

The channel send uses `POST /messages?chat_id=...` with `notify=true`. Link buttons are HTTPS-only and are laid out at most three per row.

## Status and retry semantics

`SocialPublicationStatusReader` uses `GET /messages/{mid}` and verifies the returned recipient chat before reporting `published`. Provider 404 becomes bounded `remote_missing` status evidence.

The qualified send surface exposes no caller-supplied idempotency key. Therefore ambiguous write transport failures and write-side HTTP 5xx are normalized to non-retryable `write_outcome_unknown`; automatic replay is intentionally denied because the remote post may already exist. Explicit 429 remains retryable through normalized bounded Retry-After guidance when host transport supplies it.

## Webhook lifecycle

Production updates use `POST /subscriptions`. The connector subscribes only to `message_created`, `message_edited` and `message_removed`, always supplies a separate verification secret, and permits only HTTPS endpoints with implicit port 443.

`ReceiveSocialWebhook`:

1. validates the canonical SDK request and exact `social.webhooks` capability;
2. resolves the configured verification secret and compares it in constant time with the host-extracted `X-Max-Bot-Api-Secret` value;
3. canonicalizes valid JSON and validates event type, configured ChatID, MID and timestamp;
4. derives a content-addressed delivery ID `sha256:<canonical-json-sha256>`;
5. calls the host-owned `SocialWebhookDeduplicator` before returning a normalized event.

The provider contains no durable dedup store. Production wiring must back `SocialWebhookDeduplicator` with the tenant-scoped Task-009 Inbox/idempotency boundary. Invalid/wrong-channel events are rejected before a dedup claim, preventing poisoned claims.
