# Task 094 — WooCommerce Connector Contract v1

## Scope

WooCommerce is a storefront provider behind Connector SDK v1. The frozen root `Connector` and `Runtime` interfaces do not change. Task 094 adds provider-neutral capability-specific interfaces for commerce writes, returns and verified commerce webhooks.

## Account and credential boundary

- each account is bound to one validated HTTPS WooCommerce store host and optional safe path prefix;
- Consumer Key, Consumer Secret and webhook secret are obtained only through Task-021 `SecretAccessor` callbacks;
- credentials MUST NOT be encoded into URLs, query parameters, events, logs or durable connector payloads;
- host policy owns approval, Product Compliance and dry-run policy before invoking write interfaces.

## Read semantics

The provider exposes bounded reads for products/variations, prices, explicitly managed stock, orders and per-order refunds. Customer billing/shipping identity is intentionally excluded from the canonical order projection.

## Write semantics

- product creation requires a stable seller SKU;
- an ambiguous create is reconciled by SKU and exact requested state; it is never blindly repeated;
- price, managed inventory and order-status writes are desired-state operations with read-before/read-after reconciliation;
- if the effect of an ambiguous write cannot be proved, the provider returns `write_outcome_unknown`;
- unmanaged WooCommerce stock is unsupported rather than coerced into an invented quantity.

## Webhook semantics

- HMAC-SHA256 over the exact request body is verified in constant time;
- the host supplies the expected topic from the configured callback route and it MUST match the received topic;
- replay identity is derived from authenticated material and is claimed through a durable host deduplicator;
- verified payloads enter normal inbox/sync processing only after signature and replay checks pass; only a minimized resource/timestamp envelope is emitted, never the raw WooCommerce customer/order body.

## Deferred surfaces

Variable-product creation and attribute taxonomy mutation, coupon management, customer CRUD, order creation and remote webhook provisioning remain unqualified until reusable provider-neutral contracts are defined.
