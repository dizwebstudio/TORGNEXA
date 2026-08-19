# Version Baseline — 2026-08-08

| Component | Baseline | Notes |
|---|---:|---|
| Go | 1.26.5 | minimum language version 1.26.0; exact toolchain pinned in `go.mod` |
| PostgreSQL | 18.4 | readable tag plus immutable OCI index digest in the release inventory |
| Apache Kafka | 4.3.1 | supported bugfix release, KRaft |
| Valkey | 9.1.1 | current 9.1 bugfix |
| ClickHouse | 26.6 | immutable OCI index digest in the release inventory |
| Keycloak | 26.7.x | current 26.7 line |
| n8n | external | do not package as runtime without license review |
| Syft | 1.50.0 | pinned SPDX SBOM generator; archive and binary SHA-256 verified |
| Trivy | 0.70.0 | pinned vulnerability, secret, license, misconfiguration and image scanner |
| Cosign | 3.1.3 | pinned Sigstore bundle signer/verifier |
| govulncheck | 1.6.0 | pinned Go reachability-aware vulnerability scanner |
| gosec | 2.28.0 | pinned Go SAST scanner |
| GitHub Actions | exact commits | versions and full commit pins live in `supply-chain/action-pins.json` |

Downloaded tool hashes and runtime image digests live in the checked-in
`supply-chain/` manifests and are validated by the offline policy gate. Version
or digest upgrades are separate PRs with release-note review, migration and
rollback notes.
