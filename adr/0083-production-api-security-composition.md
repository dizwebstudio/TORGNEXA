# ADR 0083: Production API routes require one fail-closed security composition

## Status
Accepted as a security-hardening supplement to Task 092.

## Context
The repository had strong edge, IAM and tenant-aware primitives, but the runnable API process
served only health while feature handlers were exposed as independent constructors. That made
future route composition an unsafe manual step: a new handler could be mounted without proving
that authentication, tenant resolution and authorization were all present.

## Decision
`internal/app/api.NewProductionHandler` is the only exported production handler factory.
Health is internally registered as the sole public route. Every configured application route
has a mandatory permission and startup fails if any of Authenticator, TenantResolver or
Authorizer is absent. The edge policy always executes before application routing. Feature
handler constructors are package-private, and an AST regression test rejects new exported
`New*Handler` factories.

## Alternatives considered
Keeping independent exported handler constructors and relying on deployment documentation was
rejected because the security invariant would remain optional. Trusting tenant or identity
headers from the reverse proxy was rejected because it would make proxy configuration part of
the authorization boundary. A generic `Public` route flag was also rejected because a future
feature could accidentally bypass authentication.

## Compatibility impact
The existing `GET/HEAD /api/v1/health` contract is unchanged. Feature handlers that were not
mounted before remain unmounted. Their internal constructors are package-private, so external
Go packages must use the secure production composition rather than mounting them directly.

## Migration and data impact
No database migration or durable data rewrite is required. Request context gains only a
minimized principal, canonical tenant scope and validated client IP after the relevant checks
succeed.

## Operational impact
Production requires an explicit trusted-proxy CIDR configuration. Invalid edge configuration
or a configured private route without all security dependencies aborts API startup. Operators
can keep private routes disabled until an identity adapter is qualified.

## Security and privacy impact
Authentication, tenant resolution and authorization are sequenced and fail closed. Raw bearer
tokens and unbounded IdP claims are not copied into request context. Forwarded headers from
untrusted peers, disallowed browser origins and over-limit requests are rejected before
application handlers.

## Consequences
No current private feature API is exposed until a concrete production identity adapter and
route registry are deliberately composed. Adding a private route requires an explicit
permission and security dependencies; adding another public route requires a reviewed code
change rather than a boolean bypass.
