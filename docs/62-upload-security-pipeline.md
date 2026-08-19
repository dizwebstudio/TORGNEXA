# Upload Security Pipeline

All user/provider uploads are untrusted: XLSX/CSV/XML/YML/JSON/ZIP, images, video, PDF/evidence, EDO/compliance files and plugin packages.

## Foundation and release boundary

Task `088a` created canonical UploadID/state, tenant-derived quarantine/release keys and the private-field `ReleasedObjectRef`. Task `088b` now completes the security path. No client object key, URL, filename, tenant identifier or scan flag can authorize release.

Canonical state flow:

`RECEIVED -> QUARANTINED -> VALIDATED -> SCANNING -> CLEAN|REJECTED -> RELEASED`.

Static validation failure may reject directly from `QUARANTINED`. Explicit re-scan moves `CLEAN`, `REJECTED` or `RELEASED` back to `QUARANTINED` and revokes the current evidence/release pointer before any new scanner work.

Downstream consumers receive only `ReleasedObjectRef`. They must call `AccessGate.ValidateReleasedRef` immediately before every object read. The reference binds upload ID, released object key, size, SHA-256, security-evidence ID and record version, so a re-scan invalidates stale references.

## Validation pipeline

Before malware scanning the pipeline re-verifies the immutable quarantine object size/SHA-256 and applies bounded checks:

- filename/path normalization; client filenames never become storage paths;
- MIME/content sniffing plus declared MIME and extension consistency;
- ZIP/XLSX archive entry count, nesting, encrypted-entry, symlink, path traversal, per-entry size, total expanded size and expansion-ratio limits;
- bounded JSON depth/token parsing;
- bounded XML depth/token/attribute parsing with DOCTYPE/ENTITY rejection;
- bounded CSV rows/columns/field sizes;
- bounded text/YAML line parsing;
- file and parser limits from `contracts/upload/upload-policy.yaml`.

Archive inspection decompresses entries through bounded readers instead of trusting attacker-controlled ZIP metadata alone. Nested ZIP inspection is separately bounded.

## Malware scanning

`MalwareScanner` is provider-neutral. A concrete Community adapter implements the ClamAV `INSTREAM` protocol with operator-owned TCP/Unix configuration; upload metadata cannot choose the scanner endpoint. Threat signature text is not persisted verbatim: evidence stores a deterministic hashed `threat_code`.

A scanner may return clean only after consuming the complete immutable object. If the scanner errors, returns malformed evidence or does not consume all bytes, the default policy is `retry_fail_closed`: the upload remains `SCANNING`, an immutable error attempt is recorded, and no consumer reference can be issued. Deployments may choose fail-closed rejection instead, but there is no fail-open mode.

## Immutable evidence and events

Migration `000023_upload_security_pipeline.sql` adds forced-RLS append-only `upload_security_evidence`. Evidence contains only bounded machine-readable checks, policy/scanner versions, SHA-256/size and decision metadata — never raw file contents or scanner logs.

Terminal decisions and release/rescan state changes enqueue versioned outbox events in the same PostgreSQL transaction:

- `security.upload.decision.v1`;
- `security.upload.released.v1`;
- `security.upload.rescan_requested.v1`.

The existing `security.upload.quarantined.v1` event remains unchanged.

## Failure and re-scan model

Storage/promote failures are fail-closed. If promotion succeeds but the database release transaction fails, the released object is an unreachable orphan until cleanup; `AccessGate` still denies it because canonical state is not `RELEASED`.

Scanner/signature or policy changes use `RequestRescan`. The current release capability is revoked first, the full static validation is repeated, content integrity is rechecked before scanning, and a new immutable evidence attempt is required before release can be recreated.

Metrics expose only bounded operation/outcome/byte/duration observations. File names, content, tenant IDs, threat signature text and credentials are excluded.
