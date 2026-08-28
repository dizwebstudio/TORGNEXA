# Production SSH deployment

`.github/workflows/production-deploy.yml` is the manual, gated production
deployment path for a single VPS. It deploys an exact immutable `vX.Y.Z` tag over
SSH, combines the repository's base Compose file with its production overlay,
keeps the previous release for rollback and waits for the configured API health
endpoint.

The workflow does **not** use the repository `docker-compose.yml`: that file is
the loopback-only Community/development baseline and must never be promoted to a
public production service.

## GitHub Environment setup

Create a protected GitHub Environment named `production` and require at least one
reviewer. Add these environment secrets:

| Secret | Value |
|---|---|
| `DEPLOY_HOST` | DNS name or IPv4 address of the deployment host. |
| `DEPLOY_PORT` | SSH port, from `1` to `65535`. |
| `DEPLOY_USER` | Dedicated non-root Unix account with only the permissions needed to run the deployment. |
| `DEPLOY_SSH_KEY` | Private OpenSSH key for that account. It is read only by the workflow runner and never written into the release archive. |
| `DEPLOY_KNOWN_HOSTS` | Pinned `known_hosts` line(s), including `[host]:port` when the SSH port is not 22. Do not generate this with an unverified `ssh-keyscan` in the workflow. |

The screenshot-provided four secrets are necessary but not sufficient: the
host-key pin is required to prevent a man-in-the-middle deployment.

Recommended environment variables (not secrets):

| Variable | Default | Meaning |
|---|---|---|
| `DEPLOY_PATH` | `/opt/torgnexa` | Absolute directory owned by the deployment account. |
| `DEPLOY_COMPOSE_FILE` | `docker-compose.production.yml` | Relative path to the production Compose overlay shipped in the release tag. `..` and absolute paths are rejected. |
| `DEPLOY_HEALTH_URL` | `http://127.0.0.1:8080/api/v1/health` | HTTP(S) endpoint reachable from the deployment host. |

## Production application values

The application `.env` on the VPS reuses the credentials documented in the
[complete environment reference](environment-variables.md), but production
must additionally define these values:

| Variable | Example | Requirement |
|---|---|---|
| `KEYCLOAK_HOSTNAME` | `auth.example.ru` | Public HTTPS hostname used by Keycloak behind the host reverse proxy. |
| `TORGNEXA_OIDC_ISSUER` | `https://auth.example.ru/realms/torgnexa` | HTTPS issuer URL matching the realm and browser configuration. |
| `TORGNEXA_OIDC_USERINFO_URL` | `https://auth.example.ru/realms/torgnexa/protocol/openid-connect/userinfo` | HTTPS userinfo URL reachable by the API. |
| `TORGNEXA_SECURITY_TRUSTED_PROXY_CIDRS` | `127.0.0.1/32` | Only the address range of the trusted local reverse proxy. |
| `TORGNEXA_SECURITY_ADMIN_CIDRS` | `127.0.0.1/32` | Explicit admin-edge allowlist. |
| `TORGNEXA_SECURITY_ALLOWED_ORIGINS` | `https://app.example.ru` | Exact public frontend origin, without a trailing slash. |
| `TORGNEXA_WORKER_UPLOADS_ENABLED` | `false` | Keep disabled until an accessible ClamAV service is configured. |

`TORGNEXA_ENV=production` and `TORGNEXA_HTTP_ADDR=0.0.0.0:8080` are applied by
the production overlay. Do not copy the Community `.env` blindly: its OIDC URLs,
development tenant IDs and loopback policy are not valid for a public service.

After the first Keycloak start, update the `torgnexa-web` client in the
`torgnexa` realm: replace the localhost redirect URIs and web origins from the
bundled development realm with the exact public frontend origin, for example
`https://app.example.ru/*` and `https://app.example.ru`. Realm JSON imports are
not a substitute for reviewing the live production client after bootstrap.

## Host prerequisites

Before the first run, the host must already contain:

1. `$DEPLOY_PATH/.env` with mode `0600`, external production credentials and
   `TORGNEXA_ENV=production` wired into the effective Compose configuration;
2. the release tag's `docker-compose.yml` plus
   `$DEPLOY_COMPOSE_FILE`, reviewed for the actual production topology
   (TLS/edge, external PostgreSQL/Kafka/Valkey/ClickHouse/S3 and Keycloak as
   applicable), not the Community file by itself;
3. Docker Engine with Compose v2 and `curl` available to `DEPLOY_USER`;
4. a tested backup/restore and rollback procedure.

The host TLS edge should route the public frontend origin to
`127.0.0.1:${TORGNEXA_FRONTEND_PORT:-5173}`, `/api/` to
`127.0.0.1:${TORGNEXA_API_PORT:-8080}` and the Keycloak hostname to
`127.0.0.1:${KEYCLOAK_PORT:-8081}`. Keep database, Kafka, Valkey, ClickHouse,
Garage and MCP ports private; the base file binds them to loopback only.

The workflow renders the merged Compose configuration without printing it, and
refuses the deployment unless the effective `TORGNEXA_ENV` is `production` and
both API and worker services are present. It uploads the checked-out tag to
`$DEPLOY_PATH/releases/$VERSION`, atomically switches `$DEPLOY_PATH/current`,
then runs `docker compose ... up -d --build`. A failed startup or health check
switches `current` back to the previous release and attempts to restart it.

## Running a deployment

1. Complete the release-candidate workflow for the tag and verify its evidence.
2. Open **Actions → production-deploy → Run workflow** on the exact tag.
3. Enter the version without `v` (for example `0.2.0`) and set the confirmation
   checkbox. Environment reviewers must approve the protected job.
4. Confirm the API health result and the service logs on the host.

There is no automatic deploy on push. A deployment is still subject to the
release, supply-chain, migration, connector and hosted-production qualification
requirements described in the release and Community deployment documents.
