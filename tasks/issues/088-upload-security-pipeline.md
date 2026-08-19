# Task 088: Upload Security Pipeline

## Objective
Implement quarantine-first Upload Security Pipeline for imports/media/documents/plugin packages with type/size/archive/parser/malware controls.

## Dependencies
021, 025, 031, 060, 065

## Deliverables
Upload state model, quarantine/release storage abstraction, MIME sniffing, archive limits, malware-scanner adapter, policy contract, metrics/audit/tests.

## Acceptance
Downstream consumers cannot access unscanned files; archive bombs/path traversal/mismatched MIME test cases rejected; scanner failure policy is explicit.

Run required repository checks and report results, risks and follow-ups.

## Status
Repository-complete. Stage `088b` closes parent Task `088` at repository level.

## Split-stage status
- `088a` Upload Quarantine Foundation: repository-complete. It creates UploadID/state, forced-RLS metadata, tenant-derived storage ports and a fail-closed `ReleasedObjectRef` gate.
- `031` Import/Export: repository-complete and consumes only a current `ReleasedObjectRef`; source size/SHA-256 is re-verified before preview and commit.
- `088b` Upload Security Pipeline: repository-complete. It adds bounded MIME/archive/parser validation, fail-closed malware scanning, immutable security evidence, re-scan/revocation, metrics, authorized release and adversarial tests.
- Parent Task `088`: repository-complete; repository Gate F1 is closed. Operational object-store/scanner deployment qualification remains environment-specific and must not weaken the release gate.
