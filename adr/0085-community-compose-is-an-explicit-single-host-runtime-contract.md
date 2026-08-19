# ADR 0085: Community Compose is an explicit single-host runtime contract

## Status
Accepted for Task 093.

## Context
TORGNEXA had a Compose file for data infrastructure but no reproducible application deployment. API/worker/scheduler/MCP were started manually, migrations were not an ordered one-shot service, OIDC/object storage were absent, and Kafka advertised `localhost`, which did not provide a valid broker address to application containers.

A one-command Community installation must not weaken the post-audit security composition or JavaScript release rules. It also must not use stale object-storage binaries solely to preserve an earlier implementation assumption.

## Decision
The repository owns one multi-stage TORGNEXA image definition containing four statically built process binaries plus a static health probe. Compose launches distinct roles from identical build inputs; locally built outputs are not modeled as imported runtime dependencies. Canonical schema upgrades remain a separate PostgreSQL client job and gate all application service starts.

PostgreSQL, Kafka, Valkey and ClickHouse remain the baseline data services. Keycloak 26.7 is the local OIDC provider with a dedicated database identity. S3-compatible storage is Garage v2.3.0 in single-node mode; S3 remains vendor-neutral to application code.

Community credentials are generated locally and never committed. Host ports bind only to loopback. The application image is non-root/read-only/capability-free. The React frontend remains outside the release Compose until its locked JavaScript graph is qualified.

## Alternatives considered
Starting Go processes manually was rejected because it does not exercise a deployable dependency graph. Running migrations inside API startup was rejected because schema changes need a separate observable/fail-closed lifecycle. Sharing the TORGNEXA database role with Keycloak was rejected due to privilege coupling. Historical MinIO Community images were rejected because current Community distribution no longer provides maintained prebuilt containers. Shipping an unlocked frontend was rejected by ADR 0084.

## Compatibility impact
No public API, Connector SDK, event schema or database schema changes. Existing localhost infrastructure ports are retained where possible; S3 moves to host port `9002` to avoid ClickHouse native port `9000`.

## Migration and data impact
No new SQL migration. The deployment job applies the existing reviewed catalog and checks exact SHA-256 metadata. Persistent Compose volumes are additive local deployment state.

## Security and privacy impact
The composition reduces exposed attack surface through loopback-only host bindings, generated credentials, separate Keycloak DB identity, scratch-based non-root/read-only application containers, dropped Linux capabilities, immutable digest-pinned external images and fail-closed migration ordering. Compose environment secrets are acceptable only for this local Community baseline; public production must use external secret management.

## Operational impact
`make community-up` becomes the canonical local deployment command. Single-node Garage and Kafka/ClickHouse/PostgreSQL instances do not provide HA and must not be described as production topology.

## Consequences
TORGNEXA can now be bootstrapped as a coherent server-side Community stack from repository state, while production/release evidence remains separately gated by Tasks 027, 065, 066, 080 and 092.
