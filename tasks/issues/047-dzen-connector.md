# Task 047: Dzen connector

## Objective
Audit and implement supported article/post/video publication capabilities through Social Connector SDK.

## Dependencies
020

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Explicit capability limitations; content transformer fixtures.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-12

Status: **repository-complete** (capability-audit / transformer completion; no live provider admitted).

- Completed the current official-surface audit without establishing a qualified public Dzen publishing contract sufficient for Connector SDK admission.
- Added deterministic Social Core content transformation for `post`, `article`, and `video` packages with canonical PublicationID/UploadID preservation and explicit type validation.
- Added fixture tests for all three content shapes plus negative article/video behavior.
- Live `publish()` is deliberately fail-closed with an explicit unavailable error; Task 047 does not use private Studio/editor endpoints, browser cookies, DOM/headless automation, reverse-engineered RPCs or a third-party publishing proxy.
- No `dzen` provider manifest, credential authority, egress route, Core branch, migration, public API or EventBus contract is introduced.
- Added capability/spec documentation and architecture review `ARCH-047`; Task-064 provider conformance is intentionally not applicable because provider admission did not occur.

The canonical dependency-ready social task remains `046 Rutube Connector`; after it, `048 YouTube Connector` is the remaining Phase-5 channel task because Task `047` is already complete.
