# Architecture

TORGNEXA starts as a modular monolith in Go with independent API, worker, scheduler and MCP processes. PostgreSQL is operational truth; Kafka is the durable event log/transport; ClickHouse stores analytics/history; Valkey is non-authoritative cache/lock/rate-limit state; S3-compatible storage holds media/evidence/import/export artifacts; Keycloak provides identity/OIDC.

## Major domains

Commerce: legal-party/counterparty master data, catalog/PIM, product compliance, offers, pricing/FX, inventory, orders/returns, procurement, WMS, fulfillment/PUDO, claims/customer service, growth/advertising/promotions and settlement ledger.

Distribution: marketplace/classified/vertical/social connectors, content/publication/campaign engines.

Integration: ERP, government/compliance including EGAIS, EDO/signing, payments/reference acquiring, logistics/reference carrier, SMS/notifications, durable webhooks, import/export and upload-security pipeline.

State correctness: Transactional Outbox -> Kafka, idempotent Inbox consumers, bidirectional Sync Engine, Reconciliation Engine and provider-authoritative status rules.

Cross-cutting: tenancy, Enterprise IAM federation/provisioning, RBAC/ABAC, approval workflow, privacy/data governance, audit/lineage/SIEM export, secrets, notification center, Cloud billing/entitlements, search, schema registry, connector conformance, plugin isolation/governance, security edge, SLO/observability, backup/DR and upgrade migrations.

## Extension rule

External providers are plugins implementing SDK ports and capability declarations. Core never branches on provider names. Architecture pillars are frozen in `docs/54-architecture-freeze-v1.md`.
