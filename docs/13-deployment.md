# Deployment

Community/dev: Docker Compose with API/worker/scheduler/MCP, PostgreSQL, single Kafka KRaft node, Valkey; optional Keycloak/ClickHouse; external or local S3-compatible storage.

Production: Kubernetes/Helm target, independently scalable stateless processes, PostgreSQL HA, Kafka HA separated roles, ClickHouse cluster, external S3, Keycloak HA or enterprise OIDC.

Configuration/secrets are external. Upgrades version DB/API/events/plugins independently and use explicit migrations.

## Process configuration

All recognized `TORGNEXA_` environment values are strictly validated and fail closed when explicitly empty or invalid. Unknown variables remain ignored so a shared deployment environment can contain settings for later modules. Common settings are `ENV`, `LOG_LEVEL`, `LOG_FORMAT`, `LOG_ADD_SOURCE`, and `SHUTDOWN_TIMEOUT`. The API additionally supports `HTTP_ADDR`, `HTTP_READ_HEADER_TIMEOUT`, `HTTP_READ_TIMEOUT`, `HTTP_WRITE_TIMEOUT`, `HTTP_IDLE_TIMEOUT`, and `HTTP_MAX_HEADER_BYTES`.

Development defaults bind the API to `127.0.0.1:8080`. A container or production deployment must explicitly set `TORGNEXA_HTTP_ADDR` (for example, `:8080`) and place the listener behind the controls specified in `docs/66-security-edge-baseline.md`. Logs default to structured JSON and redact attributes whose keys identify credentials or signing material.

`SIGINT` and `SIGTERM` initiate bounded graceful shutdown. The API stops accepting new connections and drains active requests for at most `TORGNEXA_SHUTDOWN_TIMEOUT`; the common process supervisor enforces the same outer bound for every runner. A second signal uses the operating system's default termination behavior.
