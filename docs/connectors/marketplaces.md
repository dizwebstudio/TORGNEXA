# Marketplace Connectors

This page is the entry point for marketplace integrations shipped in the
repository. It records repository state as of **2026-08-28**. Connector
manifests remain the authoritative source for admitted capabilities; provider
specifications and audits explain how those capabilities map to the remote API.

## Current coverage

| Connector | Admitted capabilities | Remote mutation | Repository evidence |
|---|---|---|---|
| [Wildberries](wildberries/README.md) | `inventory.read`, `products.read` | none | [audit](wildberries/capability-audit.md), [spec](wildberries/spec.md), [conformance](wildberries/conformance-report.json) |
| [Ozon](ozon/README.md) | `inventory.read`, `products.read` | none | [audit](ozon/capability-audit.md), [spec](ozon/spec.md), [conformance](ozon/conformance-report.json) |
| [Yandex Market](yandex-market/README.md) | `inventory.read`, `notifications.receive`, `orders.read`, `prices.read`, `prices.write`, `products.read` | exact price updates only | [audit](yandex-market/capability-audit.md), [spec](yandex-market/spec.md), [conformance](yandex-market/conformance-report.json), [reconciliation](yandex-market/reconciliation.md) |
| [Megamarket](megamarket/README.md) | `inventory.read`, `orders.read`, `products.read` | none | [audit](megamarket/capability-audit.md), [spec](megamarket/spec.md), [conformance](megamarket/conformance-report.json), [reconciliation](megamarket/reconciliation.md) |
| [Magnit Market](magnit-market/README.md) | `inventory.read`, `orders.read`, `prices.read`, `products.read` | none | [audit](magnit-market/capability-audit.md), [spec](magnit-market/spec.md), [conformance](magnit-market/conformance-report.json), [reconciliation](magnit-market/reconciliation.md) |
| [AliExpress RU](aliexpress-ru/README.md) | `products.read` | none | [audit](aliexpress-ru/capability-audit.md), [spec](aliexpress-ru/spec.md), [conformance](aliexpress-ru/conformance-report.json), [reconciliation](aliexpress-ru/reconciliation.md) |
| Lamoda | `inventory.read`, `orders.read`, `prices.read`, `products.read` (SDK contract) | health-check only | [audit](lamoda/capability-audit.md), [spec](lamoda/spec.md), [conformance](lamoda/conformance-report.json) |
| М.Видео | `inventory.read`, `orders.read`, `products.read` (SDK contract) | health-check only | [audit](mvideo/capability-audit.md), [spec](mvideo/spec.md), [conformance](mvideo/conformance-report.json) |

Each linked conformance report currently records a passing 13/13 repository
suite for connector version 1.0.0 and SDK v1. That result proves the bounded,
deterministic repository contract; it does not replace credentialed smoke tests
or qualification on the exact production account and release topology.

## Common account and synchronization lifecycle

1. The host creates a tenant-bound connector account. Non-secret provider
   identifiers live in account configuration; credentials are referenced
   through the secret store and are disclosed only for the remote-call callback.
2. Health checks verify authentication and the exact configured business,
   campaign, shop or warehouse scope before synchronization is enabled.
3. Initial and scheduled reads normalize provider data into canonical records.
   Remote identifiers stay in mapping tables rather than replacing TORGNEXA IDs.
4. Cursors, checkpoints and Inbox deduplication make retries resumable and keep
   externally visible processing idempotent.
5. Reconciliation compares canonical desired state with provider-authoritative
   remote state and records drift. Notifications are hints to read fresh state,
   not an independent source of transactional truth.
6. A remote mutation is dispatched only when the manifest admits the capability
   and the host has passed authorization, policy, risk, approval, audit and
   idempotency checks required for that operation.

## Architecture and security invariants

- Core and domain modules depend on connector capabilities, never provider
  names. Provider-specific request and response shapes stay inside the adapter.
- All remote calls use host-injected transport with bounded bodies, timeouts,
  structured errors, rate-limit handling and jittered retry for retry-safe
  conditions only.
- Tokens, API keys and authorization headers are never stored in manifests,
  logs, events or connector account plaintext fields.
- Unsupported capabilities are absent and fail closed. The existence of a
  provider endpoint is not sufficient reason to advertise a capability.
- Browser automation and scraping require a separate ADR and legal/ToS review;
  they are not fallback behavior for these connectors.
- Provider APIs and limits are volatile. Re-audit the provider spec, capability
  declaration and deterministic fixtures whenever remote behavior changes.

## Adding or expanding a marketplace connector

Start with `templates/connector-spec.md`, declare the minimum required
capabilities in the manifest, and add deterministic remote-response tests. A
verified connector must pass `contracts/conformance/connector-conformance.yaml`.
Lamoda and М.Видео are deliberately health-only in the current runtime. The
operator supplies the current HTTPS partner endpoint and credentials; a green
probe proves connectivity for that account only. It does not enable product,
price, stock or order synchronization.
Any write expansion also needs canonical desired-state semantics, retry and
idempotency rules, reconciliation behavior, risk classification and audit
evidence before the capability is admitted.
