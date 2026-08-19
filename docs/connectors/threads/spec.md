# Threads Connector Spec

## Provider

- ID: `threads`
- family: `social`
- version: `1.0.0`
- Connector SDK: v1
- official API audit date: 2026-08-11
- API authority: `graph.threads.net`
- API version: `v1.0`
- required permissions: `threads_basic`; `threads_content_publish` for publish operations
- account binding: one exact numeric Threads user ID
- authentication: Threads user OAuth token behind Task-021 `SecretAccessor`
- token exchange secret: separate app-secret reference behind Task-021

Official documentation reviewed:
- https://developers.facebook.com/documentation/threads/overview
- https://developers.facebook.com/documentation/threads/create-posts
- https://developers.facebook.com/documentation/threads/posts
- https://developers.facebook.com/documentation/threads/reference/publishing
- https://developers.facebook.com/documentation/threads/get-started/get-access-tokens-and-permissions
- https://developers.facebook.com/documentation/threads/get-started/long-lived-tokens

## Admitted capabilities

- `social.post.text`: max 500 Unicode code points
- `social.post.media`: JPEG/PNG image or image carousel, max 20 items
- `social.post.video`: one MP4 video
- `SocialPublicationStatusReader`: published media existence/read-back

The current official Threads surface also supports mixed media carousel and other post forms. Task 044 deliberately stays inside the existing provider-neutral Social SDK shape and does not add Core concepts.

## Media baseline

- images: JPEG or PNG, max 8 MiB;
- video: MP4, max 1 GiB;
- media text: max 500 Unicode code points;
- carousel: max 20 images in this adapter.

The official API fetches public `image_url`/`video_url`, so the same Task-088 + host `MediaStager` boundary used by Instagram is applied. Internal object keys are not provider parameters.

## Publishing

1. create a Threads container with `POST /{threads-user-id}/threads`;
2. for image carousel, create child containers and a parent `CAROUSEL` container;
3. poll container `status` until `FINISHED`;
4. publish with `POST /{threads-user-id}/threads_publish`;
5. return `threads:<threads_user_id>:<media_id>`.

Ambiguous write transport/5xx is non-retryable `write_outcome_unknown`.

## Token lifecycle

`ExchangeLongLivedToken` calls the official `/access_token` endpoint with `grant_type=th_exchange_token`, current Threads user token, and the separately referenced app secret. `RefreshLongLivedToken` calls `/refresh_access_token` with `grant_type=th_refresh_token` and the current long-lived token.

Both operations validate the returned token and `expires_in`, then immediately hand plaintext to a host-owned `TokenSink.RotateSecret` for the existing account secret reference. Provider results contain only `RotatedAt`/`ExpiresAt`; replacement tokens are zeroed after the sink callback and are never returned/logged by the connector.
