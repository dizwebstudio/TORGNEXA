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

TORGNEXA has releaseable Go commands and four registered first-party runtime
image definitions for PostgreSQL, Kafka, Keycloak, and Valkey. The image
definitions and their GHCR publication targets are declared in
`supply-chain/release-artifacts.json`. A workflow-produced candidate is not a
shipped release image until its exact digest has passed the image, license,
SBOM, signing, provenance, and deployment qualification gates. TORGNEXA does
not re-sign unchanged third-party images as TORGNEXA artifacts.

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

The four reviewed Community runtime repositories
`ghcr.io/dizwebstudio/torgnexa-{postgres,kafka,keycloak,valkey}` are explicit
first-party entries in this repository allowlist. Their exact tag-plus-digest
references must still appear in `supply-chain/license-policy.json` and the
release inventory; approving a repository does not approve an unreviewed image.

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

Root gosec excludes nested module roots; every nested module is then scanned
once under its own module boundary. Govulncheck follows the same complete module
inventory. `tools/securitytools` contains no Go source package and is checked as
the pinned tool carrier; adding a source package there fails until the module is
added to both scan sets. This prevents both missing modules and duplicate root
plus nested findings.

Before a Trivy secret report is retained, each finding is classified. The only
permitted synthetic classification requires an exact checked-in tuple of target,
rule ID and SHA-256 of the raw match. Missing or duplicate registered fixtures
fail closed. Every other finding remains a `credential_candidate` and blocks the
gate; match/code/snippet fields are removed before the report leaves the private
scanner scratch directory.

Container vulnerability evidence combines an OS-package scan of the immutable
image with a language-package scan of its Syft SPDX SBOM. The two Trivy schema
version 2 reports are validated and merged into one image report. Because the
SBOM already identifies Java artifacts, this path does not download Trivy's
separate Java index database and therefore does not trade runner capacity for
language-package coverage.

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

Some OS package databases expose legacy distribution labels or compound
license metadata that is not parseable as SPDX. The checked-in policy may
approve those raw Trivy values only for exact digest-pinned container images.
Acceptance requires both an exact `approved_image_artifacts` match and an exact
`approved_trivy_license_expressions` match. The arrays are sorted, bounded and
validated; mutable image references, `UNKNOWN`, and `NOASSERTION` are forbidden.
Digest or scanner-expression drift therefore returns to the parsed default-deny
path instead of inheriting an approval from an older image.

Task 117 records the approved TORGNEXA-owned Community-core repository license as Apache-2.0. The top-level `LICENSE`, `LICENSE-DECISION.md`, and owned package metadata carry that decision and `supply-chain/release-artifacts.json` may set `public_release_ready:true`. This field removes only the repository-license blocker: dependency-license, vulnerability, provenance/signing, protected-hosting and deployment qualification gates remain independently fail-closed.

#### Keycloak candidate license review — 2026-09-12

The release owner's request to review and admit the new candidate metadata is
implemented by adding the following 15 exact raw values (149 -> 164 total).
No entry is added to `allowed_spdx`, and no license obligation or security
finding is waived. This is a technical provenance review under the owner's
existing decision to retain the runtime images, not a certification of all
redistribution obligations.

Reviewed subject:
`ghcr.io/dizwebstudio/torgnexa-keycloak:v0.21.14-build.34648191406.1@sha256:d1da8092eeb21a9c2e514700a16809506c9faa373994ccc1526f246fb6167abe`.
The retained Trivy 0.70.0 full-license reports for `linux/amd64` and
`linux/arm64` both contain 74 distinct raw values, including the same 15
additions. Package versions and origins were cross-checked against the
retained OS reports and Syft SPDX inventories. License texts were also read
from this exact image's amd64 filesystem in a read-only, network-disabled
container; the arm64 license files were not independently extracted.

Paths below are relative to `/usr/share/licenses` in the image. Compound RPM
labels are preserved verbatim, not claimed to be canonical SPDX expressions.

| Exact raw value | Observed package/version or file | Review basis |
| --- | --- | --- |
| `ASL 2.0` | `dbus-broker` 28-9.el9_8; `openssl-fips-provider` and `openssl-fips-provider-so` 3.0.7-11.el9_8 | RPM metadata; `dbus-broker/LICENSE` and shared `openssl-libs/LICENSE.txt` identify Apache 2.0. |
| `BSD and ISC` | `libevent` 2.1.12-8.el9_4 | `libevent/LICENSE` contains the BSD terms and the ISC-style arc4 notice. Retain the full bundled notices. |
| `BSD or GPLv2+` | `libpwquality` 1.4.4-8.el9 | `libpwquality/COPYING` explicitly offers alternatives. Select the BSD branch for this reviewed package; retain its three conditions. This records the raw-image choice, without adding a global SPDX OR rule. |
| `BSD with advertising` | `cyrus-sasl-lib` 2.1.27-22.el9 | `cyrus-sasl-lib/COPYING` requires the Carnegie Mellon acknowledgment and preservation of notices. Do not simplify this label to BSD-3-Clause. |
| `BSD-4-Clause-UC` | `krb5-libs` 1.21.1-10.el9_8, `krb5-libs/LICENSE` | Composite Kerberos notice contains the four-clause Berkeley acknowledgment text; retain it. |
| `Bitstream Vera and Public Domain` | `dejavu-sans-fonts` 2.37-18.el9 | `dejavu-sans-fonts/LICENSE` identifies Bitstream fonts and public-domain DejaVu changes; also retain the included Arev terms and font-name restrictions. |
| `GPLv2 and GPLv2+ and LGPLv2+ and BSD with advertising and Public Domain` | `util-linux` and `util-linux-core` 2.37.4-25.el9 | RPM aggregate metadata, supported by `util-linux/COPYING.*`. Preserve every component's terms; this is not an OR choice or a reduction to one permissive license. |
| `GPLv3+ and GFDL` | `gzip` 1.12-2.el9_8 | RPM metadata plus `gzip/COPYING` and `gzip/fdl-1.3.txt`; code and documentation terms remain applicable. |
| `LGPLv2+ and BSD and Public Domain` | `libxcrypt` 4.4.18-3.el9 | `libxcrypt/LICENSING` inventories LGPL-2.1-or-later, BSD and public-domain components. Keep the legacy RPM label without reinterpreting it as a different LGPL version. |
| `LicenseRef-C-Ares` | `krb5-libs/LICENSE` | Trivy loose-file classifier label (confidence approximately 0.978), not evidence of a separate c-ares package or a canonical SPDX identifier. Admit the observed label for the reviewed composite Kerberos notice, without creating a generic custom-license alias. |
| `OpenLDAP` | `openldap` 2.6.8-4.el9, `openldap/LICENSE`; also `krb5-libs/LICENSE` | Both contain OpenLDAP Public License 2.8 text; preserve its notices and license copy. |
| `OpenVision` | `krb5-libs/LICENSE` | Explicit OpenVision Kerberos administration notice permits source/object redistribution with its notices retained. |
| `RSA` | `krb5-libs/LICENSE` | Explicit MD4/MD5 notices, including identification and notice-retention terms; not a blanket approval of unrelated RSA products. |
| `bzip2` | `bzip2-libs` 1.0.8-11.el9, `bzip2-libs/LICENSE` | Bundled bzip2/libbzip2 1.0.8 notice permits redistribution subject to origin, modification and attribution conditions. Preserve this raw label; do not silently substitute a different version's license. |
| `pubkey` | `gpg-pubkey` 5a6340b3-6229229e and fd431d51-4ae0493b, architecture `None` | RPM public-key metadata for Red Hat auxiliary key 3 and release key 2, not executable software, a private key, or an unknown software license. |

External primary-source corroboration: the
[RHEL package manifest](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/package_manifest/repositories)
records legacy RPM license labels; the
[MIT Kerberos notices](https://web.mit.edu/kerberos/krb5-1.21/doc/mitK5license.html)
document its composite licenses. The
[RPM maintainers' keyring example](https://lists.rpm.org/pipermail/rpm-maint/2024-October/029436.html)
shows the `pubkey` metadata convention, and
[Red Hat's key verification documentation](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/security_hardening/verifying-rpm-packages-with-post-quantum-signatures_security-hardening)
identifies the two observed key versions. These corroborate origins; the
digest-bound local reports remain the candidate evidence.

Evidence directory (Git-ignored):
`qualification/evidence/runtime-candidate-scan-20260908.TuFPmS/`.
The original scan evidence is not rewritten by this policy review.

| Evidence file | SHA-256 |
| --- | --- |
| `keycloak_linux_amd64.license.json` | `c476a8b001c772e81a655914484d5d2010bbe41f5917667d22876cf7797b2684` |
| `keycloak_linux_arm64.license.json` | `19148d1d99e0c2600336277d32e2f96b6a9f3579c467c3c245ddea149a28aa44` |
| Image `krb5-libs/LICENSE` (amd64) | `0d5373486138cb176c063db98274b4c4ab6ef3518c4191360736384b780306c2` |
| Image `openldap/LICENSE` (amd64) | `310fe25c858a9515fc8c8d7d1f24a67c9496f84a91e0a0e41ea9975b1371e569` |
| Image `bzip2-libs/LICENSE` (amd64) | `c6dbbf828498be844a89eaa3b84adbab3199e342eb5cb2ed2f0d4ba7ec0f38a3` |

The current schema uses two separate allowlists: these raw values are accepted
for **every** exact image reference in `approved_image_artifacts`, not through
a package-specific or Keycloak-only rule. That existing boundary is unchanged;
new image references and unlisted raw values still fail closed. In particular,
`UNKNOWN` and `NOASSERTION` remain forbidden, and `pubkey` is not a global SPDX
approval. Revalidating saved reports proves license-policy compatibility only;
it does not replace a fresh complete release scan or authorize a release tag.

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

The latest complete local scan (2026-09-05) covered every configured runtime
image/platform entry and captured the full license inventory for the seven
unique pinned image digests. The release owner's exact-digest decision is now
represented by the bounded raw-expression policy described above. Vulnerability,
secret, SAST and misconfiguration findings remain independent fail-closed gates;
license approval does not waive them or change their thresholds.

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

## First-party runtime image publication

`.github/workflows/runtime-images.yml` bootstraps immutable multi-platform
runtime-image candidates in GHCR. It is manual-only, accepts a validated target
SemVer, and builds only from the repository default branch. The publication
matrix comes from the checked-in artifact inventory rather than user input.

The matrix publisher is the only non-release job permitted `packages: write`.
It runs behind the existing protected `production` environment and authenticates
with the run-scoped `GITHUB_TOKEN`; no persistent registry credential is stored
in repository secrets. Every action is commit-pinned, checkout credentials are
not persisted, and BuildKit attaches an SBOM and maximal provenance to each
`linux/amd64` and `linux/arm64` image index.

Candidates use a unique `v<version>-build.<run-id>.<attempt>` tag and the
workflow retains `runtime-images-<run-id>` containing every exact OCI digest.
The package must remain private while it is a candidate. Publication of the
candidate does not update Compose or qualify it for production. The release
owner must next:

1. copy the exact digest references into all applicable Compose services and
   `development_runtime` entries;
2. add digest-scoped license decisions without permitting `UNKNOWN` or
   `NOASSERTION`;
3. run the complete image gate for both declared platforms and bind its SBOM,
   scan, signature, provenance, and runtime evidence to those exact digests;
4. expose or deploy only the qualified digest references.

For private GHCR packages, the production deployment account is authenticated
once with a read-only `read:packages` PAT. That pull credential belongs on the
deployment host, not in the repository and not in the image-publication job.

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
