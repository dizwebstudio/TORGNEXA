# AGENTS.md — TORGNEXA repository instructions

These instructions apply to the entire repository unless a deeper `AGENTS.md` overrides them.

## Mission

Build TORGNEXA as an API-first, event-driven, plugin-extensible commerce platform. Prefer correctness, auditability, compatibility and operability over cleverness.

## Required reading before changes

Read the smallest relevant set, but always include:
1. `docs/00-product-scope.md`
2. `docs/01-architecture.md`
3. `docs/03-module-boundaries.md`
4. relevant ADRs
5. relevant contract/schema files
6. the issue/task being implemented

## Architecture rules

- Start as a modular monolith in Go; do not create microservices without an ADR.
- PostgreSQL is the operational system of record.
- Kafka is the durable event platform. Business modules depend on the `EventBus` abstraction, never Kafka packages directly.
- Use Transactional Outbox for DB -> Kafka publication.
- Consumers must be idempotent and use inbox/deduplication for externally visible side effects.
- ClickHouse is for analytics/history, not transactional truth.
- Valkey is cache/lock/rate-limit state only; never the sole source of critical business state.
- External systems are accessed only through connector interfaces.
- Core code must not branch on platform names (`if platform == "ozon"` is forbidden). Use capabilities.
- Dangerous actions must pass policy/approval checks.
- No private signing keys, marketplace tokens or OAuth secrets in source code, logs, events or plaintext DB columns.
- n8n is an external integration. Do not embed or redistribute n8n as part of TORGNEXA without a separate license decision.
- OpenClaw interacts through MCP/API with scoped permissions; AI is never a privileged bypass.

## Go conventions

- Use standard library first.
- Keep domain packages free of infrastructure imports where practical.
- Errors must carry context but never secrets.
- Public APIs require comments.
- Prefer explicit structs over `map[string]any` for stable contracts.
- All timestamps are UTC in persistence/events; convert at edges.
- IDs: UUIDv7/ULID-compatible sortable identifiers. Do not assume DB sequences are globally meaningful.
- Money: integer minor units + currency code. Never float64.
- Quantities that may be fractional use decimal/fixed-point representation, never binary float.

## API rules

- REST API under `/api/v1`.
- OpenAPI is contract-first: update `contracts/openapi/torgnexa-v1.yaml` with API changes.
- Mutating endpoints require idempotency support where retry is expected.
- Pagination must be cursor-based for large datasets.
- Every request carries tenant/workspace context derived from auth, not trusted from arbitrary client payload.

## Event rules

- Event names: `<domain>.<entity>.<action>.vN`.
- Every event uses the canonical envelope in `contracts/events/event-envelope.schema.json`.
- Include `event_id`, `occurred_at`, `organization_id`, `workspace_id`, `correlation_id`, `causation_id`, `entity_type`, `entity_id`, `source`.
- Do not put secrets or unnecessary PII in events.
- Breaking event changes require a new version.

## Connector rules

- Every connector has a manifest and capability declaration.
- Every connector implements health and rate-limit handling.
- Prefer official documented APIs. Browser automation/scraping requires an explicit ADR and legal/ToS review.
- Connector write operations require dry-run support when the remote API permits validation/previews.
- All remote calls use timeouts, structured error mapping, retries only for retry-safe conditions, and jittered backoff.
- Store remote IDs in mapping tables; never overload internal IDs.

## Security

- Default deny.
- All service accounts and API keys are scoped.
- Write-sensitive operations must declare a risk class.
- Signing requests go through Signing Service; private keys never transit generic workers, n8n, MCP or plugins.
- Audit records for privileged operations are append-only at application level.
- Never log access tokens, Authorization headers, DataMatrix verification codes, certificate private material, customer secrets or full payment credentials.

## Testing requirements

For changed code, run at minimum:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
./scripts/check-contracts.sh
```

When database migrations change, also run migration static checks.
When contracts change, add/update contract tests or fixtures.
When connector behavior changes, add unit tests with deterministic mocked remote responses.

## Definition of Done

A task is done only when:
- implementation matches the issue acceptance criteria;
- tests cover success + important failure/idempotency cases;
- contracts/docs are updated;
- security implications were considered;
- no TODO is used to hide required scope;
- validation commands were run and results reported.

## Scope control

Do not implement adjacent features just because they are easy. If you discover required follow-up work, add it to `tasks/BACKLOG.md` or note it in the task output.

## Privacy and data-governance rules

- Classify new persisted/event/public fields; minimize PII and define retention for personal data.
- Do not copy production PII to test fixtures; use synthetic/anonymized fixtures.
- Deletion/retention changes must consider PostgreSQL, ClickHouse, S3, search, caches and derived exports.
- External content (reviews/messages/listings) is untrusted input, including when passed to AI.

## Financial/WMS rules

- Settlement and inventory history use append/ledger semantics; corrections are adjustment records, not silent rewrites.
- `available stock` is derived from ledger/reservation/quarantine/allocation state where WMS owns stock.
- Procurement recommendations record input snapshot and algorithm/version.

## Webhook rules

- Outbound webhooks are durable, tenant-scoped, signed and replay-resistant.
- Webhook endpoints are untrusted network destinations; enforce SSRF/egress controls.

## Compatibility and release rules

- Core pillar changes require ADR plus migration/compatibility/security/privacy impact.
- Verified connectors must pass the connector conformance suite.
- Public API/events/plugins follow `contracts/sdk/compatibility-policy.md`.
- Release candidates require supply-chain checks/SBOM/provenance and upgrade rehearsal appropriate to the change.
## Legal party / compliance rules

- ERP/EDO/MChD/payments/procurement must reference canonical LegalEntity/Counterparty/Contract data rather than duplicate identifiers.
- Product publication/sale writes must honor Product Compliance policy; connector code cannot bypass `block`/`approval_required`.
- FX conversion uses sourced immutable fixed-decimal rates and records the rate/source used.

## Upload / edge / SIEM rules

- Untrusted uploads remain quarantined until security policy releases them; archive/parser/malware checks are mandatory by class.
- Never trust forwarded client headers except from configured trusted proxies; edge controls supplement, never replace, application authn/authz.
- SIEM export is asynchronous from authoritative audit; sink failure must not break business commits and exported events must be minimized/redacted.

## Enterprise IAM / Cloud billing rules

- External LDAP/AD/SAML/SCIM/JIT identities map through explicit reviewed tenant/role rules; default deny.
- Cloud Billing is optional/commercial and separate from commerce payments; Community runtime must not depend on subscription availability.

