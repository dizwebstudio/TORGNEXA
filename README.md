# TORGNEXA Codex Kit

Complete architecture, contracts, Codex instructions, skills and executable backlog for **TORGNEXA** — an open-source/self-hosted commerce & distribution operating platform.

TORGNEXA covers marketplace/classified/social channels, ERP, PIM/MDM, bidirectional synchronization, reconciliation, reporting/settlements, advertising/promotions, procurement/WMS, logistics/PUDO, Russian compliance, legal-party/product-compliance master data, enterprise IAM/SIEM/Cloud billing/security edge, external automation and agent workflows.

## Package contents

- `AGENTS.md` — repository-wide Codex rules.
- `CODEX.md` — operating model and task execution order.
- `HANDOFF.md` — handoff checklist for a new Codex session/team.
- `docs/` — architecture v1.0 and domain/platform specifications.
- `adr/` — architectural decisions.
- `contracts/` — OpenAPI/events/plugins/webhooks/privacy/ledger/AI/conformance contracts.
- `frontend/` — React/TypeScript/Vite shell using the generated TypeScript SDK and host-owned OIDC adapter.
- `.codex/skills/` — repo-local Codex skills (`SKILL.md`).
- `tasks/issues/` — atomic task cards numbered 001–110; 001–100 form the
  contiguous implemented baseline, with later settings work tracked per card.
- `tasks/milestones/` — dependency-aware milestones M0-M13.
- `prompts/` and `templates/` — repeatable implementation/review artifacts.
- Go scaffold, migrations, Docker Compose and CI baseline.

## Technology baseline

Go 1.26.x, PostgreSQL 18.x, Apache Kafka 4.3.x (KRaft), Valkey 9.1.x, ClickHouse 26.x, S3-compatible storage, Keycloak 26.7.x, React/TypeScript/Vite, OpenTelemetry/Prometheus/Grafana/Loki. n8n is an external integration; MCP/OpenClaw uses scoped APIs/tools.


## Community Docker quick start

The server-side Community stack is now reproducible from repository state:

```bash
make community-up
```

The command creates a private local `.env` if needed, validates deployment
policy, builds TORGNEXA application/frontend images and starts PostgreSQL,
Kafka, Valkey, ClickHouse, Garage S3, Keycloak, the canonical migration job,
API, worker, scheduler, MCP and the React frontend. All development host ports
bind to `127.0.0.1`.

```bash
make community-status
make community-down
```

The frontend lockfile is committed and used by the Community Compose build.
Compose runs the frontend container on the loopback interface; production
frontend publication remains disabled by the JavaScript supply-chain policy.
This is a local single-host artifact, not a production CDN/web-server topology. See
`docs/deployment/093-community-docker-deployment.md`.

## Architecture rule

**Core never knows provider names.** Marketplaces, storefronts (including WooCommerce, PrestaShop and OpenCart), classified/verticals, social channels, ERP, EDO, government, payment, logistics, PUDO and CRM providers are plugins/connectors using capability contracts and the conformance suite.

Architecture v1.0 is frozen in `docs/54-architecture-freeze-v1.md`.

## Start with Codex

```text
Read AGENTS.md, CODEX.md, HANDOFF.md, docs/00-product-scope.md,
docs/01-architecture.md, docs/03-module-boundaries.md and
 tasks/issues/001-bootstrap-go-platform.md.
Implement Task 001 only. Do not expand scope. Run repository checks and
report changed files, validation evidence, risks and follow-ups.
```

After shared contracts are stable, follow `tasks/EXECUTION_PLAN.md`. Do not parallelize provider connectors before the Connector SDK, schema contracts, plugin security and connector conformance gate are stable.
