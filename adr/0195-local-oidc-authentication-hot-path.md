# ADR-0195 — Local OIDC authentication hot path

Status: Accepted

## Context

The authenticated API path performed one UserInfo HTTP request, one
authoritative session transaction and two workspace-membership transactions
before every business handler. The access-token payload was decoded but its
signature was not locally verified. This made Keycloak latency part of every
request, amplified PostgreSQL load and left decoded claims dependent on the
separate UserInfo response for authenticity.

## Decision

The API verifies access tokens locally as RS256 JWTs. The verifier accepts only
an issuer-bound JWKS endpoint and validates the signature, exact issuer,
configured API audience, exact authorized party, expiry, not-before, issued-at
and a bounded non-empty subject. The Community realm issues
`aud=torgnexa-api` to the `azp=torgnexa-web` client. An unsigned or merely
decoded payload is never identity or authorization evidence.

JWKS responses are singleflight-cached per API process. Cache-Control max-age
is bounded to 30 seconds–one hour, unknown keys and a signature mismatch can
trigger rotation refresh, and attacker-controlled refetches are limited to one
per second. A previously verified key may be used for at most 15 minutes after
normal expiry when the issuer is unavailable. With no usable cached key an
issuer outage returns the existing generic authentication `503`; invalid
tokens return `401`.

UserInfo is profile hydration and verified-email evidence, not token
validation. Successful responses are cached for five minutes with a 4096-entry
process bound and concurrent requests share one fetch. A transient failure is
cached for 15 seconds and authentication continues from signed token claims;
verified invitation email remains empty. A malformed response or a different
subject provides no profile or invitation evidence; a locally valid JWT can
still authenticate an already bound member.

Active membership is positive-cached for one second with a 4096-entry bound
and singleflight fill. Tenant resolution stores the database-authoritative
member in a typed request context, and the authorizer consumes that exact
value, so one request cannot perform a second membership lookup. Cache keys
contain organization, workspace and the irreversible subject reference.
Long-lived SSE reauthorization bypasses this cache and checks membership
directly on every scheduled security refresh.

Application-session status is still read and row-locked on every request.
`last_seen_at` is written at most once per minute unless token expiry advances;
creation, subject binding and revoked status remain authoritative. Throttling
therefore removes writes without caching revocation state.

The shared runtime exposes label-free counters for JWKS/UserInfo HTTP calls,
session and membership database calls, cache hits, throttled/written session
timestamps and authorized/denied/unavailable outcomes. Per-request IdP/DB call
distributions and fixed-bucket security-path latency p50/p95/p99 are retained
without route, tenant, subject, token or error labels. The latency observation
ends before the business handler or SSE stream begins.

## Compatibility and rollout

No database migration, REST schema or event change is required. Deployment
must add the Keycloak `torgnexa-api` audience mapper before starting the new
API and configure `TORGNEXA_OIDC_JWKS_URL` and
`TORGNEXA_OIDC_AUDIENCE=torgnexa-api`. Access tokens minted without the API
audience are rejected and must be renewed after the mapper is installed.

Mixed API versions remain data-compatible. Old replicas continue calling
UserInfo on every request; new replicas use local verification and bounded
caches. Membership changes can remain visible to ordinary requests for at most
one second on each new replica, while session revocation and SSE checks remain
immediate and fail closed.

## Security and privacy

JWT algorithms and key types are allowlisted; redirects and non-issuer egress
remain denied. Raw tokens, provider responses and raw subjects are not cached
as metrics or copied to request context. Profile PII exists only in the bounded
in-memory hydration cache and follows the access process lifetime. Invitation
binding still requires `email_verified=true` from a same-subject UserInfo
response and never falls back to the token claim.

## Validation

Unit and race tests cover signature and claim rejection, unsigned payloads,
same-`kid` rotation, unknown/stale keys, concurrent cache fills, provider
outage, call distributions and latency percentiles. A 100-request concurrent
profile asserts one JWKS call, one UserInfo call, one membership lookup, one
session check per request and one initial timestamp write. PostgreSQL tests
under forced RLS verify the one-minute write interval, expiry advancement,
revocation, invitation ownership and SSE membership/session failure paths.
