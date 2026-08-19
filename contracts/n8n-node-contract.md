# n8n Node Contract

Package: `n8n-nodes-torgnexa`. n8n remains an external runtime and is never embedded or white-labelled by this package.

## Credentials

`baseUrl` plus a scoped bearer/API credential. `baseUrl` is the absolute public API root ending in `/api/v1`. Tenant/workspace context is derived by TORGNEXA from authenticated identity; the node MUST NOT send `organization_id`, `workspace_id`, or equivalent client tenant selectors. Self-hosted and Cloud installations use the same API contract.

## Task 019 baseline resources

- Product: tenant-scoped list/search through `GET /products`.
- Order: tenant-scoped list/search through `GET /orders`.

The package is structured for later additive generic resources:
- Offer / Price / Inventory
- Return / Shipment
- Purchase Order / Supplier
- Claim / Customer Case
- Settlement / Report
- Content / Publication / Campaign
- Connector / Reconciliation
- EDO / Marking / Product Compliance / Payment / PUDO
- Counterparty / Contract / FX
- Cloud Subscription / Security Export Status

## Operations

Common: Create/Get/List/Search/Update only where the public domain API permits. Sensitive writes call public request/approval endpoints and never invoke internal application, database, connector, secret, MCP, or signing code.

## Trigger

`TORGNEXA Trigger` registers a fresh signed subscription through the public webhook API and disables it when the workflow is deactivated. `DELETE /webhook-subscriptions/{subscription_id}` is lifecycle disable, not history deletion: delivery evidence remains durable while signing material is revoked.

Incoming webhook verification MUST use the exact raw UTF-8 body with `TORGNEXA-Delivery-Id`, `TORGNEXA-Timestamp`, and `TORGNEXA-Signature`; JSON re-serialization is forbidden. Default replay window is five minutes. Configured event types are an allowlist and canonical event-name validation remains forward compatible.

Current high-value trigger choices include order changed/created, stock/price changed, publication status changed, approval required, compliance document status changed and upload quarantined. Additional canonical event types may be supplied without provider-specific node releases.

## Compatibility

The node calls public REST/webhook surfaces only. Adding a new provider plugin must not require a node release unless a new generic domain capability is added. No runtime dependency is shipped except the `n8n-workflow` peer supplied by n8n.
