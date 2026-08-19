# Social Publishing

Model: `Content -> ContentVariant -> Publication -> ChannelAccount`. One master item produces channel-specific variants by deterministic rules or AI-assisted adaptation.

Capabilities cover text/image/gallery/video/article/live, scheduling, edit/delete, comments/replies and analytics.

Publishing flow: create -> adapt -> validate -> optional approval -> schedule -> publish -> status -> analytics.

Media pipeline: S3 source -> validate -> resize/transcode/thumbnail/subtitle -> derivative -> upload.

n8n uses generic TORGNEXA resources through the public REST/webhook boundary; Task `019` establishes the external node package and webhook trigger without provider branches. OpenClaw uses MCP with normal approval policy.

## Task 020 repository baseline

Task `020` is the canonical implementation of this model. `Content` remains the editable master, `ContentVariant` is an immutable publish snapshot, `ChannelAccount` is a provider-neutral projection of one social connector account/capability set, and `Publication` owns the TORGNEXA schedule and status state machine. See `docs/71-social-core.md` and ADR `0049` for exact invariants.

The scheduler remains TORGNEXA-owned; provider connectors receive publish-now/status operations only. Media crosses the provider boundary only as Task-088 released `UploadID` references through a host `MediaAccessor`, and release is revalidated immediately before byte access. Provider remote publication IDs stay outside Social Core.
