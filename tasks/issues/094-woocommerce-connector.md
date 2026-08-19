# Task 094: WooCommerce Connector

## Status
`repository-complete` — 2026-08-12.

## Objective
Add a conformance-qualified bidirectional WooCommerce storefront connector on the current official WP REST API v3 without introducing provider-specific Core branches or weakening Tasks 010, 013, 014, 017, 021, 025, 064, 082, 092.

## Dependencies
010, 013, 014, 017, 021, 025, 064, 082, 092

## Deliverables
- provider `woocommerce` with exact HTTPS store binding and callback-scoped Consumer Key/Secret;
- products/variations, prices, managed inventory, orders and refunds read projections;
- additive provider-neutral commerce write interfaces for product, price, inventory and order-status operations;
- ambiguity reconciliation and fail-closed `write_outcome_unknown` semantics;
- HMAC-SHA256 WooCommerce webhook receiver with expected-topic binding and durable replay dedup boundary;
- official API capability audit, spec/reconciliation docs, deterministic tests and Task-064 report.

## Acceptance
1. Root Connector/Runtime SDK v1 interfaces are unchanged; write/return/webhook surfaces are additive and provider-neutral.
2. Credentials never appear in request URLs and only exist inside SecretAccessor callbacks.
3. Reads support simple/variable products, exact prices, explicit managed stock, orders and per-order refunds with bounded cursors.
4. Product create requires stable SKU and reconciles ambiguous POST results by SKU rather than blindly retrying.
5. Price/inventory/order-status writes are exact-state operations and reconcile ambiguous outcomes before returning success.
6. Unmanaged Woo stock fails as unsupported rather than inventing quantity.
7. Webhooks verify HMAC-SHA256, require host-known expected topic, and deduplicate on signed material rather than trusting mutable delivery headers.
8. No customer billing/shipping PII is projected into the Connector SDK order model or verified webhook canonical payload.
9. Task-064 provider conformance is 13/13 and architecture/repository regression remains green.

## Deferred deliberately
Variable-product creation/attribute taxonomy mapping, coupon management, customer CRUD, order creation and webhook provisioning are not admitted until reusable provider-neutral semantics are qualified.
