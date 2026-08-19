# YouTube Capability Audit — Task 048

Audit date: **2026-08-12**.

## Official contract reviewed

Current Google documentation qualifies the following public surfaces:

- `videos.insert`: `POST https://www.googleapis.com/upload/youtube/v3/videos`, OAuth scopes including `youtube.upload`, media upload up to 256 GB, and a dedicated Video Uploads quota bucket;
- resumable upload protocol: session initiation followed by `PUT` uploads, `308 Resume Incomplete`, `Range`-based status probes and chunk sizes that are multiples of 256 KiB except the final chunk;
- `videos.list`: owner-visible `processingDetails`, upload status and failure/rejection reasons;
- `channels.list?mine=true`: exact authenticated channel resolution;
- `commentThreads.list`: bounded comment-thread reads by `videoId`, up to 100 items per page;
- `commentThreads.insert` and `comments.insert`: top-level and reply writes, respectively;
- OAuth 2.0 web-server flow with refresh tokens for offline access.

Official references:

- https://developers.google.com/youtube/v3/docs/videos/insert
- https://developers.google.com/youtube/v3/guides/using_resumable_upload_protocol
- https://developers.google.com/youtube/v3/docs/videos/list
- https://developers.google.com/youtube/v3/docs/channels/list
- https://developers.google.com/youtube/v3/docs/commentThreads/list
- https://developers.google.com/youtube/v3/docs/commentThreads/insert
- https://developers.google.com/youtube/v3/docs/comments/insert
- https://developers.google.com/youtube/v3/guides/auth/server-side-web-apps

## Admission decision

Task 048 registers provider `youtube` with:

- `social.post.video`;
- `social.comments.read`.

The production OAuth grant must contain the scopes required by the operations enabled for that account. Credentials remain Task-021 callback-scoped and no bearer token or resumable Location URI crosses the connector boundary.

## Why the local upload ceiling is 10 GiB

YouTube currently documents a **256 GB** upload limit. The frozen Connector SDK v1 `MediaDescriptor` intentionally rejects objects larger than **10 GiB**, so Task 048 cannot represent a larger released object without a Core/SDK change. The connector therefore applies 10 GiB as a local TORGNEXA limit and does not change the frozen SDK solely for YouTube.

## Scheduling

YouTube supports `status.publishAt` for a never-published private video. TORGNEXA Task-020 already owns the canonical schedule and invokes `SocialPublisher` only when a publication becomes READY. The current SDK request has no provider-native scheduling field, so Task 048 does not silently duplicate the scheduler or invent a second source of truth.

## Comment writes

The public API permits top-level comments and replies, but those methods provide no provider-side idempotency key. `SocialCommentReplyRequest` carries an idempotency key, yet the frozen connector call does not receive a durable host deduplication store. Retrying an ambiguous YouTube comment write could therefore duplicate a comment. Task 048 keeps comment writes undeclared until an idempotent host composition is available.

## Analytics

YouTube Data/Analytics APIs expose view, engagement, subscriber and watch-time metrics. Task-020's current common analytics projection is primarily reach-based and has no reporting window. Task 048 does not map `viewCount` to `ReachTotal` or invent a lifetime reporting period. A future additive analytics contract can admit YouTube metrics without semantic corruption.

## Project audit caveat

Google documents that uploads from unverified API projects created after 28 July 2020 are restricted to private viewing until the project completes the YouTube API compliance audit. Production qualification must therefore verify the API project's audit/verification state in addition to OAuth scopes and channel ownership.
