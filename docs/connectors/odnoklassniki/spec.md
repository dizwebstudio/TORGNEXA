# OK Connector Spec

Audit date: **2026-08-12**.

## Identity and credentials

One TORGNEXA connector account binds exactly one numeric OK `GroupID`. `group.getInfo` must resolve that exact group before health is `healthy`.

The account secret reference contains the OAuth access token. Provider configuration contains the OK application public key and a second Task-021 `SecretReference` for the application secret. Secret bytes remain callback-scoped.

REST requests are sent only to `api.ok.ru`. The transport supplies the access token separately and the connector calculates the official OAuth REST signature as `MD5(sorted key=value params + MD5(access_token + application_secret_key))`, lower-case hexadecimal. Raw token/app-secret bytes are never returned in provider results or normalized errors.

## Publication

`social.post.text`, `social.post.media`, and `social.post.video` map to `mediatopic.post` with:

- `type=GROUP_THEME`;
- exact configured `gid`;
- `onBehalfOfGroup=true`;
- one bounded JSON `attachment`.

Text becomes a `text` attachment. Images are obtained only through `MediaAccessor.OpenReleased`, then uploaded with `photosV2.getUploadUrl` and provider-issued multipart fields `pic1..picN`; the returned upload tokens are inserted directly into the media-topic photo attachment, without `photosV2.commit`. Video uses `video.getUploadUrl`, multipart field `data`, then `video.update(publish=true)` before the movie attachment is posted.

Provider-independent request validation remains owned by Social Core. This adapter additionally limits a media publication to 20 released JPEG/PNG images and applies a conservative 32 MiB per-image local ceiling. The 32 MiB value is an adapter safety ceiling, not an assertion of an OK platform limit. MP4 video is accepted only between 16 KiB and 1 GiB, matching the audited `video.getUploadUrl` range.

Dynamic upload URLs are HTTPS-only and accepted only for qualified OK upload authorities (`ok.ru`, `mycdn.me`, `odnoklassniki.ru` and subdomains), with userinfo, encoded authority, fragments, non-443 ports and suffix tricks rejected.

## Status and analytics

The canonical remote publication identity is `ok:<group-id>:<topic-id>`. Status read accepts only an identity bound to the configured group and verifies it through `mediatopic.getByIds`.

`social.analytics.read` uses `group.getStatTopic` for at most 50 exact group-bound topic IDs and projects:

- `reach` -> `ReachTotal`;
- `reach_own` -> `ReachFollowers`;
- `link_clicks` -> `LinkClicks`;
- `complaints` -> `Reports`;
- `hides_from_feed` -> `Hides`.

Negative or malformed counters fail closed.

## Failure semantics

Transport failures or remote 5xx/system errors during a write are normalized to non-retryable `write_outcome_unknown`; callers must reconcile rather than blindly repeat the publication. Read-side authentication, permission, rate-limit, not-found and availability failures are normalized to Connector SDK categories.
