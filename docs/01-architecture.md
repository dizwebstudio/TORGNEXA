# Architecture

TORGNEXA starts as a modular monolith in Go with independent API, worker, scheduler and MCP processes. PostgreSQL is operational truth; Kafka is the durable event log/transport; ClickHouse stores analytics/history; Valkey is non-authoritative cache/lock/rate-limit state; S3-compatible storage holds media/evidence/import/export artifacts; Keycloak provides identity/OIDC.

## Major domains

Commerce: legal-party/counterparty master data, catalog/PIM, product compliance, offers, pricing/FX, inventory, orders/returns, procurement, WMS, fulfillment/PUDO, claims/customer service, growth/advertising/promotions and settlement ledger.

Distribution: marketplace/classified/vertical/social connectors, content/publication/campaign engines.

Integration: ERP, government/compliance including EGAIS, EDO/signing, payments/reference acquiring, logistics/reference carrier, SMS/notifications, durable webhooks, import/export and upload-security pipeline.

State correctness: Transactional Outbox -> Kafka, idempotent Inbox consumers, bidirectional Sync Engine, Reconciliation Engine and provider-authoritative status rules.

Workflow automation is a bounded declarative DAG on top of the same primitives:
PostgreSQL owns definitions, immutable versions, runs, leases and evidence;
EventBus/Inbox supplies event triggers; the existing scheduler owns time
dispatch; worker adapters are typed and capability/policy/approval gated. Raw
payloads, secrets and arbitrary code are never part of a workflow definition or
run state. See `adr/0116-workflow-automation-builder.md` and
`docs/55-workflow-automation.md`.

Returns and cancellations are separate tenant-scoped aggregates rather than
an overloaded order/payment status. PostgreSQL owns request state, line
allocations, inspection evidence and optimistic versions; state changes and
refund allocations publish through Transactional Outbox. The existing payment
refund aggregate remains the sole payment mutation path and represents
ambiguous remote outcomes as `unknown`/`manual_attention`. Inventory, fiscal,
settlement and carrier side effects remain capability/policy/approval-gated
runtime work. See `adr/0117-returns-cancellations-refunds.md` and
`docs/56-returns-cancellations-refunds.md`.

Cross-cutting: tenancy, Enterprise IAM federation/provisioning, RBAC/ABAC, approval workflow, privacy/data governance, audit/lineage/SIEM export, secrets, notification center, Cloud billing/entitlements, search, schema registry, connector conformance, plugin isolation/governance, security edge, SLO/observability, backup/DR and upgrade migrations.

## Extension rule

External providers are plugins implementing SDK ports and capability declarations. Core never branches on provider names. Architecture pillars are frozen in `docs/54-architecture-freeze-v1.md`.
