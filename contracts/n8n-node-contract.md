# n8n Node Contract

Package: `n8n-nodes-torgnexa`. n8n remains an external runtime and is never embedded or white-labelled by this package.

## Credentials

`baseUrl` plus a scoped bearer/API credential. `baseUrl` is the absolute public API root ending in `/api/v1`. Tenant/workspace context is derived by TORGNEXA from authenticated identity; the node MUST NOT send `organization_id`, `workspace_id`, or equivalent client tenant selectors. Self-hosted and Cloud installations use the same API contract.

## Current resources (`0.2.x`)

- Product: list/get/create/update cards, offers, prices, category assignment and image lifecycle through the public catalog API.
- Catalog: list/create categories.
- Order: list/get and lifecycle status change with `Idempotency-Key` and optimistic version.
- Inventory: positions, warehouses, fulfillment allocations and warehouse incidents.
- Fulfillment/WMS: tasks, scans, task history, packing batches and handoff.
- Synchronization: status, policy create/update/run and drift resolution.
- Pricing: deterministic repricing preview; no remote price write.

The package is structured for later additive generic resources:
- Return / Shipment domain writes requiring a reviewed public approval contract
- Purchase Order / Supplier
- Claim / Customer Case
- Settlement / Report
- Content / Publication / Campaign
- Connector / Reconciliation
- EDO / Marking / Product Compliance / Payment / PUDO
- Counterparty / Contract / FX
- Cloud Subscription / Security Export Status

## Operations

Common: Create/Get/List/Search/Update/Delete only where the public domain API permits. Every exposed mutation uses a contract-valid `Idempotency-Key`; state transitions also pass the public optimistic version. Sensitive writes call public request/approval endpoints where that contract exists and never invoke internal application, database, connector, secret, MCP, or signing code. The node does not invent or bypass an approval flow for endpoints that do not accept an approval reference.

## Trigger

`TORGNEXA Trigger` registers a fresh signed subscription through the public webhook API and disables it when the workflow is deactivated. `DELETE /webhook-subscriptions/{subscription_id}` is lifecycle disable, not history deletion: delivery evidence remains durable while signing material is revoked.

Incoming webhook verification MUST use the exact raw UTF-8 body with `TORGNEXA-Delivery-Id`, `TORGNEXA-Timestamp`, and `TORGNEXA-Signature`; JSON re-serialization is forbidden. Default replay window is five minutes. Configured event types are an allowlist and canonical event-name validation remains forward compatible.

Current high-value trigger choices include product/offer/order changed, order created, allocation/task/batch/shipment changed, inventory position/stock/warehouse changed, price changed, return requested/state changed, warehouse stock moved, publication status changed, approval required, compliance document status changed and upload quarantined. Warehouse state changes are the canonical incident-related signal; the event catalog does not define a separate incident event. Additional canonical event types may be supplied without provider-specific node releases.

## Compatibility

The `0.2.x` package targets n8n node API `1`, Node.js `22.16.x–22.x` and the public `/api/v1` contract. Artifact install E2E, offline security/unit tests, package verification and `npm pack --dry-run` are release gates. Adding a new provider plugin must not require a node release unless a new generic domain capability is added. No runtime dependency is shipped except the `n8n-workflow` peer supplied by n8n.
