# Release Checklist

Copy this template into the release evidence bundle. An unchecked item, an
empty evidence field, or an unjustified `N/A` blocks publication.

## Candidate

- Version/tag:
- Git commit SHA:
- Repository:
- Protected release environment:
- Workflow run URL/ID:
- Release owner:
- Evidence retention/expiry:
- `public_release_ready: true` only when repository licensing is resolved; this does not imply P4 PASS

## Immutable inputs

- [ ] `SC-PIN-01`: every remote action/reusable workflow is allowlisted and
  pinned to a full commit SHA; local actions were resolved and checked.
  Evidence:
- [ ] `SC-PIN-02`: every deployment/CI/base image is allowlisted and pinned to
  an OCI SHA-256 digest; declared platforms resolve.
  Evidence:
- [ ] `SC-DEP-01`: every dependency/tool manifest is discovered, locked, and
  verified read-only; no unsupported ecosystem was silently skipped.
  Evidence:
- [ ] Toolchain version and actual hosted runner image are recorded.
  Evidence:

## Build, SBOM, and reports

- [ ] Release inventory matches all discovered binaries/images/plugins.
  Evidence:
- [ ] Artifacts were built once from the candidate SHA; `SHA256SUMS` covers
  every subject and no later rebuild occurred.
  Evidence:
- [ ] `SC-SBOM-01`: exactly one valid SPDX 2.3 SBOM exists for every shipped
  subject and is linked to its digest.
  Evidence:
- [ ] `SC-SCAN-01`: secret, SAST, dependency, and container scans completed on
  the exact source/artifact/image set with fresh vulnerability data.
  Evidence:
- [ ] `SC-LIC-01`: license report and policy evaluation passed.
  Evidence:
- [ ] Sanitized reports, SBOMs, checksums, and scanner/database versions were
  archived with explicit retention.
  Evidence:

## Findings and exceptions

- [ ] No active secret finding exists.
- [ ] No unexcepted release-blocking vulnerability/SAST/container finding
  exists.
- [ ] Every exception is exact-scope, reviewed, unexpired, and linked below.
  Exception IDs/links (write `none` when empty):
- [ ] Medium/Low findings and follow-up owners are recorded rather than hidden.
  Evidence:

## Signatures and provenance

- [ ] `SC-PROV-01`: every first-party subject digest has a keyless signature
  and in-toto/SLSA provenance bundle.
  Evidence:
- [ ] Independent verification matched artifact digest, issuer, repository,
  workflow identity, commit, and tag/ref.
  Evidence:
- [ ] No third-party image was re-signed as a TORGNEXA artifact.
- [ ] Plugin signing/provenance/compatibility verification passed, or plugin
  packaging is explicitly not applicable.
  Evidence/N/A justification:

## Product and operational qualification

- [ ] Tests, vet, formatting, contracts, migrations, and build gates pass.
  Evidence:
- [ ] Compatibility and release notes identify API/event/plugin/migration
  impact.
  Evidence:
- [ ] Upgrade rehearsal, backup checkpoint, and rollback/repair evidence pass,
  or a pre-production N/A is explicitly justified.
  Evidence/N/A justification:
- [ ] `ARCH-OPS-01`: GitHub applied-rules evidence proves deletion/non-fast-forward protection, PR approvals, a required Team reviewer for architecture paths, and the SHA-pinned `.github/workflows/architecture-required.yml` ruleset workflow.
  Evidence:
- [ ] `SC-OPS-01`: protected tag release staged a non-public draft; downloaded evidence passed independent Sigstore/SLSA identity verification and GitHub staged-asset SHA-256 binding.
  Evidence:
- [ ] Production posture, P3 topology/restart/restore/upgrade evidence, and selected live connector qualification passed for the exact release.
  Evidence:
- [ ] Repository license and release metadata are approved.
  Evidence:

## P4 go-live and publication decision

- [ ] Retained `p4-go-live.json` has `status: PASS` and matches the exact tag/commit/repository.
- [ ] The GitHub release is still a draft before final approval.

- [ ] All required gates above passed for the exact evidence-manifest digest.
- [ ] Publisher has approved only the already verified subject digests.
- Decision: `BLOCKED` (change to `APPROVED` only after every required item).
- Approver and timestamp:
- Evidence manifest SHA-256:

## Emergency patch only

- Tracking incident/advisory:
- Expedited reviewers:
- Time-limited exception/follow-up IDs:
- [ ] The normal signing, provenance, secret, Critical-finding, and archival
  gates still passed; no emergency bypass was used.
