# VK Capability Audit

The adapter capability audit and the application runtime admission are
separate decisions. Task 135 admits only `social.post.text`; the other
SDK-ready rows below remain disabled in the built-in runtime until the complete
host and frontend workflow is implemented.

Audited 2026-08-11 against the official `VKCOM/vk-api-schema` repository. Its current method schemas identify API version `5.199`.

Primary evidence:

- `https://github.com/VKCOM/vk-api-schema`
- `wall/methods.json`: `wall.post`, `wall.getComments`, `wall.createComment`, `wall.getById`
- `stats/methods.json`: `stats.getPostReach`
- `stats/objects.json`: `stats_wallpost_stat`
- current VKCOM repository issue `#242` for the Community Token limitation of photo upload methods.

| Capability | Decision | Admission boundary |
|---|---|---|
| `social.post.text` | **adapter-ready / runtime admitted** | `wall.post` is defined for user access tokens; community wall ownership uses negative `owner_id`; canonical Publication ID is reused as `guid`. The `/social` frontend is the operator entry point. |
| `social.post.media` | **adapter-ready / runtime deferred** | Task-088 released media -> `photos.getWallUploadServer` -> safe upload transport -> `photos.saveWallPhoto` -> `wall.post`. A current VKCOM issue demonstrates Community Token failure for the photo-upload path while User Token succeeds, so v1 requires user OAuth. A host media workflow and frontend editor are still required. |
| `social.comments.read` | **adapter-ready / runtime deferred** | `wall.getComments`, bounded to 100 comments per connector call and scoped to the configured community. A persisted comments route and operator screen are still required. |
| `social.comments.reply` | **adapter-ready / runtime deferred** | `wall.createComment`; official schema documents `guid` as the unique identifier that avoids repeated comments. Approval/audit and frontend reply workflow are still required. |
| `social.analytics.read` | **adapter-ready / runtime deferred** | `stats.getPostReach` uses user auth, community owner ID and max 30 post IDs; only bounded post-reach counters are projected. A read model and frontend analytics screen are still required. |
| `social.post.video` | **deferred** | No Task-040 repository qualification of the current end-to-end video upload/save/processing/status contract. |
| `social.post.edit` | **deferred** | The remote method exists, but canonical edit semantics, immutable variant relation, retry/idempotency and audit behavior require an explicit extension review. |
| `social.post.delete` | **deferred** | Remote deletion exists but destructive behavior requires an explicit approval/audit policy and reconciliation semantics before capability admission. |

## Auth decision

Task 040 intentionally requires `social.user-oauth`. This is narrower than “any VK token”: a current issue in VK's own schema repository reports `photos.getWallUploadServer` returning error 27 for a Community Token while the same photo path works with a User Token. The connector therefore fails closed instead of declaring an auth shape that cannot support its admitted media capability.

## Rate-limit decision

VK exposes provider errors including rate-limit/flood conditions. The repository baseline does not infer an account-wide production ceiling. It admits conservative host pacing: one concurrent call, 350 ms minimum interval, 15 s timeout, bounded five-attempt retry policy. Dynamic rate-limit responses remain authoritative for retry guidance.

## Media egress decision

Upload-server URLs are remote data and therefore untrusted. Task 040 validates scheme/authority before passing a stream to host transport. Only HTTPS `vk.com` / `*.vk.com` / `userapi.com` / `*.userapi.com` destinations are accepted, with default/443 port and no URL userinfo or fragment.
