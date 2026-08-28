# Medusa Connector Spec

Family: `storefront`. Self-hosted (host-injected, admin-supplied like WooCommerce/OpenCart/PrestaShop), single opaque secret admin API key, Medusa v2 Admin REST API.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. Authentication follows Medusa's own published OpenAPI security scheme literally: `Authorization: Basic <token>` with the raw secret key as the token, not RFC 7617 user:pass Basic auth. Pagination follows Medusa's offset/limit/count contract (the count lives in the response body, not a header). Prices are Medusa's own decimal major-unit numbers (not cents), parsed via `encoding/json.Number` to avoid float rounding. Two capabilities are deliberately unsupported rather than faked: product create (`RemoteID == ""`) — no reliable catalog-wide SKU lookup exists to make find-or-create idempotent — and webhook receipt — Medusa has no standardized, discoverable outbound webhook/HMAC contract to verify against.

Official documentation: https://docs.medusajs.com/api/admin ; verified against the published OpenAPI spec at github.com/medusajs/medusa (www/apps/api-reference/specs/admin).
