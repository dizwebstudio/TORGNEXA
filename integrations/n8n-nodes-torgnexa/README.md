# n8n-nodes-torgnexa

TORGNEXA community node package. n8n remains an external runtime: this package calls only the public TORGNEXA REST API and signed webhook surface.

## Included in Task 019

- `TORGNEXA API` credential: absolute `/api/v1` base URL plus a scoped bearer credential.
- `TORGNEXA` action node: Product list/search and Order list/search using canonical REST contracts.
- `TORGNEXA Trigger`: dynamic signed-webhook subscription, HMAC-SHA256 verification over the exact raw request body, replay-window validation, event allowlisting, and lifecycle deactivation.

The package does **not** accept organization/workspace selectors. Tenant scope is derived by TORGNEXA from the authenticated credential. The node never reads PostgreSQL, SecretProvider, Connector SDK internals, or MCP internals.

Sensitive writes are intentionally absent from the initial node. Future sensitive operations must call the same public approval/request endpoints used by other clients; an n8n workflow is not a privileged principal.

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
```

## Webhook security

Activation generates a fresh 64-character signing secret and registers the n8n HTTPS callback through `/webhook-subscriptions`. Deactivation calls `DELETE /webhook-subscriptions/{id}`, which disables the TORGNEXA subscription and revokes its signing secret while retaining delivery history.

Incoming events are accepted only when `TORGNEXA-Delivery-Id`, `TORGNEXA-Timestamp`, and `TORGNEXA-Signature` validate against the exact raw body within the five-minute replay window. JSON re-serialization is never used for signature verification.

## Packaging boundary

No n8n runtime is embedded or redistributed by TORGNEXA. This package contains no runtime dependencies other than the `n8n-workflow` peer supplied by n8n itself.
