# Task 114 — P0 runtime closure

## Goal

Close the four P0 gaps discovered after Task 113: production connector source bridges, executable reconciliation actions, Inbox/idempotent Kafka consumption, and the frontend typecheck gate.

## Scope

- Production runtime registry for Wildberries, Ozon, Yandex Market, 1C, МойСклад and WooCommerce product reconciliation reads, composed through the policy-pinned `internal/platform/builtinruntime` boundary rather than provider imports in App/Core.
- DNS-pinned HTTPS provider transports at the application boundary; connector/domain layers remain transport-neutral.
- Versioned tenant-scoped non-secret runtime configuration API/storage for configuration-bearing providers; credentials stay in `SecretProvider`.
- Reconciliation `ActionExecutor` with deterministic idempotency evidence: safe mapping repair, remote-authoritative catalog title/status repair, WooCommerce product write repair, notifications and approval requests.
- Kafka event handling through canonical `inbox_receipts`, with webhook enqueue and inbox receipt committed atomically in the inbox-owned PostgreSQL transaction before Kafka offset commit.
- Frontend repository React Query shim updated for `onError`/`onSettled` so the official frontend shell gate matches production usage.
- OpenAPI/public SDK regeneration for runtime-config provisioning.

## Safety decisions

- ADR-0090 keeps App/Core and ordinary Platform packages provider-neutral; only the exact policy-declared built-in composition module may import active registered provider implementations.

- Marketplace product status is not invented when the SDK projection does not expose it; no synthetic status drift is generated.
- Connectors without `products.write` are never reported as auto-fixed. Local-authoritative drift is notified instead of entering an impossible approval/retry loop.
- Provider hosts are HTTPS-only, DNS-pinned per request, public-address-only and redirect-disabled.
- Runtime config rejects secret-bearing keys recursively and is forced-RLS scoped.

## Done when

1. Frontend shell validation is green.
2. Runtime registry resolves the six priority connectors for product reconciliation.
3. Reconciliation engine is created with a non-nil production ActionExecutor.
4. Kafka delivery passes through `inboxrepo.Processor` before webhook projection and Kafka commit.
5. Migration 000068 and architecture/OpenAPI/SDK inventories are synchronized.
6. Targeted tests and repository policy gates pass in the available build environment.
7. Architecture validation proves the built-in composition boundary accepts registered providers and rejects unregistered imports.
