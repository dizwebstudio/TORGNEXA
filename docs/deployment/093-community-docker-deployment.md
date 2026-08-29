# Task 093 — Community Docker deployment

## Quick start

Requirements: Docker Engine with Compose v2 and enough resources for PostgreSQL, Kafka, ClickHouse, Keycloak, ClamAV and the TORGNEXA processes.

```bash
make community-up
```

The first invocation creates `.env` with random local credentials and mode `0600`, validates the deployment policy, builds the shared TORGNEXA application image definition, initializes the databases/object store, applies all reviewed migrations, creates the canonical Kafka base/retry/DLQ topics, then starts API, worker, scheduler and MCP.

Before changing ports, OIDC, worker, ClamAV or notification settings, see the
[complete `.env` reference](environment-variables.md). It explains every
variable, valid formats, safe secret generation and recovery rules for an
existing installation.

Useful commands:

```bash
make community-status
make community-down
# Destructive reset, only when you intentionally want to delete local data:
docker compose --env-file .env down -v
```

Local endpoints after startup:

- API health: `http://127.0.0.1:8080/api/v1/health`
- Keycloak: `http://127.0.0.1:8081`
- MCP transport: `http://127.0.0.1:8090/mcp` (fail-closed until trusted IAM/governance runtime wiring is configured)
- PostgreSQL: `127.0.0.1:5432`
- Kafka: `127.0.0.1:9092`
- Valkey: `127.0.0.1:6379`
- ClickHouse HTTP/native: `127.0.0.1:8123` / `127.0.0.1:9000`
- S3-compatible Garage: `http://127.0.0.1:9002`

All host bindings are loopback-only. Containers use Docker DNS/internal ports (`postgres:5432`, `kafka:29092`, `garage:3900`, etc.). The backend bridge uses `TORGNEXA_DOCKER_NETWORK_MTU` (default `1376`) so HTTPS egress remains reliable on VPN/tunnel hosts whose path MTU is lower than Ethernet's 1500 bytes.

## Why Garage rather than a legacy MinIO image

The Community deployment needs a maintained, redistributable S3-compatible container. MinIO Community distribution moved to source-only and its historical prebuilt images are no longer an appropriate default for a security-hardened one-command install. Garage v2.3.0 provides a stable open-source container and single-node/default-bucket bootstrap while preserving TORGNEXA's vendor-neutral S3 contract.

## Secret handling

`.env.example` intentionally contains no usable secrets. `make community-up` calls `scripts/init-community-env.sh` when `.env` is absent. Keep the generated `.env` while persistent volumes exist: rotating database/object-store credentials independently from stored state can make the local deployment inaccessible.

For explicit initialization before startup use `make community-init`. Do not
create `.env` by copying the example with blank secrets.

For production use an external secret manager/Docker secrets/Kubernetes secrets rather than Compose environment variables.

## Database migration flow

`migrate` is a one-shot PostgreSQL client container. It:

1. waits for PostgreSQL health;
2. verifies the deployment TSV matches the canonical migration catalog in repository checks;
3. verifies every migration SQL SHA-256 at runtime;
4. detects untracked partial bootstrap state;
5. seeds migration history for bootstrap migrations 1–3;
6. passes reviewed metadata GUCs to every atomic migration;
7. verifies the final applied history count.

Application services depend on `service_completed_successfully`; a failed migration therefore blocks application startup.

## Keycloak

A one-shot `keycloak-db-init` creates a dedicated Keycloak role/database. Keycloak imports `deploy/keycloak/torgnexa-realm.json`. For local browser checks use the synthetic account `demo` / `demo-local-only`; `make community-up` reconciles both the Keycloak account and its administrator membership in the configured development workspace, including when an existing volume predates the realm import. The bundled setup uses `start-dev` deliberately because this is the single-host Community baseline. Public production deployment must use an optimized TLS-enabled Keycloak topology and external edge.

Community Compose includes ClamAV at the internal address `clamav:3310`, so
product image uploads are enabled by default and remain behind the quarantine
and release pipeline.

## Frontend

The JavaScript lockfile is now committed. Community Compose includes
the read-only, capability-dropped frontend development container and exposes it
only on the configured loopback port. This supports the local application and
Task-110 public documentation workflow; it is not a production static-hosting or
CDN topology. `supply-chain/js-artifacts.json` still has frontend release
publication disabled, so the local Compose artifact does not bypass Task-065
release policy or change the single-host/non-HA qualification of Task 093.

### Авторизованный браузерный smoke-тест

После запуска стека выполните `make community-e2e`. Перед тестом команда
идемпотентно восстанавливает пользователя Keycloak `demo` и его активное
членство в development workspace. Затем чистый профиль Chrome проходит
настоящий authorization-code вход, идемпотентно запускает заполнение
синтетического демо-контура и проверяет в интерфейсе каталог, главное
изображение карточки, вкладку изображений и миниатюры товара в списке и
деталях заказов. Пользовательские товары и заказы не изменяются; тест не
сохраняет профиль, токены или cookies. При сбое диагностический снимок
записывается в `/tmp`, а не в репозиторий. Можно указать `CHROME_BIN`,
`TORGNEXA_E2E_BASE_URL` и `TORGNEXA_E2E_KEYCLOAK_URL` для нестандартных
локальных адресов.

## Production boundary

This Compose file is not an HA topology. Before public production deployment repeat backup/restore and upgrade rehearsals, supply-chain/signing qualification, hosted architecture rules, provider staging qualification, TLS/WAF/DDoS design, secret management and capacity/SLO tests against the actual topology.

For a single small VPS, use the separate production overlay and its protected
manual rollout workflow: [`production-ssh-deploy.md`](production-ssh-deploy.md).
Do not change this Community file's development mode to make it production.

## Image reproducibility

Every external Compose image and the Go builder image is pinned by a human-readable version plus a multi-platform SHA-256 digest. API, worker, scheduler and MCP share the same Dockerfile, build arguments and repository revision; Compose may assign service-local names to the identical build output rather than treating a mutable local tag as an external runtime dependency. This preserves Task-065's distinction between imported images and locally built release artifacts.
