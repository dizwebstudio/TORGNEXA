# TORGNEXA Community Deployment Contract v1

Task 093 defines the single-host Community runtime composition. It changes no commerce, Connector SDK, public REST, event or database schema contract.

## Required services
The default Compose project contains PostgreSQL, Kafka, Valkey, ClickHouse, Garage S3, Keycloak, a Keycloak DB bootstrap job, the canonical PostgreSQL migration job, and TORGNEXA API/worker/scheduler/MCP processes.

## Startup contract
`postgres → {keycloak-db-init,migrate}`; infrastructure health gates precede application processes; `migrate` must complete successfully before API/worker/scheduler/MCP. Failure is fail-closed: application services are not permitted to start on a failed schema upgrade.

## Migration integrity
`deploy/postgres/catalog.tsv` is generated from `migrations/catalog.json`. The deployment check verifies exact row parity and every SQL SHA-256. Runtime migration verifies every file digest again and validates already-applied migration checksums before skipping them. Bootstrap history is seeded only after the migration framework exists; unknown/partial history fails closed.

## Identity contract
Community Keycloak uses a dedicated `keycloak` database/database role. Realm `torgnexa` is imported with the canonical baseline roles `admin`, `manager`, `operator`, `viewer`, a PKCE public web client and bearer-only API resource client. Application authorization remains owned by Task 084/secure API composition; realm roles do not bypass application authorization.

## Object-storage contract
The deployment uses S3-compatible Garage v2.3.0 in single-node mode. S3 credentials, Garage RPC/admin/metrics secrets and bucket name are supplied through generated local environment state. Object storage is a deployment adapter; application code remains S3-vendor neutral.

## Security contract
No default credential is committed. `.env.example` contains empty secret values; `scripts/init-community-env.sh` generates mode-0600 credentials. Host ports are loopback-only. TORGNEXA application containers run UID/GID 10001, read-only, with all Linux capabilities dropped and `no-new-privileges`. Data services retain only the capabilities required by their upstream image.

## Scope
This contract is for a single-host Community/dev deployment. It does not qualify production high availability, public TLS termination, immutable backup/PITR, external KMS/secrets, DDoS/WAF, provider credentials or public release signing.
