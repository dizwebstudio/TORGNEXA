# Task 093: Community Docker Deployment

## Status
`repository-complete` — 2026-08-12.

## Objective
Provide a reproducible local/self-hosted Community deployment that starts the TORGNEXA server processes and their required infrastructure with one repository command, without weakening Tasks 065, 084, 092 or the post-audit security hardening.

## Dependencies
001, 003, 018, 021, 027, 049, 065, 066, 067, 080, 084, 088, 092

## Deliverables
- one multi-stage, non-root TORGNEXA application image definition for API/worker/scheduler/MCP, with identical build inputs and distinct entrypoints;
- Compose services for PostgreSQL, Kafka, Valkey, ClickHouse, S3-compatible object storage, Keycloak, migration job, API, worker, scheduler and MCP;
- idempotent Keycloak database bootstrap and realm import;
- checksum-verifying PostgreSQL migration runner using the canonical migration catalog;
- generated local secrets with `.env` mode `0600` and no repository default passwords;
- service health/dependency ordering, persistent volumes, loopback-only host port exposure and container capability hardening;
- executable `community-check`, `community-up`, `community-down`, and `community-status` targets.

## Acceptance
1. `make community-up` generates a local secret file if absent and is the canonical one-command bootstrap.
2. API, worker, scheduler and MCP use the same digest-pinned build definition and source revision with distinct entrypoints; all external runtime/build images are immutable digest-pinned.
3. Application processes cannot start before the migration job succeeds; migration SQL SHA-256 values must match `migrations/catalog.json`.
4. Keycloak uses a separate database role/database and imports the TORGNEXA realm with `admin`, `manager`, `operator`, and `viewer` roles.
5. S3 storage creates the configured bucket/key on first start without a checked-in credential.
6. All development host ports bind to `127.0.0.1`; no floating `:latest` container image is accepted by the repository deployment check.
7. Application containers are non-root, read-only, `cap_drop: ALL`, `no-new-privileges`, with bounded tmpfs where required.
8. At Task-093 completion the source-only frontend remained outside Compose.
   Later application work committed its lockfile and added a loopback-only local
   frontend container; production frontend publication remains disabled by the
   Task-065 JavaScript artifact policy.
9. Repository architecture, migration, Go, frontend/source, connector and deployment checks remain green.

## Operational note
This Compose topology is the Community single-host development/self-host baseline, not a claim of production HA. Production deployment still requires external TLS/edge, secret management, backup/PITR, restore/upgrade rehearsals, resource sizing and Task-065/080 hosted release evidence.

The 2026-09-01 local Docker production runtime qualification passed, including
API load and worker/Kafka/PostgreSQL recovery drills. It validates the
repository topology only; it is not production-host qualification.
