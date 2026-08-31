# ADR-0134 — HTTPS-кнопки в публикациях MAX

Status: Accepted

## Context

The MAX adapter already validates and encodes bounded HTTPS URL buttons, and
the provider manifest declares `social.post.buttons`. The built-in runtime
support contract still omitted the capability, so the host API, worker and UI
correctly kept the feature closed even though the adapter was ready.

## Decision

Admit `social.post.buttons` for MAX in the built-in runtime-support contract.
Reuse the Task-181 provider-neutral button snapshot, Social API validation,
account/channel capability gates, leased worker mapping and existing MAX
inline-keyboard encoder. No provider-specific fields, callback data or new
remote lifecycle are introduced.

## Security and compatibility impact

Task 181's HTTPS-only, bounded button contract remains authoritative. The
change is additive to runtime capability metadata and generated projections;
no migration, event or public API schema change is needed. Existing edit/delete,
status and webhook ceilings remain closed.

## Operational impact

The UI advertises buttons only when the MAX connector account and channel have
the admitted capability. Removing the runtime capability or account setting
stops new button publications without affecting existing publication evidence.
Credentialed live MAX qualification remains a release gate.
