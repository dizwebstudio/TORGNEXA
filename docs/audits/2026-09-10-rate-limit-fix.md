# Task 234.8 — distributed API rate-limit fix

Date: 2026-09-10  
Status: closed in repository  
Decision: ADR-0192

## Confirmed defect

The previous API composition constructed one in-memory limiter per process. A
RED regression with a configured limit of two sent two requests through two
handlers and then sent a third request through the first handler. The third
request returned `204`, demonstrating that each replica granted its own budget.
The original limiter also serialized all keys on one mutex and swept all map
entries in the request path.

## Implemented correction

- Replaced the concrete dependency with a context-aware `Limiter` interface.
- Added a Valkey adapter with an atomic Lua operation based on Valkey server
  time. Independent limiter and API handler instances share one namespace and
  counter.
- Split admission into pre-auth client-IP, authenticated
  organization/workspace/issuer/subject and public-webhook IP budgets.
- Hash all raw budget identities before local or Valkey storage. Bound active
  key cardinality, response size, pool size, connect timeout and operation
  timeout.
- Restricted the local backend to API `development` and `test` environments.
  Production and other API environments require Valkey; startup verifies it
  with `PING` before opening the listener.
- Replaced the local global mutex/full-map sweep with 64 shards, expiry heaps
  and an atomic exact key cap. A constant 64-shard expiry pass occurs only when
  capacity has already been reached.
- Made dependency failure fail closed. Confirmed exhaustion returns `429` with
  the remaining TTL rounded up in `Retry-After`. Backend failure returns `503`
  with `Retry-After: 1`, emits only the fixed budget name, and never invokes the
  application handler.
- Added label-free monotonic counters for allowed, limited, unavailable and
  capacity-limited outcomes. The operations guide defines alert conditions and
  prohibits identity/IP metric labels.

## Verification

The real-Valkey race suite creates two independent API handlers and two
independent Valkey limiter instances. The first two authenticated requests pass
across different handlers and the aggregate third request returns `429`. The
adapter concurrency case permits exactly 17 of 128 simultaneous requests. A
two-key cardinality cap rejects a third unseen key while an existing key remains
usable.

API unit scenarios cover shared NAT with separate principals, one principal
using distributed source IPs, separate webhook and authenticated budgets,
backend failure, exact `Retry-After`, local-backend topology restrictions and
local concurrent access. Full Go test/vet, contract, architecture, generated
SDK, formatting and Compose configuration checks pass. Evidence is indexed in
[the validation record](2026-09-10-evidence/rate-limit-validation.md).

## Compatibility, security and operations

No database or durable event migration is required. OpenAPI now records the
cross-cutting `429` and `503` behavior; generated SDK artifacts were refreshed.
Rate counters are ephemeral and contain SHA-256 digests rather than raw IP or
identity values. A namespace change or Valkey state loss starts fresh windows,
so production uses one stable namespace and an HA private Valkey deployment
with a scoped ACL identity.

No deployment was performed. Applying the fix requires rolling every API
replica with the shared Valkey configuration. During a Valkey outage the API
intentionally denies admission until the dependency recovers.
