# Task 020 — Social Core

Status: **repository-complete** (2026-08-11)

Implement the provider-neutral `Content -> ContentVariant -> Publication -> ChannelAccount` model with canonical scheduling, exact connector-capability validation, Task-088 media references and immutable publication status evidence.

## Acceptance

- [x] Editable tenant-scoped `Content` master with guarded draft/ready/archive lifecycle.
- [x] Immutable `ContentVariant` snapshots for text/image/gallery/video/article with strict shape and duplicate-media validation.
- [x] Media stores only released Task-088 `UploadID` references; provider URL/object-key/credential fields are forbidden and consumer revalidation is documented.
- [x] `ChannelAccount` binds a social connector account to a canonical exact capability snapshot and cannot activate over an inactive/non-social connector account.
- [x] TORGNEXA owns canonical immediate/UTC scheduled publication semantics and a deterministic optimistic publication state machine with attempts, cancellation, retry and safe failure codes.
- [x] Additive Connector SDK v1 social publish/status/media interfaces preserve the frozen root SDK and validate exact capabilities before remote side effects.
- [x] PostgreSQL migration uses explicit tenant predicates/composite tenant keys/forced RLS, immutable history/snapshots and no hard delete.
- [x] Every mutation uses atomic Task-003 Audit + Task-008 Outbox; publication changes append immutable status history.
- [x] Four additive social v1 event payloads are registered with valid/invalid contract fixtures.
- [x] Architecture gap evidence and ADR `0049` cover frozen content/connector/event pillars.
- [x] Deterministic domain, SDK, repository-policy, migration and capability-vocabulary tests pass under the available local compatibility toolchain.

## Dependency boundary

Task `020` does not implement a provider, n8n surface, provider-owned scheduler, live-post approximation, edit/delete/comments/analytics workflow or REST API. The canonical next dependency-ready task is `019 n8n Node`; Tasks `040/041/042` later implement VK/Telegram/MAX against this Core and the Connector SDK boundary.
