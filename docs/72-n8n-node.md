# n8n Node

Task `019` ships `integrations/n8n-nodes-torgnexa` as a separate community-node package. TORGNEXA does not embed n8n and the node has no privileged in-process access.

## Supported surface (`0.2.x`)

- Credential: public TORGNEXA `/api/v1` base URL plus scoped bearer token.
- Product: list/get/create/update cards, offers, prices, category assignment and image lifecycle.
- Catalog: list/create categories.
- Order: list/get and idempotent optimistic-concurrency status transitions.
- Inventory: positions, warehouses, fulfillment allocations and warehouse incidents.
- Fulfillment/WMS: tasks, barcode scans, task history, packing batches and handoff.
- Synchronization: status, policy create/update/run and drift resolution.
- Pricing: deterministic repricing preview without remote price writes.
- Trigger: dynamic Task-063 signed webhook subscription with canonical event allowlist and additive custom canonical event names.

There is intentionally no raw API, SQL, provider-specific or generic mutation escape hatch. Every exposed mutation requires a contract-valid `Idempotency-Key`, and state transitions pass an optimistic version. A future sensitive write must exist as a reviewed public TORGNEXA endpoint and approval contract first; n8n cannot become a privileged bypass.

## Tenancy and transport

Organization/workspace are derived exclusively from authenticated TORGNEXA identity. Credential/node UI contains no tenant selector and the shared client rejects tenant-selector query names. API paths are relative to the configured `/api/v1` root. HTTPS is mandatory except loopback development, redirects are disabled and requests use bounded timeouts.

## Trigger lifecycle

On activation the node generates a unique subscription id and a fresh 32-byte signing secret, then registers the current n8n HTTPS callback. The receiver copy of the secret is kept in n8n workflow static state; TORGNEXA stores only its secret-provider reference.

On deactivation/reconfiguration the node calls `DELETE /webhook-subscriptions/{id}`. The endpoint is idempotent and means **disable**, not evidence deletion: the subscription row and delivery history remain while the active signing reference is revoked.

Incoming deliveries are accepted only when all of the following hold:

- the exact raw request body is available;
- `TORGNEXA-Delivery-Id`, `TORGNEXA-Timestamp` and `TORGNEXA-Signature` are present and valid;
- HMAC-SHA256 over `timestamp + "." + rawBody` matches using constant-time comparison;
- timestamp age is within the five-minute replay window;
- envelope delivery id matches the signed header;
- event type is in the configured canonical allowlist.

JSON re-serialization before verification is forbidden.

## Package and E2E boundary

The repository source package targets Node.js 22+ and the current n8n programmatic-node layout. `n8n-workflow` is a peer supplied by the host; no n8n runtime is shipped with TORGNEXA. Offline tests use local type stubs only as a sandbox compile harness; those stubs are excluded from the published `dist` package.

The package publishes as `n8n-nodes-torgnexa` with a versioned npm artifact and compatibility matrix. `test:e2e` installs the exact tarball into an isolated n8n-compatible host harness, executes representative read/mutation workflow steps and verifies a signed trigger. A protected deployment may repeat the same artifact against a disposable real n8n instance; production credentials are forbidden.

Task `078 Plugin Marketplace Governance` owns publication/admission policy for plugin packages; n8n distribution remains external and does not embed n8n.
