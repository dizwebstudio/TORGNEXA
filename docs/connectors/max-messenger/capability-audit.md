# MAX Capability Audit

Audited 2026-08-27 against the current official MAX Bot API documentation.

Primary official surfaces reviewed:

- API overview and authorization/rate-limit guidance;
- `POST /messages`;
- `POST /uploads`;
- `GET /messages/{messageId}`;
- channel and bot-membership methods;
- `POST /subscriptions` and `DELETE /subscriptions`;
- `Update` webhook objects.

| Capability | Decision | Admission boundary |
|---|---|---|
| `social.post.text` | **granted** | `POST /messages`, exact configured channel, max 4000 code points. |
| `social.post.media` | **granted** | Task-088 released official image formats, max 50 MiB each, official upload host allowlist. |
| `social.post.video` | **granted** | one Task-088 released MP4/MOV/MKV/WebM, max 250 MiB. |
| `social.post.buttons` | **granted: URL-only** | HTTPS link buttons only, max three per row in this adapter. |
| `social.webhooks` | **granted** | production Webhook, secret verification, exact channel/type validation, host-owned durable dedup. |
| status read | **implemented additive interface** | `GET /messages/{mid}`, exact recipient channel required. |
| edit/delete | **not declared** | API support exists but Task-042 does not qualify destructive remote mutation. |
| comments/analytics/callback actions | **not declared** | No Task-042 qualification. |
| Long Polling | **not admitted for production** | Official guidance identifies Webhook as the production mechanism. |

## Task-175 application-runtime subset

The manifest table above records the adapter's qualified SDK ceiling. The
current application runtime grants text, released image/video uploads and the
corresponding `POST /messages?chat_id=...` attachment path. Health may call
only `GET /me`, `GET /chats/{chatId}` and
`GET /chats/{chatId}/members/me`. Uploads are limited to the official image and
video hosts, and all media is revalidated by the Task-088 bridge. URL buttons,
webhooks and destructive remote mutations are not application operations. This
subset is generated from
`contracts/connectors/builtin-runtime-support-v1.json` and is what the UI/API
advertise. Official references: [message creation](https://dev.max.ru/docs-api/methods/POST/messages),
[bot identity](https://dev.max.ru/docs-api/methods/GET/me), and
[bot membership](https://dev.max.ru/docs-api/methods/GET/chats/-chatId-/members/me).

## Retry decision

The message-send request does not expose a caller-provided idempotency identity in the audited API surface. That absence is treated as a hard safety limitation: ambiguous write transport/HTTP-5xx outcomes are not automatically retried. Explicit rate-limit refusals remain retryable only from normalized provider/transport evidence.

## Webhook decision

The subscription secret is mandatory in TORGNEXA even though the provider describes it as optional. The host must extract the exact `X-Max-Bot-Api-Secret` header into ephemeral `VerificationToken`; the connector never stores the raw header or secret in canonical events. Delivery dedup is content-addressed and tenant-scoped through Task-009.
