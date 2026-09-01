# n8n-nodes-torgnexa

TORGNEXA community node package (`0.2.x`). n8n remains an external runtime:
this package calls only the public TORGNEXA REST API and signed webhook surface.

## Included in Task 019

- `TORGNEXA API` credential: absolute `/api/v1` base URL plus a scoped bearer credential.
- `TORGNEXA` action node: Product cards, offers, prices, images, catalog
  categories, orders, inventory positions, warehouses, fulfillment allocations,
  warehouse tasks/batches, synchronization policies/drifts and pricing preview.
- All mutation operations require a contract-valid `Idempotency-Key`; optimistic
  versions are passed for order, WMS and synchronization state transitions.
- Sensitive operations remain bounded by the public TORGNEXA permission,
  approval/policy and audit layers. The node does not create a privileged path
  or manufacture an approval for an endpoint that does not support one.
- `TORGNEXA Trigger`: dynamic signed-webhook subscription, HMAC-SHA256 verification over the exact raw request body, replay-window validation, event allowlisting, and lifecycle deactivation.

The package does **not** accept organization/workspace selectors. Tenant scope is derived by TORGNEXA from the authenticated credential. The node never reads PostgreSQL, SecretProvider, Connector SDK internals, or MCP internals.

Sensitive writes are exposed only where the public API provides the required
permission, approval/policy gate, optimistic version and idempotency contract;
an n8n workflow is not a privileged principal.

## Local development

Requires Node.js 22+.

```bash
npm install
npm run lint
npm run build
npm run dev
```

The repository also provides an offline structural/unit test that does not install n8n:

```bash
npm run test:offline
npm run test:e2e
npm run pack:verify
npm run verify-package
```

`test:e2e` builds a package tarball, installs that exact artifact in an isolated
temporary n8n-compatible host harness, executes read and mutation workflows
against a local HTTP API double, and verifies signed-trigger delivery. A real
n8n qualification run may additionally point the same artifact at a disposable
n8n instance; it must not use production credentials.

See [COMPATIBILITY.md](./COMPATIBILITY.md) for the supported n8n/API matrix.

## Webhook security

Activation generates a fresh 64-character signing secret and registers the n8n HTTPS callback through `/webhook-subscriptions`. Deactivation calls `DELETE /webhook-subscriptions/{id}`, which disables the TORGNEXA subscription and revokes its signing secret while retaining delivery history.

Incoming events are accepted only when `TORGNEXA-Delivery-Id`, `TORGNEXA-Timestamp`, and `TORGNEXA-Signature` validate against the exact raw body within the five-minute replay window. JSON re-serialization is never used for signature verification.

## Packaging boundary

No n8n runtime is embedded or redistributed by TORGNEXA. This package contains no runtime dependencies other than the `n8n-workflow` peer supplied by n8n itself.
