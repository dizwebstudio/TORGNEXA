# Task 136 — Unauthenticated verified webhook ingress boundary

Status: Repository implementation complete

## Problem

Every production route is a `ProtectedRoute` and fails closed at startup
without a full Authenticator -> TenantResolver -> Authorizer chain
(`internal/app/api/security_composition.go`). A payment provider posting a
webhook has none of our OIDC credentials, so there is currently no way for an
external server to call into this system at all. `sdk.PaymentWebhookVerifier`
is fully implemented for `yookassa` and `sbp`
(`internal/platform/builtinruntime/paymentstransport.go`, added while
building the payments core: create/status/refund/reconcile are live, but
webhook delivery was explicitly descoped for exactly this reason) and unit
tested, but unreachable from outside the process. This task adds the ingress
boundary; Task 137 wires payment providers through it.

See ADR-0105 for the accepted design and rejected alternatives.

## Scope

- A new route table (e.g. `PublicWebhookRoute`) registered in
  `NewProductionHandler` alongside `ProtectedRoute`, explicitly bypassing
  Authenticator/TenantResolver/Authorizer. `validateProtectedRoute`'s
  invariants must not weaken for existing routes.
- Path shape `/api/v1/webhooks/{domain}/{connector_id}/{account_id}` (payments
  is the first `{domain}`); `account_id` resolves the tenant scope and
  connector account directly from `connector_accounts` — no bearer token is
  read.
- Fixed, non-tenant-configurable resource bounds on this path: capped body
  size, short timeout, and the existing `securityedge` per-source-IP rate
  limiter applied before any handler logic runs, on its own budget separate
  from authenticated tenant traffic.
- Uniform, fast response regardless of whether `account_id`/`connector_id`
  resolve to a real, active account — no account-enumeration signal in the
  response. Distinguish resolution vs. verification failure only in internal
  logs/metrics.
- A generic dispatch seam so a route handler can be registered per
  `(domain, connector_id)` without every domain reimplementing the
  bounds/rate-limit/enumeration behavior above.

## Acceptance criteria

- A route registered through the new table is reachable with no
  `Authorization` header and never populates `ScopeFromContext` /
  `PrincipalFromContext` (there is no principal to attach).
- An existing `ProtectedRoute` registered with an empty `Permission` still
  fails startup exactly as today; the new table cannot be used to smuggle an
  authenticated route past that check.
- Requests exceeding the body/time bound are rejected before any downstream
  domain code runs.
- The per-IP rate limit budget for this path is independent of authenticated
  API traffic budgets (a burst against the webhook path cannot starve
  unrelated tenants, and vice versa).
- Unknown `account_id`, unknown `connector_id`, and a verification failure
  inside the handler all produce the same response shape/status to the
  caller.
- Go test/vet, contracts, architecture and OpenAPI-parity gates pass with no
  new path requiring `bearerAuth`.

## Explicit exclusions

- No payment-specific (or any other domain-specific) handler logic — that is
  Task 137. This task only builds the boundary and dispatch seam.
- No signature/HMAC verification framework for providers that do sign
  deliveries; ADR-0105 treats callback re-verification as the load-bearing
  check for now. A per-provider signature pre-filter, if added later, is
  additive on top of this boundary, not a replacement for it.
- No change to `ProtectedRoute` semantics or the existing OIDC chain.
