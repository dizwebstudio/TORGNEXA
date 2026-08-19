# Task 095: PrestaShop Connector

## Status
`repository-complete` — 2026-08-12.

## Objective
Add a provider-neutral PrestaShop storefront connector on the official native Webservice API, reusing Task-094 commerce read/write contracts without adding provider names to Core.

## Dependencies
010, 013, 014, 017, 021, 025, 064, 082, 094

## Deliverables
- exact HTTPS store/base-path binding and callback-scoped Webservice API key;
- bounded product/combination, price, stock-available and order projections;
- exact price, StockAvailable quantity and order-state writes with read-before/read-after reconciliation;
- multi-language and optional multi-shop context in account configuration;
- deterministic tests, provider capability audit, spec and Task-064 conformance candidate.

## Acceptance
1. Root Connector/Runtime SDK v1 interfaces are unchanged.
2. Webservice key remains inside SecretAccessor callback and is not placed in URLs.
3. JSON is used only for reads; mutation bodies use XML/PATCH because the native Webservice does not accept JSON input.
4. Stock quantity is sourced only from `stock_availables`, not deprecated product quantity fields.
5. Combination price is projected as base product price plus combination impact with exact decimal arithmetic.
6. Inventory/price/status writes reconcile state before declaring success; ambiguous effects fail closed as `write_outcome_unknown`.
7. Order status transition uses `order_histories` rather than overwriting arbitrary order fields.
8. Customer address/identity data is excluded from canonical order projection.

## Deferred deliberately
Product creation/update, specific-price/promotions, returns/credit slips, webhooks and PrestaShop-9 Admin API/OAuth2 migration are deferred until their provider-neutral semantics are separately qualified.
