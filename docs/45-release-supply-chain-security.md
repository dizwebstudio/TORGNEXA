# Release & Software Supply-Chain Security

Every release must be traceable from an immutable source revision through the
build and security gates to the exact published artifact digest. A successful
local build is development evidence; it is not an operational release
qualification.

The terms **must**, **must not**, **required**, and **forbidden** in this
document are release gates. Missing tools, unavailable vulnerability data,
malformed reports, and missing evidence fail closed.

## Artifact and dependency inventory

The checked-in release inventory is the authority for artifacts that TORGNEXA
builds or ships. Discovery and the inventory must agree in both directions:

- every releaseable `cmd/*` binary is listed;
- every first-party container image and plugin package is listed when one is
  introduced;
- every third-party image referenced by shipped deployment configuration is
  classified as shipped runtime or development-only;
- an unregistered discovered artifact fails the gate, and a stale inventory
  entry also fails it.

TORGNEXA currently has releaseable Go commands but no first-party container or
plugin package. That is an explicit not-applicable case, not permission to skip
future image or plugin gates. Third-party images are never re-signed as
TORGNEXA artifacts.

All Go modules and future supported package-manager manifests are discovered
recursively. A newly discovered ecosystem without an implemented lockfile and
scanner policy fails closed instead of being silently ignored.

## Immutable inputs

### CI actions and tools (`SC-PIN-01`)

- Every remote GitHub Action and reusable workflow uses an allowlisted full
  lowercase 40-hex commit SHA. Mutable tags, branches, short SHAs, and
  expressions in `uses:` are forbidden, including for first-party actions.
- Local actions must resolve inside the repository, must not be symlinks, and
  their nested remote actions are checked recursively.
- Docker actions use an immutable OCI digest.
- Hosted runners use a fixed runner label rather than a `*-latest` label. The
  actual runner image release is recorded in provenance because hosted labels
  are not content-addressed.
- External CLI versions are exact. Downloaded binaries require a checked-in
  SHA-256 verification; pipe-to-shell installers and `@latest` are forbidden.

### Dependencies (`SC-DEP-01`)

- The Go language and toolchain versions are exact and consistent across all
  modules.
- Validation uses `GOWORK=off`, a read-only module graph, `go mod verify`, and
  `go mod tidy -diff` for every module.
- Required module and tool versions are immutable and covered by checksums.
  Repository-external local replacements are forbidden.
- Lockfile or checksum drift is a hard failure. Scanner/tool dependencies are
  inventoried separately from dependencies linked into shipped artifacts.

Vulnerability databases are different from build dependencies: a release uses
a fresh trusted snapshot and records its timestamp and digest. Pinning an old
database indefinitely is forbidden.

### Containers (`SC-PIN-02`)

Every external image in Compose, Dockerfile `FROM`, CI job/service/container,
and Docker action references an allowlisted registry and repository plus
`@sha256:<64 lowercase hex>`. A readable tag may precede the digest. Tag-only,
`latest`, variable/interpolated, short, or malformed digests are forbidden;
`FROM scratch` is the only base-image exception.

The networked gate resolves every digest and verifies every declared target
platform. Missing manifests, missing platforms, registry errors, or scanner
errors fail closed.

## Build and evidence chain (`SC-EVID-01`)

Release artifacts are built once in a clean directory. A SHA-256 manifest is
created immediately, and SBOM generation, scanning, signing, attestation, and
publication consume those exact bytes or image digests. Rebuilding or
retagging after a security gate is forbidden unless every downstream gate is
repeated for the new digest.

The release evidence manifest binds:

- release version, immutable Git commit, tag/ref, repository, and workflow run;
- every artifact name, type, platform, and SHA-256 or OCI digest;
- the digest of every SBOM, vulnerability, license, SAST, secret, and container
  report;
- signature and provenance bundle references;
- scanner/tool versions and vulnerability database identity;
- applicable risk exceptions and manual qualification evidence.

Evidence and sanitized reports are archived even when a gate fails. Failed
candidate binaries are not attached to a public release. Retention is explicit
in workflow configuration; missing evidence or implicit retention is not a
pass.

## SBOM (`SC-SBOM-01`)

Generate schema-valid SPDX 2.3 JSON for every shipped binary and container
image using a pinned generator. Each SBOM must have non-empty package and tool
metadata and be linked to the exact subject digest in the evidence manifest.
There must be exactly one current SBOM for every inventory subject and no
unregistered or stale SBOM.

Shipped third-party runtime images receive an SBOM and scan record. Images
classified as development-only remain digest-pinned and scanned, but are not
part of the TORGNEXA artifact-signing set.

## Security and license gates

### Scans (`SC-SCAN-01`)

Pinned tools perform secret scanning, SAST, Go dependency analysis, and
container scanning. Every Go module and every pinned shipped/development image
is covered. Scanners emit machine-readable, sanitized reports.

Release-blocking outcomes are:

- any active secret finding;
- any reachable Go vulnerability unless an exact approved exception applies;
- every unexcepted Critical finding; the default production policy also blocks
  High SAST, dependency, and container findings;
- a scanner error, missing or malformed report, or stale vulnerability data.

Medium and Low findings remain visible in release evidence and must not be
discarded by summary-only logs. Scheduled scans repeat against the default
branch because vulnerability state changes without source changes.

### Risk exceptions (`SC-EXC-01`)

A vulnerability or SAST exception is a checked-in structured record scoped to
the exact finding, component, version or digest, release, justification,
tracking issue, approver, approval time, and expiry. Wildcards, expired entries,
and scope mismatches are invalid. Approval is enforced through protected
review/CODEOWNERS; a name written into an unreviewed file is not approval.

An active credential cannot be risk-accepted: revoke/rotate it and remove the
material. A secret-scanner false-positive suppression must use a non-secret
fingerprint, exact scope, justification, reviewer, and expiry.

The checked-in policy currently sets `approval_enforced:false` and contains no
exceptions. This is intentional: validators exercise exact scope, expiry, and
CODEOWNERS rules, but the scanner/evidence pipeline refuses every non-empty
exception set until protected-review enforcement is operationally qualified.
Consequently no finding is currently waived, even when the schema could
represent a future reviewed exception.

### Licenses (`SC-LIC-01`)

License policy uses parsed SPDX expressions and defaults to deny for
`UNKNOWN`, `NOASSERTION`, custom/unrecognized, and explicitly denied licenses.
For `AND`, every term must be permitted. For `OR`, the selected permitted
license must be recorded; substring matching is forbidden. A legal exception
is package/version-scoped, reviewed, justified, and expiring.

Task 117 records the approved TORGNEXA-owned Community-core repository license as Apache-2.0. The top-level `LICENSE`, `LICENSE-DECISION.md`, and owned package metadata carry that decision and `supply-chain/release-artifacts.json` may set `public_release_ready:true`. This field removes only the repository-license blocker: dependency-license, vulnerability, provenance/signing, protected-hosting and deployment qualification gates remain independently fail-closed.

## Signing and provenance (`SC-PROV-01`)

A trusted release job uses short-lived OIDC identity for keyless signing. No
private signing key is committed, placed in a generic CI secret, or passed to a
worker/plugin. Every first-party artifact and image is signed by digest and has
an in-toto Statement/SLSA provenance attestation containing at least:

- the exact subject digest;
- source repository, commit, tag/ref, and workflow identity;
- builder identity and invocation parameters;
- material identities for the toolchain, dependencies, and base images.

Before publication, verification must bind the subject digest to the expected
OIDC issuer and exact repository/workflow/ref identity. Broad identity patterns
are forbidden. Signature and transparency/verification bundles are archived.

Official and verified plugin artifacts follow the same digest, SBOM, scan,
signature, provenance, compatibility, and revocation model when plugin
packaging exists.

## CI identity and release orchestration (`SC-PERM-01`, `SC-GATE-01`)

Normal push/pull-request jobs are unprivileged: contents are read-only,
checkout credentials are not persisted, and untrusted PR code receives no
OIDC or publishing permission. `pull_request_target` must not execute
untrusted repository code. SARIF upload permission, if needed, is isolated to
the reporting job.

Only a protected release environment may grant `id-token: write` and
attestation permission. `contents: write` and package-registry permission are
limited to the final publisher. `write-all` is forbidden.

The publisher has explicit successful dependencies on tests, contracts,
build/hash, SBOM, secret, SAST, vulnerability, license, container, signature,
and provenance verification. `continue-on-error`, shell error suppression, a
missing report, or publish-before-verify cannot convert a failed gate into a
release.

## Local validation versus operational qualification (`SC-OPS-01`)

Local/offline validation proves deterministic policy behavior:

- pin, inventory, workflow-DAG, and least-privilege policy checks;
- negative fixtures for every fail-closed branch;
- module/checksum checks and clean artifact hashing;
- SBOM, scanner-report, exception, license, provenance, and evidence parsers;
- build and repository-required tests when their verified caches are present.

Unprivileged network CI additionally proves current vulnerability data,
action-SHA existence, image digest/platform resolution, live scans, and report
archival.

Operational release qualification requires a protected prerelease run on the
real hosting platform. The run must build an immutable prerelease tag, exercise
the actual OIDC signer/attester and protected environment, download the
archived candidate, and independently verify artifact hash, SBOM linkage,
signature identity, provenance subject/source, and report completeness. This
external smoke is a Task 065 blocker while pending. Local implementation or
mocked signing is not an operational release PASS.

Branch/tag rulesets, required reviewers, OIDC issuer behavior, transparency
service behavior, registry permissions, and hosted artifact retention cannot
be proven from a filesystem-only sandbox. Their evidence belongs to the
protected prerelease record.

The latest local live scan (2026-08-09) passed all first-party source gates and
found no runtime-image secrets. It blocked the candidate because Kafka 4.3.1
reported 10 High findings on each supported architecture, PostgreSQL 18 Alpine
reported 15 High and 1 Critical on each architecture, and the default-deny
license policy rejected or routed findings from all four development-runtime
images to legal review. ClickHouse and Valkey had no High/Critical
vulnerabilities in that database snapshot. These are release-input blockers,
not reasons to weaken scanner thresholds or silently reclassify licenses.

## Release and emergency-patch procedure

1. Build from a protected semantic-version tag whose commit is on the approved
   release ancestry.
2. Run all automated gates on that exact commit and artifact set.
3. Complete `templates/release-checklist.md` with evidence links and explicit
   justified not-applicable entries.
4. Verify signatures, provenance, SBOM linkage, and evidence independently.
5. Publish only after the protected environment approves the verified digest.

Emergency patches use the same technical gates. Review and rollout may be
expedited, but signing, provenance, secret checks, Critical vulnerability
policy, and evidence archival are never bypassed. Any permitted time-limited
risk exception records the owner and mandatory follow-up deadline.

## Task 078 plugin publication linkage

Plugin marketplace governance consumes this evidence rather than duplicating it. `official` and `verified` marketplace versions require successful conformance, malware/supply-chain, license/security-contact, subject-bound SBOM and provenance review for the exact artifact digest. `community`/`private` packages still require conformance, supply-chain/malware and legal/security-contact review, but their trust label does not claim verified builder identity. A later artifact is a new subject and must be reviewed and consented again.


## P4 staging and go-live promotion

Task 118 separates staging from publication. A protected tag release that passes the existing test/runtime/security/attest/verify DAG creates a **draft** GitHub Release and uploads the exact evidence manifest, deterministic evidence bundle, and verified first-party binary subjects. The release job does not clear the draft flag.

`make p4-qualification` is the independent go-live layer. It requires the exact tagged tree and real P3 topology evidence, captures the active GitHub rules applying to the protected branch, requires a SHA-pinned ruleset workflow plus a Team architecture reviewer, independently re-verifies every first-party Sigstore bundle and SLSA provenance identity, compares GitHub Release asset SHA-256 digests with the locally verified staged bytes, validates reviewed non-secret production posture, and performs two consecutive remote health checks for every active production connector account.

Only a retained `p4-go-live.json` with `status: PASS` can authorize `make p4-publish`. Promotion re-hashes all subordinate P4 evidence, requires the exact clean release tag, rechecks every staged asset digest/size with no extras, uploads the root go-live report itself, and only then clears the draft flag. The promoter re-reads the GitHub draft and refuses promotion unless the expected evidence bundle, manifest and four first-party binaries are present. Thus Task 118 provides publication machinery without converting repository completion into a fabricated operational PASS.

The production SSH deployment workflow has the same boundary: before creating
an archive or contacting the production host, it fetches the public release for
the requested exact tag and verifies the `p4-go-live.json` asset's GitHub
SHA-256/size, published non-prerelease state, PASS status and repository,
version and commit identity. The sanitized verification result is retained for
90 days with the deployment run. Missing, changed, draft, prerelease or
identity-mismatched evidence blocks rollout.
