# MAX Capability Audit

Audited 2026-08-11 against the current official MAX Bot API documentation.

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

## Retry decision

The message-send request does not expose a caller-provided idempotency identity in the audited API surface. That absence is treated as a hard safety limitation: ambiguous write transport/HTTP-5xx outcomes are not automatically retried. Explicit rate-limit refusals remain retryable only from normalized provider/transport evidence.

## Webhook decision

The subscription secret is mandatory in TORGNEXA even though the provider describes it as optional. The host must extract the exact `X-Max-Bot-Api-Secret` header into ephemeral `VerificationToken`; the connector never stores the raw header or secret in canonical events. Delivery dedup is content-addressed and tenant-scoped through Task-009.
