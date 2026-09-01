# n8n compatibility matrix

The package is a public n8n community node. n8n is never embedded, bundled or
given access to TORGNEXA internals.

| Package | n8n node API | Node.js | TORGNEXA API | Status |
| --- | --- | --- | --- | --- |
| `0.2.x` | `1` | `22.16.x–22.x` | `/api/v1` | supported |

## Supported surfaces

| Surface | Operations | Safety boundary |
| --- | --- | --- |
| Product | list/get/create/update, offers, prices, category assignment, image lifecycle | public catalog permissions, idempotency for writes, optimistic version in body where applicable |
| Catalog | list/create categories | public catalog permissions and idempotency |
| Order | list/get/change status | authenticated tenant scope, `orders.status.write`, idempotency, optimistic version and audit |
| Inventory | positions, warehouses, fulfillment allocations, warehouse incidents | stock permissions, idempotency for mutations, append-only/audited server state |
| Fulfillment/WMS | tasks, scans, task history, packing batches and handoff | WMS permissions, idempotency, assignment/lifecycle/version checks |
| Synchronization | status, policy create/update/run, drift resolution | sync permissions, connector capability guard, idempotency and optimistic version |
| Pricing | deterministic repricing preview | dry-run only; no remote price write |
| Trigger | catalog/order/price/stock/warehouse/fulfillment/return/approval events | HTTPS callback, exact-body HMAC, five-minute replay window, allowlist and durable disable |

## Versioning and support policy

- `0.2.x` is compatible with the public `/api/v1` contracts listed above.
- Additive API fields and canonical event types are accepted without a node
  major version; changed required fields or semantics require a compatibility
  review and a new package major/minor decision.
- The package pins the `n8n-workflow` peer to `2.16.0` and supports Node.js
  `22.16.x–22.x`.
- Every release runs TypeScript, offline security/unit tests, artifact install
  E2E, package verification and `npm pack --dry-run`.
- Credentials contain only the absolute API base URL and bearer token. Tenant
  or workspace selectors, raw database access, SecretProvider internals,
  private signing keys and arbitrary network access are not supported.
- n8n writes must use the same public approval/policy/idempotency boundaries as
  other clients. An operation is not exposed merely because an HTTP endpoint
  exists; unsupported capabilities fail closed.

## Release checklist

1. Update package version and this matrix.
2. Run `npm run build`, `npm run test:offline`, `npm run test:e2e`,
  `npm run verify:package` and `npm pack --dry-run`.
3. Inspect the tarball contents; only `dist`, package metadata, README, license
   and compatibility documentation may ship.
4. Publish from a protected release job with provenance and a disposable n8n
   install test; never publish from a workstation with production credentials.
