# ADR-0190 — Continuous authorization of open SSE streams

Status: Accepted

## Context

ADR-0189 fixed write deadlines and reconnect refresh. An established stream
still retained the access decision made when it connected: credential expiry,
application session revocation and membership changes did not stop it. Even
metadata-only audit invalidations require a current tenant-scoped permission.

## Decision

Reuse the protected route's Authenticator, TenantResolver and Authorizer in
that order every 15 seconds, with a five-second total timeout per check. One
monitor per connection runs serial checks independently of audit polling and
frame writes. Any denial, unavailable dependency, timeout or cancellation ends
the stream. Concrete security dependencies must honor the supplied context.
Handler completion cancels and joins its monitor; checks cannot outlive it.

Bind the stream to the originally authenticated issuer, subject, session and
subject references, canonical organization/workspace, credential expiry and
route permission. Rechecks use fresh identity and scope in their context and
the original permission. Changed roles are evaluated by the current authorizer;
changing identity, tenant, session or expiry inside a connection is forbidden.
Token renewal requires a new connection through the usual protected route.

The OIDC authenticator supplies the original validated token expiry as private
request-local Principal.ExpiresAt. A separate context deadline cancels the
stream at that expiry without waiting for the next check. Every frame deadline
is the earlier of this deadline and the ADR-0189 five-second write budget, so
expiry also interrupts a blocked write. Missing expiry or a missing trusted
authorization binding refuses the stream before audit access or SSE output.

An access failure after streaming starts closes the stream without appending
JSON, authentication events or dependency details to its payload. Reconnection
uses normal authorization and pre-stream HTTP error handling. Existing
ready/invalidate/heartbeat frames and reconnect refresh remain unchanged.

## Security and operational bounds

This is periodic reauthorization, not instantaneous revocation. A change just
after a successful check can wait for the next 15-second tick and up to five
seconds of checking. A write already underway retains its bounded write budget
when the monitor cancels access. Bytes already delivered or buffered cannot be
retracted; proxy behavior and scheduler delays are outside these application
timers. Credential expiry independently limits both the context and writes.

Checks deliberately use authoritative membership/session stores and existing
OIDC UserInfo validation; no allow-on-error or authorization cache is added.
Consequently an IdP/store outage closes established streams as well. The cost
is up to four additional complete checks per minute per connection: UserInfo,
session observation and both existing membership resolutions. Session checks
can update last_seen through the established repository and must not duplicate
login evidence or reactivate revoked sessions. Shared watchers, connection
caps, JWKS validation, membership deduplication and last_seen throttling remain
Task 234.6/234.7; this change makes no production capacity claim.

## Compatibility, migration and privacy

No migration, new dependency, persisted field or event schema. ExpiresAt is
excluded from JSON; the guard stores only trusted dependencies and the original
binding, without copying bearer tokens, request objects, profile or email into
that context value. SSE remains metadata-only and normal data reads retain
authorization and forced RLS. Existing session evidence retention is unchanged.

OpenAPI documents the lifetime and failure semantics; generated SDK hashes are
refreshed without changing public operations or signatures. Internal alternate
Authenticator implementations must return a verified future expiry to serve
SSE. Deploy every API instance and drain old connections to apply the fix to
existing streams. No frontend runtime change is required. Rolling back restores
the original lifetime gap, so it is not a safe substitute for this behavior.

## Validation

Before the fix, revoking a permission left a real HTTP stream open until the
17-second test cancellation. Regression coverage now includes permission and
binding changes, fresh authorization context, absent/expired credentials,
provider failure, timeout, late success and cancellation during a check.
Real TLS HTTP/1.1 and HTTP/2 exercise expiry independently of the monitor;
http.Server/net.Pipe proves blocked writes stop at expiry, before their budget.

Real PostgreSQL with forced RLS and the production OIDC/session/membership
composition proves active sessions survive rechecks, then streams close on
session revocation, member disable, membership/session storage failure or
rejected UserInfo subject. Fixtures are synthetic, with local TLS UserInfo.
A10 HTTP and isolated Chrome reconnect/coalescing/logout regressions also pass.
See the [report and evidence](../docs/audits/2026-09-10-sse-authorization-fix.md).
