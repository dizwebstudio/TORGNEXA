# Task 154 — Saleor storefront connector

Status: Repository implementation complete

## Problem

The Integrations surface had storefront connectors for WooCommerce, OpenCart,
PrestaShop, Shopify, Medusa, Shopware and Magento, but not Saleor — a
widely used headless, open-source commerce platform whose API is GraphQL
only, with no REST surface at all. This is architecturally the most
different storefront connector in this repository so far.

## Scope

- New `connectors/saleor` package: products/inventory/prices/orders/returns
  read, prices/inventory write, order cancellation write, and product
  update (SKU rename included — see Explicit exclusions for why *create*
  is still unsupported). Host-injected transport
  (`store_host`/`base_path`/`channel`/`warehouse` runtime config), the
  same self-hosted admin pattern WooCommerce/Medusa/Shopware/Magento
  already use, with `channel`/`warehouse` being Saleor's own slugs
  (Saleor is a genuinely multi-channel, multi-warehouse platform, unlike
  the single-currency/single-location model every prior storefront
  connector in this repository addresses).
- Authentication: a single opaque bearer App access token minted once at
  install/creation time (Dashboard > Apps & webhooks) — like Magento's
  Integration token, no runtime OAuth exchange, signing, or refresh is
  needed on this connector's side at all.
- Real GraphQL client (`saleorHTTP` in
  `internal/platform/builtinruntime/http.go`) built against Saleor's own
  published GraphQL schema and Django view/webhook source
  (github.com/saleor/saleor), not guessed: the exact `productVariants`/
  `orders` Relay cursor-connection shape, the `productVariant`/
  `productVariantUpdate`/`productVariantChannelListingUpdate`/
  `productVariantStocksUpdate`/`productChannelListingUpdate`/`orderCancel`
  mutations and their `errors: [XError!]!` payload shape, and — critically
  — Saleor's GraphQL error-handling model: unlike every REST connector in
  this repository, Saleor's GraphQL endpoint returns HTTP 200 for almost
  every outcome including auth/permission failures (verified directly
  against `saleor/graphql/views.py`), so this connector classifies errors
  from the top-level `errors[].extensions.exception.code` field (Saleor's
  own Python exception class name) instead of relying on HTTP status.
- Registry wiring (`ProductReader`/`ProductWriter`/`healthConnector`/
  `SupportsPriceWrite`), contracts entry, frontend `presentation.json` +
  logo (Saleor's own verified wordmark ink color `#161819`, taken directly
  from Saleor's official site-hosted logo SVG), and architecture governance
  registration.

## Acceptance criteria

- `go build/vet/test ./...` passes repository-wide.
- Frontend typecheck, `node --test`, and the connector-catalog generator
  pass.
- Saleor appears in the Integrations surface with correct brand colors and
  reuses the existing generic sync/registry machinery with no
  Saleor-specific branch outside `internal/platform/builtinruntime`.

## Explicit exclusions

- Product create (`RemoteID == ""`): Saleor requires a `productType`
  assignment to create a valid product (`ProductCreateInput.productType:
  ID!`, which defines the product's entire attribute schema), which
  `sdk.ProductWriteRequest` does not carry — defaulting it to some assumed
  product type would put a real, publicly visible product live with the
  wrong attribute set.
- Webhook receipt (`notifications.receive`): genuinely investigated, not
  assumed unsupported by precedent. Saleor's real signing mechanism (a
  detached JWS/RS256, verifiable via the store's own published
  `/.well-known/jwks.json`, requiring no separate shared secret) was traced
  through Saleor's own core source, then found to structurally exceed this
  Connector SDK's 256-byte webhook `Signature` field cap — an RS256
  detached JWS's base64url signature segment alone is already ~342
  characters. There is no way to carry it through this envelope without
  silent truncation.
- Order status transitions beyond cancellation: every other Saleor order
  status is a side effect of fulfillment/invoicing workflows with their own
  mutations, not a directly settable field.
- Variant modeling: every Saleor `ProductVariant.sku` is flattened to a
  single-variant `RemoteProduct` row, matching the precedent already set by
  WooCommerce/Shopware/Magento. `Title` reads/writes the shared parent
  `Product.name` (Saleor has no per-variant display name), so a Title or
  StatusRemoteID (publication) write through one variant's row is shared
  with every sibling variant under the same parent product — a real,
  documented side effect, not an oversight.
- Live-instance qualification: unverifiable without a real self-hosted
  Saleor deployment and App credential, same limitation already documented
  for SBP/Robokassa/Shopify/Medusa/Shopware/Magento.
- The Task-064 conformance suite's `sandbox_isolation` check could not be
  exercised in this repository's execution environment because it cannot
  create unprivileged Linux user namespaces (`unshare --user` returns
  `Operation not permitted`); this is an environment constraint shared by
  every connector's Task-029 sandbox probe, not a Saleor-specific gap.
  `docs/connectors/saleor/conformance-report.json` records the genuine
  12/13 result rather than a fabricated pass.
