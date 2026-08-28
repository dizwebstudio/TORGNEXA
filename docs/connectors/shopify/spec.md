# Shopify Connector Spec

Family: `storefront`. OAuth2 authorization-code, per-tenant host (`{shop}.myshopify.com`), Admin REST API for products/inventory/prices/orders/returns.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. The per-tenant OAuth host is resolved generically by the host-owned OAuth runtime's `HostParameter`/`HostSuffix` template mechanism, not by any Shopify-specific branch in the app layer. Pagination follows Shopify's cursor (`page_info`/Link header) contract, not page numbers. Two capabilities are deliberately unsupported rather than faked: product create (`RemoteID == ""`) — Shopify's REST API has no reliable cross-catalog SKU search, so the find-or-create-by-SKU idempotency pattern other storefront connectors use cannot be implemented safely — and webhook receipt — Shopify signs webhooks with the OAuth app's shared client secret, which the host-owned OAuth runtime does not currently expose to a per-account connector adapter.

Official documentation: https://shopify.dev/docs/api/admin-rest
