# Task 146 — Medusa storefront connector

Status: Repository implementation complete; live qualification blocked

## Problem

The Integrations surface had storefront connectors for WooCommerce, OpenCart,
PrestaShop and Shopify, but not Medusa — a widely used open-source,
self-hosted headless commerce engine.

## Scope

- New `connectors/storefronts/medusa` package: products/inventory/prices/orders/returns
  read, prices/inventory write, order cancellation write, and product update
  (not create — see Explicit exclusions). Host-injected transport
  (`store_host`/`base_path`/`store_currency` runtime config), the same
  self-hosted admin pattern WooCommerce/OpenCart/PrestaShop already use — no
  OAuth or per-tenant host template needed (unlike Task 144's Shopify work),
  since a self-hosted merchant configures their own host directly.
- Real HTTP transport (`medusaHTTP` in
  `internal/platform/builtinruntime/http.go`) using Medusa v2's actual Admin
  REST shapes verified against Medusa's own published OpenAPI spec
  (github.com/medusajs/medusa, `www/apps/api-reference/specs/admin`), not
  guessed: inline product variants, offset/limit/count pagination (the count
  lives in the response body, not a header, unlike WooCommerce/Shopify),
  decimal major-unit price amounts (not cents, parsed via `json.Number` to
  avoid float rounding), and `Authorization: Basic <token>` carrying the raw
  admin secret key as-is — Medusa's own documented wire format for its
  single-secret admin API key, not RFC 7617 user:pass Basic auth.
- Registry wiring (`ProductReader`/`ProductWriter`/`healthConnector`/
  `SupportsPriceWrite`), contracts entry (upgraded a pre-existing `planned`
  placeholder row to `stage: ready`, `surface: integrations`, matching
  WooCommerce/OpenCart's pattern), frontend `presentation.json` + logo
  (Medusa's own documented UI color tokens: near-black surface, the
  documented "interactive" blue as accent — no single saturated brand color
  the way Shopify has one), and architecture governance registration.

## Acceptance criteria

- `go build/vet/test ./...` passes repository-wide.
- Frontend typecheck, `node --test`, and the connector-catalog generator
  pass.
- Medusa appears in the Integrations surface with correct brand colors and
  reuses the existing generic sync/registry machinery with no
  Medusa-specific branch outside `internal/platform/builtinruntime`.

## Explicit exclusions

- Product create (`RemoteID == ""`): Medusa's REST API has no reliable
  catalog-wide exact-match SKU lookup (only per-inventory-item SKU
  filtering, which does not establish whether a *product* with that SKU
  already exists), so the find-or-create-by-SKU idempotency pattern
  WooCommerce/OpenCart use cannot be implemented safely here without
  risking duplicate products on a retried create. Returns a normalized
  `unsupported` error, not a faked success.
- Webhook receipt (`notifications.receive`): Medusa has no standardized,
  discoverable outbound webhook delivery/signature contract — Medusa's own
  documentation describes reacting to events via in-process subscribers,
  not signed HTTP callbacks to a third party. Returns a normalized
  `unsupported` error rather than accepting an unverifiable delivery.
- Order status transitions beyond cancellation: Medusa models fulfillment,
  payment capture, etc. as distinct sub-resources with their own request
  shapes, not a settable `order.status` field; only the one unambiguous
  single-call transition (`POST /admin/orders/{id}/cancel`) is supported.
- Live-instance qualification requires a real self-hosted Medusa v2 deployment
  and secret API key. The executable gate is `scripts/medusa-smoke.sh`,
  documented in `docs/connectors/medusa/docker-live-qualification.md`.
- The canonical Task-064 report records 13/13 SDK checks. This deterministic
  suite is not a live-store qualification. The repository DTC Starter Docker
  smoke passed on 2026-08-29; external live status remains blocked until a
  separate non-production endpoint is supplied and passes the same smoke.
