# TORGNEXA

[Русская версия](README.ru.md)

TORGNEXA is an open-source, self-hosted, API-first commerce and distribution
platform. It unifies operations across marketplaces, storefronts, social
channels, ERP, PIM/MDM, inventory, orders, fulfillment, finance, compliance
and automation.

## What is included

- provider-neutral commerce and distribution core;
- marketplace, storefront, classified, social, ERP, payment, logistics and
  compliance connectors;
- catalog/PIM, pricing, inventory, orders, returns, procurement, WMS,
  fulfillment and settlement workflows;
- REST API, OpenAPI contracts, signed webhooks, MCP/OpenClaw and n8n
  integration boundaries;
- tenant-scoped governance, approvals, audit/lineage, secrets, privacy,
  upload security, IAM and SIEM foundations;
- React/TypeScript frontend, Go services, migrations, Docker Compose and CI
  validation tooling.

## Repository map

- `docs/` — architecture and domain/platform documentation;
- `adr/` — architectural decisions;
- `contracts/` — OpenAPI, event, plugin, webhook and JSON Schema contracts;
- `connectors/` — SDK-based provider implementations grouped by category;
- `internal/` — core, platform and application packages;
- `frontend/` — React/TypeScript/Vite application;
- `tasks/` — scoped implementation tasks and execution plans;
- `scripts/`, `deploy/`, `docker-compose*.yml` — development and deployment
  tooling.

## Architecture

TORGNEXA starts as a modular monolith in Go. PostgreSQL is the operational
system of record; Kafka is the durable event platform; ClickHouse stores
analytics/history; Valkey is limited to cache, lock and rate-limit state; and
S3-compatible storage holds media and evidence artifacts.

Core code does not branch on provider names. Connectors implement SDK ports and
capability declarations, while host-side runtime adapters own network access,
secret callbacks, policy checks and bounded retries. Architecture v1 is frozen
in [`docs/54-architecture-freeze-v1.md`](docs/54-architecture-freeze-v1.md).

## Community quick start

Requirements: Docker with Compose v2 and a local checkout.

```bash
make community-init
make community-up
make community-status
```

The Community stack runs locally on loopback and includes PostgreSQL, Kafka,
Valkey, ClickHouse, S3-compatible storage, ClamAV, Keycloak, the API, worker,
scheduler, MCP service and frontend. Local demo access uses the synthetic
Keycloak account `demo` / `demo-local-only`.

For a full browser smoke test, start the stack and run:

```bash
make community-e2e
```

See [`docs/deployment/environment-variables.md`](docs/deployment/environment-variables.md)
for configuration and safe secret rotation. Do not use `.env.example` as a
production configuration. Production deployment guidance is in
[`docs/deployment/093-community-docker-deployment.md`](docs/deployment/093-community-docker-deployment.md)
and [`docs/deployment/production-ssh-deploy.md`](docs/deployment/production-ssh-deploy.md).

## Validation and evidence

The tracked [`VALIDATION_REPORT.md`](VALIDATION_REPORT.md) is a concise
repository validation summary. Run-specific runtime and release evidence is
generated into `qualification/evidence/` or retained as CI artifacts; those
outputs are deliberately not committed as source files.

## Development

Read [`docs/00-product-scope.md`](docs/00-product-scope.md),
[`docs/01-architecture.md`](docs/01-architecture.md),
[`docs/03-module-boundaries.md`](docs/03-module-boundaries.md) and the relevant
task card before changing the repository. Public API changes are contract-first
under `contracts/openapi/`; mutating operations must preserve tenant scope,
idempotency, auditability and security policy.

The standard repository checks are:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
./scripts/check-contracts.sh
```

## License

TORGNEXA is distributed under the Apache License 2.0. See [`LICENSE`](LICENSE).
