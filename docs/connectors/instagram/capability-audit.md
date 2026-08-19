# Instagram Capability Audit

Audited 2026-08-11 against current official Meta Instagram Platform documentation.

| Capability | Decision | Admission boundary |
|---|---|---|
| `social.post.text` | **denied** | Instagram feed publishing requires media; no fake text-only adapter. |
| `social.post.media` | **granted** | 1 JPEG or 2-10 JPEG carousel, Task-088 revalidation and host HTTPS staging. |
| `social.post.video` | **granted** | one MP4 Reel, max 300 MiB, container readiness required. |
| Stories | **not declared** | Social SDK v1 has no Story kind and Task 043 does not widen Core. |
| mixed carousel | **not declared** | current provider-neutral `SocialPostMedia` models image-only collections. |
| comments/analytics/edit/delete | **not declared** | outside Task-043 qualification. |

Account access is limited to professional Business/Creator accounts and the publish baseline requires `instagram_business_basic` plus `instagram_business_content_publish`.

The official API retrieves public `image_url`/`video_url`; therefore internal storage references are never sent to Meta. A host `MediaStager` produces an ephemeral HTTPS retrieval URL only after current release evidence is revalidated.
