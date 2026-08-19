# VK Connector Spec

## Provider

- ID: `vk`
- family: `social`
- connector version: `1.0.0`
- Connector SDK: v1
- VK API schema baseline: `5.199`
- API host: `api.vk.com`
- account configuration: positive VK community `GroupID`
- authentication: OAuth2 **user** access token behind Task 021 `SecretAccessor`

Admitted capabilities:

- `social.post.text`
- `social.post.media` (images/gallery only, connector support ceiling 10 images)
- `social.comments.read`
- `social.comments.reply`
- `social.analytics.read`

## Canonical publish flow

The host owns Content, immutable ContentVariant, Schedule and Publication state. The adapter accepts only `SocialPublishRequest` after the host has made the Publication READY.

Text publish:

1. validate `SocialPublishRequest` and manifest capability;
2. resolve account `GroupID` outside secret storage;
3. obtain the user OAuth token only inside a SecretAccessor callback;
4. call `wall.post` with `owner_id=-GroupID`, `from_group=1`;
5. pass canonical `PublicationID` as VK `guid`;
6. map `post_id` to adapter remote ID `-<group_id>_<post_id>`;
7. return `published` with the connector observation time.

Image/gallery publish additionally:

1. call Task-088 `MediaAccessor.OpenReleased` for every image immediately before use;
2. accept JPEG/PNG/WebP and a connector support ceiling of 50 MiB per image;
3. call `photos.getWallUploadServer` for the configured group;
4. reject dynamic upload URLs unless they are HTTPS, have no userinfo/non-443 port/fragment, and resolve syntactically under `vk.com` or `userapi.com`;
5. upload through the host-injected transport, never through a provider-created HTTP client;
6. call `photos.saveWallPhoto`;
7. attach only the returned `photo<owner>_<id>` value to `wall.post`.

No object-storage key, public URL or secret credential enters Social Core.

## Retry and status semantics

`wall.post.guid = PublicationID`, so a retry reuses the same provider idempotency identity. The host still owns retry timing using the manifest and normalized `RemoteError`.

`ReadSocialPublicationStatus` validates that the adapter remote ID belongs to the configured community, reads it with `wall.getById`, and maps:

- present exact wall post -> `published`;
- absent exact wall post -> `failed / remote_missing`;
- transport/auth/rate/provider failures -> normalized error, not a fabricated terminal status.

VK publication in this baseline has no synthetic `processing` state.

## Comments

`wall.getComments` is exposed through the additive SDK-v1 `SocialCommentReader`. Pagination uses a versioned Base64URL cursor bound to the exact community and post. The host sees a provider-neutral comment projection only.

`wall.createComment` is exposed through `SocialCommentReplier`. A canonical UUIDv7/ULID idempotency key is passed as VK `guid`. Replies are authored from the configured community (`from_group=GroupID`).

## Analytics

`stats.getPostReach` is exposed through `SocialAnalyticsReader`, bounded to 30 publication IDs per call. The adapter rejects foreign-community remote IDs before remote access and projects only:

- total/follower reach;
- link clicks;
- community visits/joins;
- reports;
- hides;
- unsubscribes.

Provider-only demographic payloads are not copied into the SDK projection.

## Error boundary

HTTP and VK API-envelope failures become bounded Connector SDK `RemoteError` values. Raw `error_msg`, response body, OAuth token, upload URL and transport error text are never copied into SDK errors or health reason codes.
