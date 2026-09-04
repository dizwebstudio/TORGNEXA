# TORGNEXA Community License Decision

Status: approved for repository release metadata on 2026-08-18.

TORGNEXA-owned Community core code in this repository is licensed under **Apache License 2.0** (`Apache-2.0`). The complete license text is in the top-level `LICENSE` file.

Third-party dependencies, generated artifacts, provider SDKs, examples, and integrations retain their own applicable license terms. In particular, the n8n integration remains a separate package and must not copy or embed n8n product source unless separately licensed.

On 2026-09-04, the release owner also approved distribution of the current pinned runtime
images containing `GPL-2.0-only` and `LGPL-2.1-or-later` components. This
approval applies to the exact image digests in
`supply-chain/release-artifacts.json`, including the reported `ICU` and
`bzip2-1.0.6` components. Trivy's `Public`, `Public Domain`, `public-domain`,
`bzip-2-1.0.6` and `GPLv3+` labels are accepted only through exact
normalization to the approved policy identifiers; any image digest or license
expression outside that set remains subject to the default-deny policy and a
new review.

Connector implementations must continue to comply with provider API terms and trademark requirements. This repository license decision does not waive dependency-license, vulnerability, provenance, signing, protected-branch, or release-topology qualification gates.

Repository release metadata may therefore set `public_release_ready: true`; publication still fails closed whenever any independent Task 065 / Task 080 / runtime qualification gate is not satisfied.
