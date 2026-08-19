# VK Connector Conformance Plan

Task-064 admission uses the common 13-check Connector SDK v1 suite for provider `vk`.

Provider-specific deterministic evidence additionally covers:

- manifest/committed JSON equality and explicit denial of video/edit/delete;
- OAuth user token retained behind `SecretAccessor`;
- no `access_token` parameter leakage from the provider request model;
- canonical `PublicationID -> wall.post.guid` retry identity;
- Task-088 media re-open immediately before upload;
- dynamic upload URL allowlist/SSRF rejection before upload transport;
- image upload/save/attachment pipeline;
- exact configured-community remote-ID enforcement;
- published vs `remote_missing` status mapping;
- opaque comment cursor bound to publication;
- reply idempotency through `wall.createComment.guid`;
- max-30 post reach read and safe metric projection;
- VK envelope rate-limit normalization without raw provider `error_msg` leakage.

Live qualification must use non-production VK credentials and a test community. Synthetic fixtures are repository contract evidence only; they are not represented as live VK proof.
