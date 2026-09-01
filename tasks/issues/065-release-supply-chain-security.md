# Task 065: Release supply-chain security

## Objective

Add fail-closed SBOM, secret/license/dependency/container scans, artifact
signing/provenance, and release-checklist CI without giving untrusted changes a
publishing identity.

## Dependencies

001

## Status

Repository-local implementation completed on 2026-08-09 and extended by Task 118 with draft staging, independent downloaded-evidence verification, staged-asset digest binding, and P4 promotion gating; operational acceptance remains an external release fact.

```yaml
local_implementation_status: completed
operational_release_status: blocked
protected_prerelease_smoke: pending
public_release_ready: true
```

Local policy checks and mocked verification are implementation evidence only.
They are not an operational release PASS.

## Deliverables

- immutable action/tool/dependency/container pin policy and automated checker;
- complete artifact/runtime/dependency inventory with fail-closed discovery;
- SBOM, secret, SAST, dependency, license, and container scan gates;
- digest signing, provenance, evidence manifest, and report archival path;
- least-privileged CI and protected release workflow;
- deterministic negative fixtures and tests for every fail-closed policy;
- release checklist, emergency-patch procedure, and updated documentation.

No API, event, or capability contract change is required for this task.

## Acceptance criteria

- `SC-PIN-01`: every remote action/reusable workflow is allowlisted and pinned
  by full commit SHA; CI runners and external tools avoid mutable versions.
- `SC-PIN-02`: every external Compose/Dockerfile/workflow image is allowlisted
  and pinned by OCI SHA-256 digest; all declared platforms resolve.
- `SC-DEP-01`: every Go module and supported package manifest is discovered,
  locked, verified read-only, scanned, and not silently skipped.
- `SC-SBOM-01`: exactly one valid SPDX 2.3 SBOM is archived for every shipped
  subject and linked to the exact binary/image digest.
- `SC-SCAN-01`: secret, SAST, dependency, and container scans fail closed on
  tool/report/data errors; active secrets and policy-blocking findings prevent
  publication.
- `SC-EXC-01`: exceptions are exact-scope, reviewed, justified, expiring, and
  cannot waive an active credential.
- `SC-LIC-01`: SPDX license expressions are evaluated by default-deny policy;
  unknown/denied licenses and an unresolved repository license block the
  applicable release.
- `SC-EVID-01`: artifacts are built once and a retained evidence manifest binds
  the source, subject digests, SBOMs, reports, signatures, provenance, tools,
  database identity, and exceptions.
- `SC-PROV-01`: every first-party release subject is keylessly signed by digest,
  attested, and verified against exact issuer/repository/workflow/ref identity
  before publication.
- `SC-PERM-01`: PR CI is read-only and has no OIDC/publish authority; protected
  release jobs receive only job-specific write permissions.
- `SC-GATE-01`: the publisher depends on every required gate; error suppression,
  missing evidence, and publish-before-verify are rejected.
- `SC-TEST-01`: deterministic valid/invalid fixtures cover action SHA pins,
  local-action escape, tool/dependency drift, image digests, SBOM completeness,
  scanner failure/severity/expiry, SPDX expressions, permissions/DAG, altered
  subjects, and wrong provenance identity/ref.
- `SC-OPS-01`: a protected prerelease on the real hosting platform exercises
  OIDC signing/attestation and independently verifies the downloaded evidence
  bundle. This is required because it cannot be proved locally.

The repository-required test, vet, contract, and build checks also pass, and
the results, risks, and follow-ups are recorded.

## Required negative evidence

At minimum, tests reject mutable/short/unknown actions; path-escaping local
actions; tag-only/latest/variable/malformed images; lock/checksum/toolchain
drift; unverified downloads; malformed/missing/stale scan reports; Critical
findings without a valid exception; wildcard/expired/wrong-scope exceptions;
unknown/denied license expressions; incomplete/wrong-subject SBOMs; privileged
PR jobs; skipped gates; altered/unsigned subjects; and wrong OIDC issuer,
repository, workflow, commit, or ref.

## Pending blockers

1. Task 118 now provides the exact protected-release machinery for `SC-OPS-01`: the workflow stages a non-public draft, P4 independently re-verifies Sigstore/SLSA identity and binds GitHub asset SHA-256 digests before promotion. The actual protected run remains pending until those commands pass on the real hosting account/release tag; repository code alone cannot mark this item accepted.
2. Resolved in Task 117: TORGNEXA-owned Community core is licensed Apache-2.0, the top-level `LICENSE` is present, package metadata is aligned, and `public_release_ready:true` removes only the repository-license blocker.
3. The 2026-08-09 live two-platform runtime scan correctly failed closed.
   Kafka 4.3.1 has 10 High findings per platform; PostgreSQL 18 Alpine has 15
   High and 1 Critical per platform. The dependency-license policy also rejects
   or requires review for findings in every current development-runtime image.
   No active secret was found. Image upgrades, exact finding triage, and legal
   classification must be reviewed rather than waived implicitly.

Risk-exception parsing, exact scoping, expiry, and CODEOWNERS enforcement are
implemented and tested. Applying waivers to scanner output remains deliberately
disabled while `approval_enforced:false`; the current release path therefore
accepts no risk exception and fails closed.

Evidence is tracked in `VALIDATION_REPORT.md`; the operational release record
uses `templates/release-checklist.md`.

### Latest qualification — 2026-09-01

The repository-local Docker production runtime qualification, backup/restore,
PITR, upgrade and workflow checks passed. This evidence is retained under
`qualification/evidence/release-gates-20260901/` (ignored by Git). It does not
replace `SC-OPS-01`: the protected GitHub prerelease, OIDC/Sigstore proof and
independent downloaded-evidence verification still require the real hosting
account and release tag.
