# TORGNEXA Community License Decision

Status: approved for repository release metadata on 2026-08-18.

TORGNEXA-owned Community core code in this repository is licensed under **Apache License 2.0** (`Apache-2.0`). The complete license text is in the top-level `LICENSE` file.

Third-party dependencies, generated artifacts, provider SDKs, examples, and integrations retain their own applicable license terms. In particular, the n8n integration remains a separate package and must not copy or embed n8n product source unless separately licensed.

On 2026-09-05, the release owner approved distribution of every package-license
expression reported by pinned Trivy 0.70.0 for the exact current runtime image
digests in `supply-chain/release-artifacts.json`. The complete local scan
covered all 23 configured image/platform entries, seven unique image digests
and 129 unique raw Trivy license expressions. This includes the distribution
metadata aliases and compound Fedora/RPM expressions that are not valid SPDX
syntax by themselves.

The approval is encoded in `supply-chain/license-policy.json` as the
intersection of `approved_image_artifacts` and
`approved_trivy_license_expressions`: both the report's exact `@sha256` image
reference and its exact raw expression must match. `UNKNOWN` and
`NOASSERTION` cannot be approved this way. A changed image digest, a newly
reported expression, or a non-image report falls back to the parsed SPDX
default-deny policy and requires a new decision.

Connector implementations must continue to comply with provider API terms and trademark requirements. This repository license decision does not waive dependency-license, vulnerability, provenance, signing, protected-branch, or release-topology qualification gates.

Repository release metadata may therefore set `public_release_ready: true`; publication still fails closed whenever any independent Task 065 / Task 080 / runtime qualification gate is not satisfied.
