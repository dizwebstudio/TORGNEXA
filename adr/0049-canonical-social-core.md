# ADR 0049: Canonical provider-neutral Social Core

Status: Accepted

## Context

TORGNEXA needs one content/publication model before n8n and provider-specific social connectors are added. Putting VK, Telegram, MAX or other remote post identifiers, scheduling semantics or upload URLs into Core would fork behavior by provider, bypass the frozen connector-capability boundary, and make retries/status history inconsistent.

## Decision

Use one provider-neutral social aggregate because provider-owned schedules, media locators and remote identifiers would make retry/capability semantics diverge and violate the frozen connector boundary.

Add `internal/core/social` with `Content`, immutable `ContentVariant`, `ChannelAccount` and `Publication`. Content is the editable master; variants are immutable publish snapshots using Task-088 `UploadID` references only; a channel account projects a tenant-scoped social connector account plus its canonical capability snapshot; a publication binds one variant, one channel and one TORGNEXA-owned schedule.

Required publish capability is exact: text/article use `social.post.text`, image/gallery use `social.post.media`, and video uses `social.post.video`. Unsupported formats fail closed; `live` remains absent until an exact additive capability exists.

Connector SDK v1 gains additive `SocialPublisher`, `SocialPublicationStatusReader` and `MediaAccessor` interfaces without changing frozen root interfaces. Provider calls are publish-now/status-read only after the host scheduler makes a publication ready. Media bytes are opened through the host from a currently released UploadID; durable URLs, object keys and storage credentials are forbidden.

The publication lifecycle is `scheduled -> ready -> publishing -> published|failed`, with `failed -> ready|cancelled` and cancellation from scheduled/ready/failed. Entering `publishing` increments attempt; published/cancelled are terminal. PostgreSQL is the system of record with explicit tenant predicates, forced RLS, optimistic versions, immutable status history, and atomic Task-003 audit + Task-008 outbox evidence. Remote provider post IDs remain outside Core.

## Consequences

Task 019 n8n and Tasks 040+ social providers can target one stable model and capability vocabulary. A provider cannot silently downgrade media/video to text, bypass canonical scheduling, read quarantined media, or place provider payloads in Core. The model intentionally does not implement edit/delete/comments/analytics workflows yet; those capabilities already exist in SDK vocabulary and require separate reviewed application behavior when used.

## Alternatives considered

Provider-specific social tables were rejected because they duplicate lifecycle and scheduler behavior. Treating every channel as a generic webhook was rejected because social publish/status capabilities and media semantics need typed validation. Letting providers schedule remotely was rejected because it would create multiple clocks/sources of truth and inconsistent retries. Storing signed media URLs in variants was rejected because Task-088 release is revocable and storage credentials/locators must not become durable domain state.

## Compatibility impact

The change is additive. Four new v1 social event payload schemas are registered. Connector SDK major v1 root interfaces are unchanged; additive social interfaces use existing capability names. Existing REST/OpenAPI operations are unchanged.

## Migration and data impact

Expand migration `000027_social_core.sql` creates tenant-scoped content, variant/media, channel-account, publication and status-history tables. Existing readers/writers remain compatible and no backfill is required.

## Security and privacy impact

Tenant isolation is fail-closed in application queries and PostgreSQL RLS. Social Core stores business-authored content but no provider credentials, raw provider payloads, signed URLs, object keys or remote errors. Media is referenced by released upload identity only. Audit/outbox payloads contain bounded IDs/state rather than content bodies.

## Operational impact

The TORGNEXA scheduler owns due-publication transition to `READY`; workers claim work through optimistic status/version transitions. Provider execution must revalidate channel capability and current media release before remote side effects. Provider status/error normalization must use bounded safe reason codes.
