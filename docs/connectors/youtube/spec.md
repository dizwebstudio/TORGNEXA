# YouTube Connector Spec

Audit date: **2026-08-12**.

## Identity and OAuth boundary

One TORGNEXA connector account binds exactly one configured YouTube `ChannelID`. Health resolves the authenticated user's owned channel and becomes healthy only when its ID is exactly the configured ID.

The account secret reference contains the current OAuth credential material. It is exposed only inside Task-021 `SecretAccessor.UseSecret`; token bytes are not retained on the Connector and never appear in normalized errors.

## Publication mapping

The manifest grants `social.post.video` only for writes. Task-020 remains canonical owner of Content, immutable ContentVariant, Schedule and Publication state.

A READY `SocialPublishRequest` must contain exactly one Task-088 released video. The adapter accepts `video/*` MIME types supported by the host/media pipeline and enforces the frozen SDK-v1 maximum of 10 GiB.

Metadata mapping is deterministic:

- first non-empty content line -> title, at most 100 Unicode code points;
- full bounded content text -> description, at most 5000 UTF-8 bytes;
- `<` and `>` are rejected because YouTube excludes them from title/description;
- configured `CategoryID`, privacy, subscriber-notification, made-for-kids and synthetic-media defaults are sent explicitly by the typed transport;
- canonical Publication ID is propagated as `ExternalID` for transport audit/reconciliation metadata but is not exposed as a public YouTube field.

## Resumable upload state machine

Production transport binds these typed operations to the documented Data API v3 contract:

`READY -> StartResumableUpload -> UploadChunk* -> processing|published`

The host transport owns the resumable `Location` URI and returns only an opaque local `SessionID` to provider code.

Chunks are 8 MiB by default and therefore multiples of YouTube's required 256 KiB quantum. The final chunk may be shorter. For every accepted chunk, the transport returns the next confirmed byte offset.

If chunk delivery has an unavailable/ambiguous result, the connector does **not** assume that the chunk failed. It calls `ProbeResumableUpload`, validates the confirmed offset and resumes only from that exact position. Misaligned, skipped, overlapping or out-of-range offsets fail closed. An expired session becomes `upload_session_expired`; an unresolvable result becomes `upload_outcome_unknown`.

An ambiguous session-start result becomes `upload_session_outcome_unknown` and is not blindly retried.

## Status reconciliation

Remote publication IDs have the form:

`youtube:<channel-id>:<video-id>`

Status reads reject a foreign channel before calling the transport. `videos.list`-derived state maps as:

- upload `uploaded` + processing `processing` -> `processing`;
- upload `uploaded` + processing `succeeded` -> `published`;
- upload `processed` -> `published`;
- upload `failed`, `rejected`, `deleted`, processing `failed` or `terminated` -> `failed` with a bounded normalized reason.

Provider failure/rejection reasons are allowlisted before crossing the SDK boundary.

## Comments

`social.comments.read` uses top-level `commentThreads.list` projection only. Cursor is the opaque YouTube `nextPageToken`; the connector caps each page at 100 and rejects duplicate/malformed comment IDs, invalid timestamps and foreign publication IDs.

Replies embedded below a thread are not flattened into the top-level page in Task 048. Comment writes remain undeclared for idempotency reasons documented in `capability-audit.md`.

## Quota and errors

Upload-limit and quota failures normalize to bounded `rate_limited` category errors (`upload_limit_exceeded` / `quota_exceeded`). Retry guidance is preserved only when bounded. Auth, permission, not-found, comments-disabled, invalid metadata, expired-session and unavailable outcomes are normalized without returning raw Google response bodies.
