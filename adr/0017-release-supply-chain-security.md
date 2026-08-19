# ADR 0017: Release supply-chain security

## Status

Accepted. Operational qualification is pending the protected prerelease smoke
defined by `docs/45-release-supply-chain-security.md`.

## Context

Release inputs include source, Go modules and tools, CI actions, hosted runners,
base/runtime images, and vulnerability data. Mutable references or a rebuild
between scanning and publication break source-to-artifact traceability. Local
tests also cannot prove hosted branch protection, OIDC identity, transparency
services, or registry permissions.

## Decision

Release artifacts require an immutable inventory, locked inputs, secret/SAST/
dependency/license/container gates, per-subject SBOMs, digest signing, and
signed provenance. The exact artifact bytes are built once and their digests
flow through every later gate. Plugin artifacts follow the same trust model.

CI uses least privilege. Pull-request execution cannot sign or publish.
Release signing is keyless through a short-lived protected OIDC identity;
private release keys do not transit generic CI workers.

Critical findings fail closed unless the applicable policy explicitly permits
an exact, reviewed, time-limited exception. Scanner failure or missing evidence
is a failed gate, not a clean result. Vulnerability data must be fresh and its
identity recorded rather than frozen indefinitely for reproducibility.

Third-party runtime images are pinned and scanned. TORGNEXA does not re-sign an
upstream image as if it were a first-party artifact.

## Operational qualification

Repository tests and mocked signature verification prove implementation only.
Task 065 also requires one protected prerelease dry run on the real hosting
platform with independent verification of the downloaded subject, OIDC
identity, SBOM, provenance, and archived evidence. Until it passes, local
implementation must not be reported as an operational release PASS.

The unresolved Community license decision independently keeps
`public_release_ready:false`; engineering controls cannot substitute for legal
approval.

## Consequences

- Mutable actions, tools, dependencies, and image references are rejected.
- Release reports and attestations become retained auditable artifacts.
- Emergency patches keep the same technical gates and use expedited review,
  not a bypass.
- Hosted protection and OIDC claims require external evidence and cannot be
  inferred from repository configuration alone.
