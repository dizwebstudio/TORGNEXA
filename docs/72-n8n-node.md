# n8n Node

Task `019` ships `integrations/n8n-nodes-torgnexa` as a separate community-node package. TORGNEXA does not embed n8n and the node has no privileged in-process access.

## Baseline surface

- Credential: public TORGNEXA `/api/v1` base URL plus scoped bearer token.
- Product: list/search through `GET /products`.
- Order: list/search through `GET /orders`.
- Trigger: dynamic Task-063 signed webhook subscription with canonical event allowlist and additive custom canonical event names.

There is intentionally no raw API, SQL, provider-specific or generic mutation escape hatch. A future write operation must exist as a reviewed public TORGNEXA endpoint first; sensitive writes remain subject to Task `017` and relevant governance.

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

## Package boundary

The repository source package targets Node.js 22+ and the current n8n programmatic-node layout. `n8n-workflow` is a peer supplied by the host; no n8n runtime is shipped with TORGNEXA. Offline tests use local type stubs only as a sandbox compile harness; those stubs are excluded from the published `dist` package.

Task `078 Plugin Marketplace Governance` is the next dependency-ready task and owns publication/admission policy for plugin packages.
