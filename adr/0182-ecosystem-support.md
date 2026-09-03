# ADR-0182: provider-neutral ecosystem and support control plane

Status: Accepted

## Context

TORGNEXA already has separate owners for connector readiness, plugin admission,
customer service, mobile warehouse work, Cloud billing and SLO/DR. A customer
still needs one honest view of which integrations, apps, partners, mobile
surfaces and hosted tiers are usable. A manifest count, a health-check or a
marketplace listing cannot prove that a business operation is ready, qualified
or supported.

Task 231 also covers commercial and partner processes. Those processes must not
create a second connector registry, CRM, settlement ledger, subscription ledger
or mobile backend. The new surface therefore acts as a bounded projection and
evidence register over the existing modules.

## Decision

Add the `internal/core/ecosystem` domain package and a tenant-scoped PostgreSQL
adapter for the small amount of customer-owned evidence that does not belong to
another bounded context:

- an integration/app/partner portfolio with statuses
  `integrated`, `verified`, `ready`, `qualified`, `supported`, `deprecated` and
  `blocked`;
- onboarding runs with bounded checks and fail-closed evaluation;
- partner certification records with expiring, redacted evidence;
- mobile, hosted-tier and support policy projections;
- outcome metrics and explicit external release-gates.

Promotion is sequential and evidence-backed. `qualified` accepts only exact
credentialed sandbox/live evidence; `supported` requires explicit support
evidence and owner/response policy. No API, SDK, MCP tool or frontend count can
promote a resource. Community/self-hosted surfaces never receive a hosted SLA
claim without retained topology and DR evidence.

The application exposes additive tenant-scoped routes under
`/api/v1/ecosystem`, generated Go/Python/TypeScript SDK methods, the read-only
MCP tool `commerce.ecosystem.overview` and the `/ecosystem` operator workspace.
Onboarding and certification writes require authenticated scope,
`Idempotency-Key`, audit capture and append-only persistence. Existing
`pluginmarketplace`, `connector readiness`, `customerservice`, `cloudbilling`,
mobile WMS and n8n contracts remain authoritative for their own state.

## Consequences

The operator can see portfolio coverage, onboarding blockers, partner evidence,
support load, mobile delivery and hosted readiness in one place. Metrics are
labelled as observed or qualification-required; they never substitute for
remote proof. The database migration is additive, forced-RLS and append-only.
Events and evidence contain bounded digests and references only, never tokens,
private keys, raw provider payloads or unnecessary personal data.

Partner and hosted production claims remain disabled until the external gates
listed in the operations runbook pass. The repository can prove contracts,
tenant isolation, redaction, idempotency, frontend/API/SDK wiring and synthetic
state transitions, but it cannot manufacture third-party credentials, hardware
coverage or production topology evidence.

## Compatibility impact

Migration `000058_ecosystem_support.sql` adds only the onboarding and partner
evidence tables. It does not alter Product, connector, support, settlement,
subscription, WMS or customer ledgers. The OpenAPI and event changes are
additive and generated SDK artifacts must be regenerated whenever the contract
changes.

## Migration and data impact

Migration `000058_ecosystem_support.sql` is additive, forced-RLS and append-only;
existing business ledgers and connector account data are not rewritten.

## Security and privacy impact

All reads and writes use the authenticated organization/workspace scope and
FORCE RLS. Audit is required before privileged writes. Evidence is digest-only
and has bounded references and expiry. App/partner permissions continue to be
enforced by host policy and SecretProvider; this projection cannot grant raw
tenant secrets, private keys, arbitrary network access or policy bypass.

## Operational impact

`make ecosystem-support-qualification` runs the repository synthetic gate.
Credentialed connector qualification, partner sandbox-to-production UAT and
rollback, hosted SLO/SLA topology and DR drill, mobile device/printer matrix,
and production backup/restore remain release gates owned by the relevant
operators.

## Alternatives considered

Making the plugin marketplace the source of all ecosystem state was rejected:
it cannot own tenant onboarding, partner certification or operational support
evidence. Adding commercial and support fields to connector manifests was
rejected because manifests describe packages, not runtime truth. Declaring all
ready connectors supported was rejected because support and SLA require an
owner, policy and retained evidence.
