# Instagram Connector Spec

## Provider

- ID: `instagram`
- family: `social`
- version: `1.0.0`
- Connector SDK: v1
- official API audit date: 2026-08-11
- API authority: `graph.instagram.com`
- API version: `v26.0`
- login: Instagram API with Instagram Login / Business Login
- required account class: Instagram Professional account, Business or Creator
- required permissions for the admitted publish surface: `instagram_business_basic`, `instagram_business_content_publish`
- account binding: one exact numeric Instagram user ID
- authentication: OAuth2 access token behind Task-021 `SecretAccessor`

Official documentation reviewed:
- https://developers.facebook.com/documentation/instagram-platform/instagram-api-with-instagram-login
- https://developers.facebook.com/documentation/instagram-platform/content-publishing
- https://developers.facebook.com/documentation/instagram-platform/instagram-graph-api/reference/ig-user/media
- https://developers.facebook.com/documentation/instagram-platform/instagram-graph-api/reference/ig-user/media_publish
- https://developers.facebook.com/documentation/instagram-platform/instagram-graph-api/reference/ig-container

## Admitted capabilities

- `social.post.media`: one JPEG image or 2-10 JPEG image carousel
- `social.post.video`: one MP4 Reel
- `SocialPublicationStatusReader`: read-after-publish existence check for a published media ID

Not admitted: `social.post.text`, buttons, Stories, mixed image/video carousel, provider scheduling, comments/replies, analytics, edit/delete.

## Account health

Health queries the exact configured user at `/{IG_ID}?fields=id,username,account_type`, requires exact ID equality and Business/Media_Creator account type. Caller input cannot override the configured IG user ID.

## Media safety and staging

`MediaAccessor.OpenReleased` is called immediately before staging every item. The connector accepts a conservative current official baseline:

- feed image: JPEG only, max 8 MiB;
- Reel: MP4 only, max 300 MiB;
- caption: max 2200 Unicode code points;
- image carousel: max 10 items.

Meta fetches public media URLs. The provider therefore requires a host-supplied `MediaStager` which receives only a revalidated Task-088 released stream and returns a short-lived HTTPS URL. URLs must have a normal hostname, HTTPS/443, no userinfo/backslash/fragment, and an expiry between +2 minutes and +24 hours. Signed query strings are permitted and are never copied into normalized errors/results.

## Publishing flow

1. Revalidate/stage released media.
2. `POST /{IG_ID}/media` for image/Reel or carousel children.
3. Poll the container `status_code` until `FINISHED` using a bounded provider-local wait.
4. For carousels, create parent `CAROUSEL` container from child IDs and wait for it.
5. `POST /{IG_ID}/media_publish` with the creation ID.
6. Return bounded remote identity `instagram:<ig_user_id>:<media_id>`.

`ERROR`/`EXPIRED` container states fail closed as `media_rejected`. Ambiguous POST transport/5xx outcomes are normalized to non-retryable `write_outcome_unknown` instead of automatically replaying a possibly successful write.
