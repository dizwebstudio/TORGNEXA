# ADR 0087: PrestaShop native Webservice storefront provider

## Status
Accepted for Task 095.

## Context
TORGNEXA needs a PrestaShop storefront provider without adding provider-specific branches to Core. The commerce contracts introduced by Task 094 already cover canonical product, price, inventory, order and desired-state write semantics. PrestaShop exposes products and combinations separately, keeps sellable quantity in the StockAvailable resource, and uses its Webservice resource model for compatibility-oriented external integrations.

## Decision
Admit PrestaShop as marketplace-family provider `prestashop` and reuse the additive Task-094 commerce interfaces without changing root Connector or Runtime SDK interfaces. Bind every account to an exact HTTPS store host/base path, language and optional shop identifier. API keys remain callback-scoped Task-021 secret material.

Products and combinations are projected to canonical products/variants; product base price plus combination price impact is calculated exactly. Inventory is read and written only through StockAvailable. Bounded order and order-detail reads project commerce orders without customer billing/shipping PII. Price and inventory mutations use XML resource PATCH bodies and order-state transitions use order-history creation, followed by read-after reconciliation. Ambiguous mutations fail closed as `write_outcome_unknown` when desired state cannot be proven.

Product creation, promotion authoring, returns and webhooks are intentionally not claimed by v1. A future PrestaShop Admin API transport may be added behind the same provider-neutral semantics rather than changing Core.

## Alternatives considered
Direct database integration was rejected because it couples TORGNEXA to merchant schema/version details and creates an unnecessary credential and integrity boundary. Provider-specific Core models were rejected because Task 094 already established reusable commerce contracts. Treating product quantity as authoritative was rejected in favor of StockAvailable. Blind mutation retries were rejected because ambiguous external writes must be reconciled before success or retry.

## Compatibility impact
Root Connector SDK v1 interfaces remain unchanged. PrestaShop implements only additive commerce capability interfaces already admitted by Task 094. Existing providers require no changes.

## Migration and data impact
No PostgreSQL schema migration is required. Remote product, combination, stock and order identities use existing Task-010 mapping/reconciliation primitives. No customer address, email or payment details are added to canonical order projections.

## Security and privacy impact
The connector accepts a bounded store host/base path rather than arbitrary per-request URLs. API keys are resolved only through SecretAccessor and are never persisted in account configuration. TLS is mandatory. Customer billing/shipping PII is deliberately excluded from the canonical read model.

## Operational impact
Operators must configure an API key with the minimum required resource permissions, store currency, language and optional shop identifier. Stores with custom modules or nonstandard product semantics may require a later explicit adapter rather than implicit field guessing. Release qualification must rerun Task-064 conformance in the canonical Linux isolation environment.

## Consequences
TORGNEXA gains a native PrestaShop storefront adapter while keeping Core frozen. The provider shares the WooCommerce commerce surface and establishes a reusable pattern for additional merchant-owned storefronts.
