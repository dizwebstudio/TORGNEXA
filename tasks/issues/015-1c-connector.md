# Task 015

ERP spec; implement one configured HTTP/OData read-only baseline, mappings and reconciliation; CommerceML separate adapter.

## Status

Completed.

## Acceptance

Implementation + tests + updated contracts/docs; run required checks.

## Repository completion — 2026-08-10

Repository implementation is complete.

- `onec` is the first architecture-registered ERP provider. The manifest grants only `erp.catalog.read` and `erp.inventory.read`; ERP writes/orders and CommerceML are not part of Task 015.
- The connector uses the standard 1C:Enterprise OData v3 publication surface. Host/base path, catalog/register names and field mappings are non-secret account configuration resolved through a provider-local host-injected resolver; the frozen SDK-v1 `Runtime` root is unchanged.
- Basic-auth username/password remain one Task-021 opaque secret reference and plaintext exists only inside `SecretAccessor.UseSecret`.
- Task 015 adds additive provider-neutral SDK-v1 `ERPCatalogReader` and `ERPInventoryReader` projections. Inventory quantity is an exact bounded decimal string; no float64 crosses the connector boundary.
- Catalog reads preserve remote reference identity, code/SKU/title/brand, opaque `DataVersion`-style revision and explicit archive state. Inventory reads use configured accumulation-register `Balance()` product/location/quantity fields.
- Ordered `$top/$skip` cursors are opaque and SHA-256-bound to the publication/mapping configuration. Duplicate IDs/balances, negative/exponent quantities, malformed/oversized bodies, unsafe configuration and cursor reuse after mapping changes fail closed.
- 1C reference IDs stay Task-010 remote mappings; Task-013/014 own source-of-truth, replay and scheduled-full/on-demand reconciliation. No configuration-independent updated-at field is fabricated for incremental sync.
- Provider code has no direct HTTP/socket/DNS, database, filesystem/process, Core or App authority; actual egress is host-injected.
- Deterministic fixtures and `docs/connectors/onec/conformance-report.json` qualify the adapter; all 13 mandatory Task-064 checks pass including Linux namespace/chroot isolation.

Operational note: production enablement still requires a least-privilege read-only 1C user, TLS publication, a live `$metadata` smoke test, confirmation of configured OData catalog/register and field names, and existing Task-080 hosted architecture controls. The exact-decimal ERP inventory reader is exposed through the reviewed built-in runtime registry and remains separate from the integer-based commerce inventory route.
