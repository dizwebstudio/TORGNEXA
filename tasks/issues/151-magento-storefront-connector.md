# Task 151 — Magento (Adobe Commerce open-source) storefront connector

Status: Repository implementation complete

## Problem

The Integrations surface had storefront connectors for WooCommerce,
OpenCart, PrestaShop, Shopify, Medusa and Shopware, but not Magento 2 —
one of the most widely deployed self-hosted open-source e-commerce
platforms, with a large installed base among mid-market merchants.

## Scope

- New `connectors/magento` package: products/inventory/prices/orders/returns
  read, prices/inventory write, order cancellation write, and product update
  (excluding SKU rename — see Explicit exclusions). Host-injected transport
  (`store_host`/`base_path`/`store_currency` runtime config), the same
  self-hosted admin pattern WooCommerce/Medusa/Shopware/OpenCart/PrestaShop
  already use.
- Authentication: a single opaque bearer access token minted once by a
  Magento Admin "Integration" and activated — unlike Shopware's
  client_credentials exchange, Magento issues one long-lived token used
  directly as a bearer credential on ordinary REST calls; no runtime OAuth
  signing, exchange or refresh logic is needed on this connector's side at
  all.
- Real HTTP transport (`magentoHTTP` in
  `internal/platform/builtinruntime/http.go`) using Magento's actual REST
  API shapes verified against Magento's own published core source
  (github.com/magento/magento2) and developer documentation, not guessed:
  the `searchCriteria` bracket-notation query protocol (`searchCriteria[currentPage]`,
  `searchCriteria[pageSize]`, `searchCriteria[filter_groups][N][filters][0][field/value/condition_type]`),
  the documented pagination quirk where requesting a page past the last one
  returns the last valid page instead of an empty result (guarded
  proactively in `cursor.go` rather than trusted as an end-of-list signal),
  the legacy CatalogInventory `stockItems` API for a single synthetic
  location (not MSI multi-source inventory), and Creditmemo-based returns.
- Registry wiring (`ProductReader`/`ProductWriter`/`healthConnector`/
  `SupportsPriceWrite`), contracts entry (upgraded a pre-existing `planned`
  placeholder row to `stage: ready`, `surface: integrations`), frontend
  `presentation.json` + logo (Magento's official brand orange `#EE672F`,
  verified against Magento's own published brand assets), and architecture
  governance registration.

## Acceptance criteria

- `go build/vet/test ./...` passes repository-wide.
- Frontend typecheck, `node --test`, and the connector-catalog generator
  pass.
- Magento appears in the Integrations surface with correct brand colors
  and reuses the existing generic sync/registry machinery with no
  Magento-specific branch outside `internal/platform/builtinruntime`.

## Explicit exclusions

- Product create (`RemoteID == ""`): Magento requires `attribute_set_id`
  and other remote-only mandatory fields to create a valid product, none of
  which `sdk.ProductWriteRequest` carries. Defaulting them (a placeholder
  attribute set) would put a real, publicly visible product live
  incorrectly, which is worse than refusing outright. Returns a normalized
  `unsupported` error, not a faked success.
- SKU rename on update (`RemoteID != SellerSKU`): Magento's REST API does
  support renaming a SKU via the same `PUT /products/{sku}` call, but this
  connector's RemoteID *is* the SKU (see `ReadProducts`), and its
  write/verify/reconcile-after-ambiguous-failure path always re-fetches by
  RemoteID — which would stop resolving the instant a rename succeeded.
  Rejecting the request outright is safer than a verification path that
  silently stops working after its first successful use.
- Webhook receipt (`notifications.receive`): Magento's open-source core has
  no built-in outbound webhook delivery mechanism or signature scheme at
  all (Adobe Commerce Cloud's separate Adobe I/O Events product is not part
  of the Admin REST API and is not available to a self-hosted open-source
  install).
- Order status transitions beyond cancellation: every other Magento order
  state transition depends on the order's current position in Magento's
  order/invoice/shipment state machine in ways a single generic
  `StatusRemoteID` write cannot safely encode; only the unambiguous
  `POST /orders/{id}/cancel` transition is supported.
- Variant modeling: every Magento SKU is flattened to a single-variant
  `RemoteProduct` (RemoteID = SellerSKU = SKU); this connector does not
  model Magento's configurable-parent/simple-child product relationship,
  matching the precedent already set by WooCommerce and Shopware.
- Live-instance qualification: unverifiable without a real self-hosted
  Magento deployment and Integration credential, same limitation already
  documented for SBP/Robokassa/Shopify/Medusa/Shopware.
- The Task-064 conformance suite's `sandbox_isolation` check could not be
  exercised in this repository's execution environment because it cannot
  create unprivileged Linux user namespaces (`unshare --user` returns
  `Operation not permitted`); this is an environment constraint shared by
  every connector's Task-029 sandbox probe, not a Magento-specific gap.
  `docs/connectors/magento/conformance-report.json` records the genuine
  12/13 result rather than a fabricated pass.
