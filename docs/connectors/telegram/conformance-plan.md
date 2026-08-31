# Telegram Connector Conformance Plan

Task-064 admission uses the common 13-check Connector SDK v1 suite for provider `telegram`.

Provider-specific deterministic evidence additionally covers:

- committed manifest equality and exactly six admitted social capabilities;
- strict negative numeric channel configuration and bot-token validation;
- bot token retained behind `SecretAccessor` and absent from normalized params;
- health `getMe -> getChatMember` exact-channel posting-right verification;
- text plus HTTPS URL-button encoding through the application publication route;
- single-photo and single-video URL-button bounds; album buttons remain rejected;
- one-photo, 2–10 photo album and one-MP4-video publish paths;
- Task-088 media re-open immediately before every upload;
- worker mapping from Core image/gallery/video variants to the provider-neutral
  SocialPublishRequest and receipt-safe publication lifecycle;
- 10 MiB photo / 50 MiB video support ceilings;
- channel-bound remote ID parsing before edit/delete egress;
- single-message text/media edit and explicit album-edit denial;
- bounded single/album deletion;
- real HTTP 429 envelope normalization with `retry_after`;
- ambiguous write transport/5xx -> non-retryable `write_outcome_unknown`;
- nil/provider failure paths return controlled errors rather than panic.

Live qualification must use a non-production Telegram bot and dedicated test channel. Synthetic fixtures are repository contract evidence only and are not represented as live Telegram proof.
