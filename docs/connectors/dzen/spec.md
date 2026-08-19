# Dzen Task-047 Spec

Audit date: **2026-08-12**.

## Admission decision

No architecture provider, manifest, credential scope, remote endpoint, rate policy, or live SocialPublisher implementation is admitted in Task 047 because the audit did not obtain a qualified official public publishing contract.

This is deliberate capability limitation, not an implementation placeholder. The integration must not fall back to private editor/Studio calls, browser session cookies, headless UI automation or undocumented RPCs.

## Content transformer

`scripts/dzen-content-transformer.py` converts an already validated Task-020 `SocialPublishRequest` into a bounded `ContentPackage` without network effects:

- `post`: text and/or released image references; video-kind requests are rejected;
- `article`: text is mandatory and any media must be images;
- `video`: exactly one Task-020 video reference is mandatory;
- buttons are rejected because no qualified Dzen remote contract exists for their representation;
- media stays as canonical UploadID references; no internal object key or secret is exported.

`publish()` always raises the explicit live-publishing-unavailable gate. That fail-closed behavior is the accepted Task-047 runtime behavior until a future official contract is audited and architecture/provider admission is repeated.
