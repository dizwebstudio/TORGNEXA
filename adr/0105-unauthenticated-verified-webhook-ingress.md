# ADR-0105 — Unauthenticated verified webhook ingress

Status: Proposed

## Context

Every production HTTP operation today is a `ProtectedRoute`
(`internal/app/api/security_composition.go`): `NewProductionHandler` fails
closed at startup unless a route carries a non-empty `Permission` and passes
Authenticator -> TenantResolver -> Authorizer in that exact order. That chain
assumes the caller holds one of *our* OIDC-issued bearer tokens.

A payment provider's server-to-server webhook (YooKassa, and eventually any
acquiring bank fronting SBP) cannot hold one of those tokens — it is a third
party calling us, not one of our tenants' browsers or API clients. The
existing OAuth callback path (`ConnectorOAuthCallbackPath`) looks superficially
similar but is not a precedent: it is the *user's own browser* completing a
redirect inside the user's own authenticated session, not an unattended
server-to-server call.

Task 073 / ADR-0071 already added `PaymentWebhookVerifier` to the Connector SDK
and Task 137 implements it for `yookassa`/`sbp` (`VerifyWebhook` re-fetches the
authoritative payment from the provider's own API before trusting anything the
delivery body claims — see `internal/platform/builtinruntime/paymentstransport.go`).
That verifier is fully implemented and unit-tested but has no HTTP route to
call it from: there is currently no ingress path in this codebase for an
inbound call that isn't one of our own authenticated tenants.

## Decision

Add a second, parallel route table — public webhook routes — registered
alongside `ProtectedRoute` in `NewProductionHandler`, explicitly bypassing
Authenticator/TenantResolver/Authorizer. Because it bypasses that chain, it
must supply its own, different authenticity anchor rather than skip
authentication entirely:

1. **Path-embedded routing, not a bearer token.** The provider posts to
   `/api/v1/webhooks/payments/{connector_id}/{account_id}`. `account_id`
   resolves the tenant scope and connector account from
   `connector_accounts` directly; no `Authorization` header is read or
   trusted.
2. **The real trust anchor is the existing callback-verification contract,
   not a signature on the inbound request.** `VerifyPaymentWebhook` uses the
   resolved account's own stored secret to call the provider's API and
   confirm the payment independently of the delivered body. This works even
   for providers (YooKassa) that sign nothing, and is strictly stronger than
   trusting a header: an attacker who guesses/enumerates a valid
   `account_id` still cannot forge a payment the provider's own API will not
   corroborate.
3. **Fixed, small resource bounds**, independent of the tenant-configurable
   limits `ProtectedRoute` uses: capped body size (already 1<<20 in
   `VerifyPaymentWebhook`), a short fixed timeout, and the same
   `securityedge` per-source-IP rate limiter every other route uses, applied
   before the handler runs.
4. **Replay protection via the existing append-only evidence table**
   (`payment_webhook_receipts`, migration 000018) keyed on
   `(organization_id, workspace_id, connector_account_id, delivery_id)`.
   `delivery_id` is a content hash of the verified callback response (already
   computed in `paymentstransport.go`), so a true provider retry (identical
   redelivery) dedups and a genuinely new state transition does not.
5. **No account enumeration.** The handler returns the same fast response
   (e.g. `200`) whether or not `account_id` resolves to a real account of
   the claimed `connector_id`, and logs the distinction only internally.
   Verification failures never leak *why* to the caller.
6. This is a new, narrow route class, not a general escape hatch:
   `validateProtectedRoute`'s invariants (non-empty `Permission`, full auth
   chain) continue to apply, unmodified, to every existing route. Only
   webhook routes explicitly registered in the new table skip them.

## Alternatives considered

- **Shared-secret HMAC signature header.** Rejected as the sole mechanism:
  YooKassa signs nothing, so this would require a per-provider inconsistent
  path anyway, and a leaked shared secret is a standing risk with no
  rotation story as clean as re-verifying against the provider's own API.
  May still be layered in per-provider where a bank does sign, as a cheap
  pre-filter before the callback round trip — but never as a *replacement*
  for it.
- **IP allowlisting.** Rejected: provider IP ranges rotate and are not
  reliably published/stable enough to fail closed on.
- **Client-certificate (mTLS) from the provider to us.** Rejected: payment
  providers do not originate client certificates to arbitrary merchants;
  this direction of mTLS does not exist in practice for YooKassa or typical
  SBP acquirer gateways.
- **Route account_id resolution through a signed, expiring URL token
  instead of a bare ID.** Deferred, not rejected: adds forgery resistance
  against URL leakage (logs, referrers) at the cost of needing token
  issuance/rotation UX during account setup. Worth revisiting once a second
  webhook-capable provider family (not just payments) exists and the
  ingress is generalized.

## Compatibility impact

Additive only. No existing `ProtectedRoute` changes shape or behavior. New
OpenAPI paths are added without a security scheme (or a distinct
`webhookAuth` scheme documenting the callback-verification contract instead
of `bearerAuth`).

## Migration and data impact

None beyond the already-applied migration 000018 (`payment_webhook_receipts`).

## Security and privacy impact

The webhook body is never persisted or trusted directly; only its digest and
the independently re-fetched, verified fields are. Account resolution failure
and verification failure are indistinguishable to the caller. This route
class carries no tenant bearer token and must never be reachable by
`ScopeFromContext`/`PrincipalFromContext` — those remain `ProtectedRoute`-only
by construction (a public webhook handler has no principal to attach).

## Operational impact

Public webhook routes need their own rate-limit budget separate from
per-tenant API traffic (a burst from one aggressive provider must not starve
unrelated tenants), and their failures should page differently than an
authenticated-route failure: a spike in verification failures on this path is
either a misconfigured account or an attempted forgery, not a tenant bug.

## Consequences

- `PaymentWebhookVerifier` becomes reachable for the first time since Task
  073/ADR-0071 introduced it.
- Establishes the pattern any future inbound provider callback (not just
  payments) reuses, instead of each one inventing its own bypass.
- Real-time payment status updates become possible; Task 138's polling
  reconciliation becomes the safety net for missed/failed deliveries rather
  than the only freshness mechanism.
