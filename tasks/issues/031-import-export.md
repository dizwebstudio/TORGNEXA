# Task 031: Import / Export

## Status
Repository-complete. Parent Task `088` is now repository-complete; imports accept only a current security-evidenced `ReleasedObjectRef` and revalidate it immediately before every source read.

## Objective
Implement the provider-neutral CSV/JSON import pipeline skeleton: released-object integrity, versioned mapping, validation/dedupe, preview, commit and result reporting, plus canonical Product CSV/JSON export encoding.

## Dependencies
004, 007, 008, 017, 030, 088a

## Deliverables
ReleasedObjectRef-only source boundary; bounded CSV/JSON parser; reusable/versioned mapping with deterministic fingerprint; canonical Product validation and within-file dedupe; opaque prepared-preview commit boundary; source SHA-256 re-verification before writes; idempotent exact-match replay; bounded result report; Product CSV/JSON export encoder; Draft 2020-12 contracts; tests/docs/architecture evidence.

## Acceptance
Raw/quarantine object keys, filenames, URLs and arbitrary client streams cannot enter the importer. Source size/SHA-256 must match the release reference before preview and again immediately before commit. Invalid or duplicate rows make preview non-committable. Commit can only consume an opaque value returned by Preview and uses the existing Catalog port; exact replay is reported as unchanged while divergent conflicts fail per-row without overwriting canonical data. Mapping identity/version/field bindings are fingerprinted into preview/result evidence. Task `088b` now authorizes release only after current CLEAN security evidence; stale references are revoked by re-scan and fail before preview/commit.
