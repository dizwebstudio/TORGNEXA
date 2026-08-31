# ADR-0127 — MAX media publication worker route

Status: Accepted

## Context

The MAX adapter already implemented the documented three-step media flow, but
the built-in host transport intentionally rejected upload URLs and the
application support contract exposed only text. This made the runtime truthfully
fail-closed, but left a qualified adapter operation unused.

## Decision

Compose MAX image, image-gallery and supported-video publication in the existing
Social worker. Core media variants are mapped to the provider-neutral
`SocialPublishRequest`, and the upload request carries the bot token only for
the duration of the callback-scoped transport call. The host admits
`POST /uploads?type=image|video`, validates the returned type-specific HTTPS
upload host, builds a bounded multipart `data` part and sends it with the same
bot token before the final `POST /messages` call.

The allowlist is exact: `iu.oneme.ru` for images and `omub.okcdn.ru` for video;
only HTTPS and an empty or 443 port are accepted. The transport body is capped
at 256 MiB, while the adapter enforces the provider's 50 MiB image and 250 MiB
video ceilings. No arbitrary files or audio are enabled.

Buttons, webhooks, status reads and destructive remote mutations remain outside
the application runtime subset. Existing receipt and ambiguous-write recovery
semantics continue to apply because MAX does not provide a caller idempotency
identity for the complete upload/send sequence.

## Consequences

The runtime support contract truthfully exposes MAX text, media and video
publication. The upload security boundary is reused by the worker and an
unreleased or unavailable file fails before provider egress. No provider
specific attachment fields enter Social Core.

## Security and privacy impact

Bot tokens are never persisted in upload metadata or events and remain scoped to
the transport callback. The provider upload URL is treated as untrusted response
data and is constrained to exact official hosts before DNS/egress. File names,
media types, sizes and multipart bodies are bounded; object keys remain hidden
behind the released-upload accessor.

## Compatibility impact

The existing Social SDK and worker dispatch are reused. The additive
runtime-support change enables two existing manifest capabilities; no API,
event or database migration is required.

## Operational impact

Media uploads can be substantially larger than JSON calls, so the transport has
a separate 256 MiB request ceiling and reuses the provider's existing timeout,
rate-limit and retry policy. Live qualification still requires a non-production
MAX bot and dedicated channel.
