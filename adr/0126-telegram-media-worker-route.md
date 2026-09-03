# ADR-0126 — Telegram media publication worker route

Status: Accepted

## Context

The Telegram adapter already implemented the official `sendPhoto`,
`sendMediaGroup` and `sendVideo` shapes, but the production worker dispatched
only text. Enabling those capabilities without a host-owned released-upload
bridge would bypass the quarantine and malware-release boundary.

## Decision

Compose Telegram photo, photo-album and MP4-video publication in the existing
Social worker. Core image, gallery and video variants are mapped to the
provider-neutral `SocialPublishRequest`; the worker requires the matching
account capability and passes a `MediaAccessor` that resolves the tenant from
the connector account, revalidates the released upload, sniffs the bounded
content type and opens only the server-derived released object.

The Telegram host transport sends URL-encoded text requests and bounded
multipart requests for media. Multipart field names, filenames, media types,
declared sizes and total body size are validated before egress. The provider
adapter remains responsible for the exact Telegram method and remote receipt.

URL buttons, edit/delete, inbound webhooks and unsupported file types remain
unadmitted. The worker continues to use the existing receipt and ambiguous
write recovery boundary; media does not introduce automatic duplicate-prone
resend.

## Consequences

The runtime support contract now truthfully exposes Telegram text, media and
video publication. Upload security is a prerequisite for media publication;
when uploads are disabled or an object is not released, the publication fails
with a controlled media-unavailable outcome. No provider-specific fields enter
Social Core.

## Security and privacy impact

Bot tokens remain callback-scoped. Object keys are derived by the upload gate,
tenant scope is taken from the host-owned account, content is reopened only
after release validation, and upload bytes are bounded before Telegram egress.
Remote payloads and media contents are not written to audit or events.

## Compatibility impact

The existing Social SDK, Core publication model and upload contract are reused.
The additive runtime-support change enables two existing manifest capabilities;
no API, event or database migration is required.

## Operational impact

The current rate limit and timeout apply to multipart requests, with a separate
128 MiB body ceiling above the adapter's 10 MiB photo and 50 MiB video limits.
Live qualification still requires a non-production bot and dedicated channel.

## Alternatives considered

Keeping media SDK-only was rejected because the released-upload worker bridge
already provides the required quarantine and bounded egress controls.

## Migration and data impact

No database migration is required; existing upload-release, publication and
receipt records continue to store only their provider-neutral projections.
