# Task 096: OpenCart Connector

## Status
`runtime-complete` — 2026-08-12; reference bridge extension packaged — 2026-08-28; order sync admission and tenant status mapping — 2026-08-30.

## Objective
Add a bidirectional OpenCart storefront connector without coupling TORGNEXA to admin sessions, direct remote database access, or unstable provider-specific Core branches.

## Dependencies
010, 013, 014, 017, 021, 025, 064, 082, 094

## Design decision
OpenCart 4.x has a native storefront API used heavily by order/checkout flows, but it is not a complete stable external catalog-management contract. TORGNEXA therefore binds to a narrow versioned OpenCart extension contract (`extension/torgnexa/api/*`) installed in the merchant shop. The extension owns translation to OpenCart internals; the TORGNEXA connector remains stable.

## Deliverables
- HTTPS store binding and callback-scoped bridge bearer token;
- versioned `v1` bridge contract for health, products, variants, orders and desired-state writes;
- product/SKU reconciliation, exact price/inventory/order-status writes and fail-closed ambiguous outcomes;
- bounded product/order reads with no customer PII projection;
- deterministic tests, bridge contract documentation and Task-064 conformance candidate.
- installable OpenCart 4.x reference extension with idempotency and
  customer-PII-free projections;

## Acceptance
1. Root Connector/Runtime SDK v1 interfaces are unchanged.
2. TORGNEXA never logs or persists the bridge bearer token.
3. Remote OpenCart database credentials are never required by TORGNEXA.
4. Product create reconciles by stable SKU before any retry.
5. Price/inventory/order-status writes are desired-state operations with read-after verification.
6. The bridge advertises API version `v1`; incompatible bridges fail health checks closed.
7. Customer billing/shipping identity is not part of canonical order projection.
8. Order reads and status writes are admitted only with an explicit complete
   tenant `order_statuses` map; the generic worker translates canonical states
   at the runtime composition boundary.

## Deferred deliberately
OpenCart option/variant authoring semantics, returns, webhook provisioning and a distributable marketplace-signed `.ocmod.zip` are separate follow-up packaging work. The connector contract already supports variant remote IDs supplied by a bridge implementation.

The repository now ships a distributable reference `.ocmod.zip` build script
for the bridge. Live qualification still requires a real non-production
OpenCart 4.x instance, an installed extension and a dedicated store token; the
package itself is not evidence of a partner production certification.
