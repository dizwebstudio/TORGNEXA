# Task 088a: Upload Quarantine Foundation

## Objective
Establish the non-bypassable quarantine boundary required before Task `031` can consume uploaded objects.

## Dependencies
021, 025, 060, 065 repository foundations. This stage was the first step of `088a -> 031 -> 088b`; that chain is now repository-complete and parent Task `088` is closed.

## Deliverables
- Opaque `UploadID` and full reserved upload state model.
- Tenant-derived quarantine/released object locators; client filenames never become keys.
- Quarantine and future release storage ports.
- Canonical PostgreSQL upload metadata with forced RLS.
- Repository API limited to create/receive/quarantine/read; no release mutation.
- Transactional `security.upload.quarantined.v1` outbox intent.
- `ReleasedObjectRef` plus fail-closed `AccessGate` for downstream consumers.
- Upload policy contract documenting `RELEASED` as the only consumer state.
- Migration/security tests and architecture review.

## Acceptance
`RECEIVED -> QUARANTINED` succeeds and records actual size/SHA-256 under a server-derived tenant path. Cross-tenant or non-released object resolution returns the same fail-closed error. The Go repository and SQL migration expose no transition beyond quarantine before Task `088b`. Task `031` can depend on `ReleasedObjectRef` without gaining access to unscanned bytes.

## Status
Repository-complete. This stage alone did **not** close parent Task `088`; Task `031` and stage `088b` have since completed the chain. Parent Task `088` is now repository-complete with MIME/archive/parser/malware controls, immutable evidence, re-scan/revocation, metrics and authorized release.
