# Released Object Reference v1

Downstream consumers such as Task 031 Import/Export MUST receive upload content only through the upload `AccessGate`.

The canonical runtime reference is `uploads.ReleasedObjectRef`. Its fields are private and there is no public constructor. A reference can be returned only when all of these conditions hold for the authenticated `organization/workspace` scope:

- the `UploadID` is canonical;
- the canonical upload record is `RELEASED`;
- the released object key is the server-derived tenant path for that UploadID;
- immutable content size and SHA-256 are present;
- immutable Task-088 security evidence is present and is the current clean evidence;
- the reference is bound to the current upload record version.

A consumer MUST call `AccessGate.ValidateReleasedRef` immediately before every object read. Re-scan first transitions the canonical record away from `RELEASED`, clears its current evidence/release pointer and increments its version, so previously issued references fail closed before new scanner work begins.

Task `031` follows this rule both before preview reads and immediately before commit re-reads. Client-provided object keys, bucket names, signed URLs, tenant identifiers, release flags, scan decisions or previously cached stale references are never valid substitutes for a current `ReleasedObjectRef`.
