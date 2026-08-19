# Threads Capability Audit

Audited 2026-08-11 against current official Meta Threads API documentation.

| Capability | Decision | Admission boundary |
|---|---|---|
| `social.post.text` | **granted** | max 500 code points. |
| `social.post.media` | **granted** | JPEG/PNG, max 8 MiB each, image carousel max 20 in this SDK-v1 adapter. |
| `social.post.video` | **granted** | one MP4, max 1 GiB. |
| mixed carousel | **deferred** | official API supports it, but current Social SDK models image collection or one video; Task 044 makes no Core changes. |
| replies/insights/delete | **not declared** | outside Task-044 qualification. |

`threads_basic` is required for Threads API calls and `threads_content_publish` for publish endpoints. The provider keeps account identity exact and denies caller override.

Token lifecycle is admitted as a provider-local operation: short-lived exchange and long-lived refresh use the documented grants and rotate secret material only through host-owned secret storage.
