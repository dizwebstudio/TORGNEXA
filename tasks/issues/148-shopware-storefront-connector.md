# Task 148 — Shopware 6 storefront connector

Status: Repository implementation complete

## Problem

The Integrations surface had storefront connectors for WooCommerce, OpenCart,
PrestaShop, Shopify and Medusa, but not Shopware 6 — a widely used
self-hosted (and cloud) e-commerce platform, especially in the EU market.

## Scope

- New `connectors/shopware` package: products/inventory/prices/orders/returns
  read, prices/inventory write, order cancellation write, and full product
  update including SKU changes (Shopware's `productNumber` is a single
  top-level field, unlike Shopify/Medusa's variant-nested SKU — see
  Explicit exclusions for why *create* is still unsupported). Host-injected
  transport (`store_host`/`base_path`/`store_currency` runtime config), the
  same self-hosted admin pattern WooCommerce/Medusa/OpenCart/PrestaShop
  already use.
- Authentication: Shopware's own "Integration" client id/secret, exchanged
  for a short-lived OAuth2 client_credentials bearer token by the connector
  itself (`POST /api/oauth/token`) — the same self-contained pattern already
  established by `connectors/gigachat`, not the host-owned browser OAuth
  runtime built for Shopify (Task 144/ADR-0108), since Shopware's grant is
  client_credentials against a per-merchant host, with no browser
  redirect involved. The access token and the resolved currency entity id
  (Shopware prices are keyed by a currency UUID, not an ISO code) are
  cached in-memory for the lifetime of one call.
- Real HTTP transport (`shopwareHTTP` in
  `internal/platform/builtinruntime/http.go`) using Shopware's actual Admin
  API shapes verified against Shopware's own published core source
  (github.com/shopware/shopware, `src/Core/Content/Product`,
  `src/Core/Checkout/Order`) and developer documentation, not guessed:
  the Criteria search API (`POST /api/search/{entity}`, 1-indexed
  page/limit/total pagination, `associations` for eager-loading
  `stateMachineState`/`lineItems`), the parent/child product variant model,
  and the two-hop `transactionCapture.transaction.orderId` nested filter
  path used to attribute refunds to an order.
- Registry wiring (`ProductReader`/`ProductWriter`/`healthConnector`/
  `SupportsPriceWrite`), contracts entry (upgraded a pre-existing `planned`
  placeholder row to `stage: ready`, `surface: integrations`), frontend
  `presentation.json` + logo (Shopware's own live site's
  `--color-brand-500`/`--color-brand-800` CSS custom properties), and
  architecture governance registration.

## Acceptance criteria

- `go build/vet/test ./...` passes repository-wide.
- Frontend typecheck, `node --test`, and the connector-catalog generator
  pass.
- Shopware appears in the Integrations surface with correct brand colors
  and reuses the existing generic sync/registry machinery with no
  Shopware-specific branch outside `internal/platform/builtinruntime`.

## Explicit exclusions

- Product create (`RemoteID == ""`): unlike Shopify/Medusa this is not a
  SKU-search reliability problem (Shopware's Criteria filter on
  `productNumber` is an exact, reliable match) — it is that Shopware
  requires a `taxId` and at least one price entry to create a valid
  product, neither of which `sdk.ProductWriteRequest` carries. Defaulting
  them (a placeholder tax/price) would put a real, publicly visible
  product live at the wrong price, which is worse than refusing outright.
  Returns a normalized `unsupported` error, not a faked success.
- Webhook receipt (`notifications.receive`): Shopware's core Webhook entity
  signs deliveries with its own dedicated secret (`shopware-shop-signature`
  header, HMAC-SHA256), a distinct credential from the Integration client
  id/secret this connector holds for Admin API access. This connector has
  no scoped way to read that separate webhook secret today.
- Order status transitions beyond cancellation: every other Shopware order
  state-machine transition depends on the order's current position in that
  state machine in ways a single generic `StatusRemoteID` write cannot
  safely encode; only the unambiguous `POST /api/_action/order/{id}/state/cancel`
  transition is supported.
- Cross-call token/currency caching: because a fresh `ConfigurationSource`
  closure is bound on every registry call (the self-hosted host varies per
  account, unlike gigachat's fixed host), this connector is constructed
  fresh per call like every other storefront connector, so the in-memory
  cache only pays off within one call's own sub-requests, not across
  separate top-level operations. A real, accepted efficiency trade-off,
  not a correctness issue.
- Live-instance qualification: unverifiable without a real self-hosted
  Shopware deployment and Integration credential, same limitation already
  documented for SBP/Robokassa/Shopify/Medusa.
- The Task-064 conformance suite's `sandbox_isolation` check could not be
  exercised in this repository's execution environment because it cannot
  create unprivileged Linux user namespaces (`unshare --user` returns
  `Operation not permitted`); this is an environment constraint shared by
  every connector's Task-029 sandbox probe, not a Shopware-specific gap.
  `docs/connectors/shopware/conformance-report.json` records the genuine
  12/13 result rather than a fabricated pass.
