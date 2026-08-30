# Module Boundaries

## Core domains
Tenancy/identity, legal-party/counterparty, catalog/PIM, product compliance, offers/pricing/FX, inventory, orders, cancellations/returns/refund allocations, fulfillment, content/campaigns, procurement, WMS, customer service/claims, settlement ledger, Cloud billing, reporting, workflow/approval, audit/lineage, privacy metadata.

## Integration domains
Connector runtime, sync/reconciliation, import/export/upload security, ERP, compliance/government/EGAIS, EDO/signing, payments/reference acquiring, logistics/PUDO/reference carrier, social, SMS notifications, durable webhooks.

## Platform domains
EventBus/outbox/inbox, secrets, storage, search, authn/authz/federation/provisioning, notifications, SIEM export, schema registry, plugin isolation/governance, security edge, observability, migration framework, conformance harness.

## Dependency direction
Domain packages depend on ports/shared public types. The architecture policy explicitly allowlists `internal/platform/domain` as a shared primitive package for Core values such as exact money and currency; it is not an infrastructure adapter. Infrastructure implements ports. Provider connector packages import only the Connector SDK (`internal/platform/connectors`); host-side SDK runtime adapters bridge approved secrets/HTTP/event capabilities. Providers never import Core, PostgreSQL, Kafka, or concrete secret internals.

The machine-readable package inventory is `architecture/policy.json`.
`make architecture` validates every Go file, including tests and files behind
build tags: Core cannot import Platform/App/provider implementations; Platform
cannot import App/provider implementations; provider adapters cannot import
Core, App, `database/sql`, PostgreSQL internals, direct network packages, host
filesystem/environment packages, or process/plugin/syscall/unsafe escape
hatches. New package directories are unregistered until the policy and a
complete gap review add them.

## Connector package layout

Built-in providers are grouped by the `family` declared in their manifest and
live under one category directory: `connectors/<category>/<provider>` (for
example, `connectors/marketplaces/ozon`, `connectors/storefronts/woocommerce`
and `connectors/ai/claude`). The provider directory remains the package and
policy boundary; the category is organizational only and must not be imported
as a Go package. Generators, architecture checks and lifecycle inventory scan
this single category level recursively. Provider documentation keeps the
stable `docs/connectors/<provider>` path.

## Frozen rule
Core must not gain provider-specific branches. Adding a provider is a plugin/connector task unless an ADR demonstrates a missing generic capability and includes migration/compatibility/security impact.

Tasks 010, 025, 029, and 064 now establish the required SDK, isolation, dry-run/runtime, and conformance gates. Provider admission remains fail-closed in the Task-064 completion change until a later protected architecture change whose merge base already contains all four completed prerequisites. Provider plus generic Core/SDK changes are classified as mixed and require both provider evidence and a new/superseding ADR.

## Isolated plugin boundary

Task 025 adds `internal/platform/pluginsecurity` as the host-side, non-executing
verification boundary for future third-party plugins. It verifies signed artifact
identity and least-privilege permission grants and emits only an inert admission
plan. Task 029 is the first task allowed to bind that plan to an isolated runtime.
