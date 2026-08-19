# OK Capability Audit — Task 045

Audit date: **2026-08-12**. Only official `apiok.ru` documentation was used for provider admission.

## Qualified official surfaces

| Surface | Official evidence | Task-045 decision |
|---|---|---|
| Group media-topic publication | `mediatopic.post`: `GROUP_THEME`, `gid`, JSON attachment; group posting requires `GROUP_CONTENT`; attachment supports text/photo/movie | Admit text/media/video group publication |
| Photo upload | `photosV2.getUploadUrl`; external applications require a session/OAuth and `PHOTO_CONTENT`; upload response returns photo tokens; media-topic flow skips commit | Admit released JPEG/PNG upload before publication |
| Video upload | `video.getUploadUrl`; `VIDEO_CONTENT` + `VALUABLE_ACCESS`; 16 KiB..1 GiB; `video.update` completes upload and can publish | Admit one released MP4 before publication |
| Topic statistics | `group.getStatTopic`; `GROUP_CONTENT` + `VALUABLE_ACCESS`; includes reach/link-click/complaint/hide fields | Admit bounded analytics read |
| Request signing | official REST method pages specify `MD5(access_token + application_secret_key)`, sorted parameter concatenation and final MD5 signature | Implement locally with callback-scoped secrets |

Official references:

- https://apiok.ru/dev/methods/rest/mediatopic/mediatopic.post
- https://apiok.ru/dev/methods/rest/photosV2/photosV2.getUploadUrl
- https://apiok.ru/dev/examples/photo_upload
- https://apiok.ru/dev/methods/rest/video/video.getUploadUrl
- https://apiok.ru/dev/methods/rest/video/video.update
- https://apiok.ru/dev/methods/rest/group/group.getStatTopic

## Explicitly not admitted

Task 045 does not claim user-wall/note publication, post edit/delete, comments, polls/music, provider-native scheduling, webhooks, ad management or group administration. Unsupported operations remain absent from the manifest rather than being simulated through undocumented surfaces.

The official API does not publish one universal request-frequency number for all methods; the provider therefore uses a conservative host-side rate policy and honors normalized remote throttling rather than claiming a platform-wide quota.
