# RUTUBE Connector Spec

Audit date: **2026-08-12**.

## Identity and credential boundary

One TORGNEXA connector account binds exactly one `ChannelID` and one non-secret `ContractID`. `ContractID` identifies the current official partner contract/profile installed in the host transport; it is not an endpoint or credential.

The account secret reference contains the partner credential and is exposed only inside the Task-021 `SecretAccessor` callback. Health calls `PartnerTransport.ResolveChannel` and becomes healthy only when the exact configured channel is returned.

## Publication mapping

The manifest grants only `social.post.video`. Social Core still owns Content, immutable ContentVariant, Schedule and Publication state. The provider receives one READY `SocialPublishRequest` and one Task-088 released MP4 reference.

The adapter executes this state machine:

`READY -> create-session -> upload-bytes -> commit -> processing|published`

A canonical Publication ID is passed as `ExternalID` to the partner transport so a qualified transport can use provider-side idempotency/reconciliation where the account contract supports it.

Metadata is deterministic: the first non-empty text line becomes a title (maximum 200 Unicode code points), while the bounded request text becomes the description (maximum 5000 code points). If text is empty, a stable title derived from the canonical Publication ID is used. No tags/category are invented because no current public upload contract was qualified.

Only current Task-088 released `video/mp4` objects are accepted. The adapter applies a conservative local range of 16 KiB..10 GiB, then additionally requires the partner-issued upload session's `MaxBytes` to admit the object. This 10 GiB ceiling is a TORGNEXA safety bound, not a claim about RUTUBE's platform limit.

## Status and reconciliation

Remote publication IDs are `rutube:<channel-id>:<video-id>` and are accepted only for the exact configured channel. Typed partner states map as:

- `processing` -> `SocialRemoteProcessing`;
- `published` -> `SocialRemotePublished`;
- `failed` + safe reason code -> `SocialRemoteFailed`.

Malformed or foreign channel/video state fails closed.

## Quota and ambiguous writes

`quota_exceeded` and `rate_limited` become normalized Connector SDK `rate_limited` errors with bounded retry guidance. Unknown/unavailable create/commit write outcomes become `write_outcome_unknown`; ambiguous upload streaming becomes `upload_outcome_unknown`. TORGNEXA must reconcile instead of blindly creating a second upload session.

No raw partner body, credential, URL or Studio session data is returned to Core.
