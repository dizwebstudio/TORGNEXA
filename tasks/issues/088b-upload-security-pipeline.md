# Task 088b: Upload Security Pipeline Completion

## Status
Repository-complete. This stage closes parent Task `088` and repository Gate F1.

## Objective
Complete the quarantine-first upload security pipeline so only immutable content that has passed bounded static validation and malware scanning can obtain a current `ReleasedObjectRef`.

## Dependencies
`088a`, `031`, and the parent Task-088 foundations (`021`, `025`, `060`, repository-complete `065`).

## Deliverables
- Executable `QUARANTINED -> VALIDATED -> SCANNING -> CLEAN/REJECTED -> RELEASED` state machine.
- MIME sniffing plus extension/declared-MIME consistency checks.
- Filename/path normalization and archive traversal/symlink/encryption rejection.
- Archive entry/depth/expanded-size/per-entry/nested-size/ratio limits.
- Bounded CSV/JSON/XML/YAML/text parser validation.
- Malware-scanner port plus fail-closed ClamAV INSTREAM adapter.
- Immutable tenant-scoped security evidence and versioned outbox decision/release/rescan events.
- Re-scan that revokes the current release capability before scanning and invalidates stale references.
- Security pipeline metrics boundary and adversarial tests.
- Authorized release only after current CLEAN evidence; downstream consumers must revalidate `ReleasedObjectRef` before every read.

## Acceptance
Path traversal, MIME mismatch, archive bombs, nested-depth overflow, unsafe XML, parser-depth overflow and synthetic malware are rejected before release. Scanner outage is explicitly fail-closed and retryable by default. A clean scanner decision is accepted only after the scanner consumes the complete immutable object. PostgreSQL independently enforces tenant RLS, append-only evidence, evidence shape and legal state transitions. Re-scan invalidates previously issued release references, including references held by Task `031`.
