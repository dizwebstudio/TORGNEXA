# ADR 0088: OpenCart versioned bridge provider

## Status
Accepted for Task 096.

## Context
TORGNEXA needs bidirectional OpenCart product, price, inventory and order semantics. OpenCart's storefront API surface is tied closely to store-side checkout/order workflows and version-specific controllers, while TORGNEXA requires a stable external integration contract for catalog and desired-state writes. Automating administrator sessions or granting remote database credentials would create brittle and unsafe coupling.

## Decision
Admit OpenCart as marketplace-family provider `opencart` through a shop-local, versioned TORGNEXA extension API rooted at `extension/torgnexa/api/*`. The OpenCart extension owns translation between version-specific OpenCart models and the stable bridge-v1 JSON contract. TORGNEXA itself never authenticates to an administrator UI and never connects to the merchant database.

The bridge exposes health, bounded product/order reads, SKU lookup, product create/update, variant price/inventory writes and order-status writes. TORGNEXA binds the exact HTTPS store host/base path and authenticates with a callback-scoped bearer secret. Product create reconciles ambiguous outcomes by unique SKU; price, inventory and order-status writes use desired-state read-before/read-after reconciliation and fail closed when state cannot be proven.

Option-authoring, returns, webhooks and distributable Marketplace-signed `.ocmod.zip` packaging are outside Task 096 and require explicit follow-up qualification.

## Alternatives considered
Direct database access was rejected because it bypasses OpenCart application invariants and expands the credential blast radius. Administrator-session automation was rejected because it is brittle, difficult to scope and unsuitable for server-to-server operation. Coupling TORGNEXA directly to one OpenCart internal controller version was rejected because the shop-local extension can isolate version differences behind a stable contract.

## Compatibility impact
Root Connector SDK v1 and Task-094 additive commerce interfaces remain unchanged. The bridge contract is versioned independently; incompatible bridge changes require a new bridge API version rather than silent semantic drift.

## Migration and data impact
No TORGNEXA database migration is required. Existing mapping and reconciliation primitives persist remote identities and desired-state evidence. Bridge responses intentionally exclude customer billing/shipping PII from the canonical order projection.

## Security and privacy impact
TLS is mandatory and the connector accepts a bounded store host/base path rather than arbitrary request URLs. The bridge bearer token is resolved through SecretAccessor and is not stored in cleartext configuration. No OpenCart administrator credential or remote database credential crosses into TORGNEXA. The bridge must enforce its own route-level authorization and return only the minimum commerce fields required by the contract.

## Operational impact
Each OpenCart shop needs the TORGNEXA bridge extension installed and a scoped bridge token provisioned. Bridge and connector API versions must be compatible; health checking rejects unsupported versions. Marketplace packaging/signing remains a separate release artifact and is not implied by repository completion of Task 096.

## Consequences
TORGNEXA gains stable bidirectional OpenCart semantics without making Core depend on OpenCart internals. The cost is a small store-side extension that must be maintained and packaged per supported OpenCart release line.
