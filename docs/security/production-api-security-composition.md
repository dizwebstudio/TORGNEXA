# Production API security composition

Status: enforced in the production API runtime.

The API process has a single production HTTP composition root: `api.NewProductionHandler`.
It always applies the vendor-neutral security edge before routing and it registers only the
health endpoint as public. Application route descriptors cannot opt out of authentication.

Every non-health route must provide a permission and startup fails closed unless all three
host-owned dependencies exist:

1. `Authenticator` validates the credential and returns a minimized principal. HTTP headers
   are not accepted as identity by the composition root.
2. `TenantResolver` resolves a canonical organization/workspace scope for that principal.
3. `Authorizer` approves the named permission inside that resolved scope.

Only after all three stages succeed are the principal and tenant scope attached to request
context. Admin-only routes additionally require the validated client IP to be in the
configured admin CIDRs.

The same pipeline applies trusted-proxy/X-Forwarded-For validation, bounded rate limiting,
request body limits, browser origin/CSRF policy and response security headers. Forwarded
headers from an untrusted socket peer are rejected.

## Drift prevention

Feature-specific API handler constructors are package-private. A package test parses all
non-test Go files and fails if another exported `New*Handler` factory is introduced.
The public health route is created internally; `ProtectedRoute` has no public-bypass flag.
Repository CI executes these tests before release.

## Production configuration

Production API deployments must set `TORGNEXA_SECURITY_TRUSTED_PROXY_CIDRS` explicitly.
Admin CIDRs, browser origins, body limits, rate limit and HSTS lifetime are also loaded from
validated security configuration. An invalid edge configuration aborts API startup.
