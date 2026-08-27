# Social Core

Task `020` establishes the canonical provider-neutral social publishing model used by n8n and all social-channel connectors.

## Domain model

`Content` is the editable master record. It moves `draft -> ready|archived`, may return `ready -> draft`, and becomes immutable when archived. Publishing never sends the mutable master directly.

`ContentVariant` is an immutable version-1 snapshot of one content item. Supported formats are:

| Format | Shape | Required connector capability |
|---|---|---|
| `text` | body, no media | `social.post.text` |
| `image` | exactly one image upload | `social.post.media` |
| `gallery` | 2–20 image uploads | `social.post.media` |
| `video` | exactly one video upload | `social.post.video` |
| `article` | title + body | `social.post.text` |

`live` is not represented. The frozen SDK v1 capability catalog has no exact live-publish capability, so Core rejects it rather than pretending another capability is equivalent.

`ChannelAccount` is a Social Core projection of one Task-010 `connector_account`. It contains only the connector-account ID, display name, canonical sorted capability snapshot and `disabled|active` state. It cannot become active unless the underlying connector account is active and has family `social`.

`Publication` binds one immutable variant to one channel account and one host-owned schedule. Immediate publications start `ready`; future publications start `scheduled` with a UTC timestamp.

## State machine and retry

The only allowed publication transitions are:

`scheduled -> ready | cancelled`

`ready -> publishing | cancelled`

`publishing -> published | failed`

`failed -> ready | cancelled`

Entering `publishing` increments `attempt` exactly once. Other transitions cannot change it. `published` and `cancelled` are terminal. A failure contains only a normalized lowercase `reason_code`; raw provider errors do not enter Core, audit or event history.

The scheduler is canonical TORGNEXA infrastructure. Providers do not own scheduling. A worker publishes only after the host has moved a record to `READY`, then claims it via the optimistic `READY -> PUBLISHING` transition. A competing worker with a stale version loses with `ErrConflict`.

## Media boundary

Variants store only Task-088 `UploadID` references plus media kind/alt text. Raw S3 keys, filesystem paths, public/signed URLs and credentials are forbidden.

Creation verifies the upload is currently `released`, but that is not a permanent trust grant: Task-088 may revoke release after re-scan. The connector host therefore **must revalidate current release immediately before every media read**. The additive SDK `MediaAccessor.OpenReleased` is the only intended byte-access route for social providers.

## Connector SDK v1

Task 020 adds capability-specific interfaces without changing the frozen root Connector/Runtime major:

- `SocialPublisher.PublishSocial` for publish-now execution;
- `SocialPublicationStatusReader.ReadSocialPublicationStatus` for normalized status polling;
- `MediaAccessor.OpenReleased` for host-mediated released media bytes;
- `SocialPublishRequest` with text/media/video shape validation;
- `ValidateSocialPublish` to require the exact manifest capability before remote side effects;
- Task-041 additive `SocialButton` / `social.post.buttons` plus `SocialEditor` and `SocialDeleter` operation surfaces for providers that explicitly admit those capabilities.

Remote post IDs remain provider/mapping adapter data and are not fields in Social Core. Task 020 intentionally did not invent provider behavior for edit/delete/comments/analytics. Task 040 adds only additive SDK-v1 comment read/reply and bounded publication-analytics projections plus the first VK adapter; edit/delete remain undeclared by VK. Task 041 adds provider-neutral HTTPS URL-button data plus additive edit/delete interfaces and admits them only for the Telegram adapter with channel-bound receipts. Task 042 adds the additive `social.webhooks` capability plus `SocialWebhookReceiver`/host-owned dedup interfaces and admits MAX production webhook reception without moving durable delivery state into the provider; canonical Content/Publication ownership, authorization, audit and scheduling remain unchanged.

## Persistence and tenancy

Migration `000027_social_core.sql` creates:

- `social_contents`;
- `social_content_variants`;
- `social_variant_media_refs`;
- `social_channel_accounts`;
- `social_publications`;
- `social_publication_status_events`.

Every table is organization/workspace scoped, uses composite tenant foreign keys and forced RLS. Repository transactions also set tenant scope and include explicit tenant predicates. Immutable snapshots/history cannot be updated/deleted/truncated; mutable records use optimistic versions and guarded transitions.

Every mutation commits Task-003 Audit and Task-008 Outbox in the same PostgreSQL transaction. Publication mutations additionally append immutable status evidence.

Task 132 adds `social_publication_receipts` as separate append-only operational
evidence. Remote IDs do not become canonical fields. The production worker
leases due/ready Telegram and MAX text publications, and receipt-aware crash recovery
prevents duplicate sends when a process stops between provider success and the
final canonical status transition.

## Events

Task 020 registers four additive v1 payloads:

- `commerce.social.content_changed.v1`;
- `commerce.social.variant_changed.v1`;
- `commerce.social.channel_account_changed.v1`;
- `commerce.social.publication_status_changed.v1`.

Payloads carry bounded identities/state/version metadata only. Content bodies, provider payloads, raw errors and credentials are intentionally absent.

## Dependency boundary

Task `020` supplies the model and provider-neutral execution interfaces. Task `019 n8n Node` can now expose these resources/actions without provider branches. Tasks `040 VK`, `041 Telegram`, and `042 MAX` are repository-complete provider adapters against this Core. Task 132 composes Telegram text publication end to end through the production API, worker and dedicated `/social` surface; Task 133 admits the same exact text-only path for MAX. Later social connectors implement the same capability/conformance boundary rather than adding provider-specific state to Core.
