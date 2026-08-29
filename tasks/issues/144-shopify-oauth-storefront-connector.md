# Task 144 — Shopify OAuth storefront connector

Status: Repository implementation complete; Docker protocol qualification passed; external Dev Store qualification blocked

## Problem

The Integrations surface had storefront connectors for WooCommerce, OpenCart
and PrestaShop, but not Shopify — one of the most widely used e-commerce
platforms. Connecting it required a genuinely new architecture capability
first: every existing OAuth2 connector (VK, Bitrix24) uses a single fixed
authorization/token host declared in its manifest, but Shopify's OAuth host
is per-merchant (`{shop}.myshopify.com`), which the host-owned OAuth runtime
(Task 134) had no way to express.

## Scope

- Extend `sdk.OAuth2Configuration` with `HostParameter`/`HostSuffix`
  (template a tenant-specific host into `authorization_url`/`token_url` from
  the account's own non-secret runtime config, bounded to a declared
  suffix so a tenant's config can never redirect the flow to an arbitrary
  host), `ScopeSeparator` (Shopify's authorize endpoint wants comma-joined
  scopes, not RFC 6749's default space), and `ExtraTokenParams` (static
  manifest-declared token-exchange form fields, e.g. Shopify's
  `expiring=1` to get a rotatable refresh token). All four are additive and
  opt-in: every existing OAuth2 connector's manifest is byte-for-byte
  unchanged and behaves exactly as before.
- `connectorauth.ResolveOAuth2Host` performs the substitution;
  `internal/app/api/connector_accounts.go`'s `oauthStart`/`oauthCallback`
  call it only when `configuration.HostParameter != ""`, so the generic
  HTTP layer never branches on connector identity — it stays exactly as
  provider-agnostic as it was before this task.
- New `connectors/storefronts/shopify` package: products/inventory/prices/orders/returns
  read, prices/inventory/order-status write, and product update (not
  create — see Explicit exclusions). Real HTTP transport
  (`shopifyHTTP` in `internal/platform/builtinruntime/http.go`) using
  Shopify's actual Admin REST shapes (inline product variants, distinct
  `inventory_item_id` per variant, `page_info`/Link-header cursor
  pagination, `X-Shopify-Access-Token` header) verified against Shopify's
  own developer documentation, not guessed.
- Registry wiring (`ProductReader`/`ProductWriter`/`healthConnector`/
  `SupportsPriceWrite` in `registry.go`), contracts entry (`stage: ready`,
  `surface: integrations`, matching WooCommerce/OpenCart's existing
  pattern), frontend `presentation.json` + logo, and architecture
  governance registration.
- Along the way, fixed a real pre-existing bug this surfaced:
  `ProductWriteRequest.Validate()` hardcoded WooCommerce's own status
  vocabulary (draft/pending/private/publish) as if it were a universal
  enum, which made it structurally impossible for any connector with a
  different vocabulary (Shopify's active/archived/draft) to use
  `products.write` at all. Generalized to a bounded free-text check,
  matching the sibling `OrderStatusWriteRequest.StatusRemoteID`, which
  already worked this way.

## Acceptance criteria

- `go build/vet/test ./...` passes repository-wide.
- Frontend typecheck, `node --test`, and the connector-catalog generator
  pass.
- Shopify appears in the Integrations surface with correct brand colors,
  and its OAuth connect flow templates the tenant's shop domain into the
  authorize/token URLs without any Shopify-specific code in the shared
  HTTP OAuth layer.

## Explicit exclusions

- Product create (`RemoteID == ""`): Shopify's REST API has no reliable
  cross-catalog SKU search (only per-product listing or the GraphQL Admin
  API), so the find-or-create-by-SKU idempotency pattern
  WooCommerce/OpenCart use cannot be implemented safely here without
  risking duplicate products on a retried create. Returns a normalized
  `unsupported` error, not a faked success.
- Webhook receipt (`notifications.receive`): Shopify signs webhook
  deliveries with the OAuth app's client secret, shared across every
  merchant installation of the app, not a per-account credential. The
  host-owned OAuth runtime projects `UseSecret` down to only the current
  per-account access token, so this connector has no scoped way to reach
  that shared secret today. Returns a normalized `unsupported` error
  rather than skipping HMAC verification.
- Marking an order fulfilled: a distinct Shopify sub-resource requiring
  line-item detail this SDK's single `StatusRemoteID` write contract
  cannot carry; only cancel/close/reopen are supported.
- Live-merchant qualification: unverifiable without a real Shopify
  partner/development store and OAuth app, same limitation already
  documented for SBP/Robokassa.
- Docker qualification: Shopify has no official self-hosted Docker store, so
  the stateful protocol double in `docker-compose.shopify-test.yml` passed
  Admin REST API `2026-07` contract smoke on 2026-08-29, including reads,
  product/price/inventory writes, read-after-write and automatic cleanup.
- External Dev Store qualification remains blocked until a Shopify Partner/Dev
  Dashboard development store, installed app token, required scopes and a
  synthetic SKU are supplied. The protocol-double result is not a merchant
  store certification.
- The canonical Task-064 report is 13/13 PASS, including the shared
  `sandbox_isolation` check. Qualification status is tracked in
  `docs/connectors/shopify/live-qualification-status.json`.
