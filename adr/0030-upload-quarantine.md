# ADR 0030: Uploaded Files Are Quarantined Until Security Release

## Decision
All untrusted uploads enter quarantine and pass size/type/archive/parser/malware checks before downstream use. Scanner adapters are pluggable.

## Consequences
Import/media/EDO/compliance workflows must handle asynchronous scan state and fail-closed policies.
