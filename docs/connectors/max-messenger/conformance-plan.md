# MAX Connector Conformance Plan

Task-064 admission uses the common 13-check Connector SDK v1 suite for provider `max-messenger`.

Provider-specific deterministic evidence additionally covers:

- committed manifest equality and exactly five admitted capabilities;
- strict numeric channel configuration plus separate bot-token/webhook-secret references;
- exact bot/channel health proof and posting permission;
- text and HTTPS link-button publication;
- Task-088 image/video re-open before upload;
- official upload-host SSRF allowlist and media ceilings;
- worker mapping from Core image/gallery/video variants to the provider-neutral
  SocialPublishRequest and receipt-safe publication lifecycle;
- exact channel-bound remote publication IDs and status reads;
- non-retryable ambiguous write outcomes;
- production webhook endpoint validation, subscription/unsubscription and fixed update allowlist;
- constant-time webhook-secret verification;
- rejection of foreign-channel/malformed timestamp events before dedup;
- canonical-JSON SHA-256 delivery IDs and duplicate recognition through host-owned dedup.

Live qualification must use a non-production MAX bot and dedicated test channel with production-style HTTPS webhook infrastructure. Synthetic fixtures and the Linux sandbox report are repository evidence, not a claim of live MAX certification.
