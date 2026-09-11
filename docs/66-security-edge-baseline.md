# Security Edge Baseline

TORGNEXA deployments require a documented ingress/edge security contract independent of a specific reverse-proxy or cloud vendor.

## Baseline

- TLS termination with modern protocol/cipher policy and automated certificate rotation;
- HSTS for production HTTPS;
- trusted-proxy allowlist before honoring forwarded IP/proto headers;
- request/body/header/time limits and upload limits aligned with Upload Security Pipeline;
- global/tenant/credential/IP rate limits with explicit bypass policy for internal services;
- CORS allowlist and CSRF protection for browser cookie flows;
- admin/API optional IP allowlists and mTLS for selected machine paths;
- WAF adapter/rules for common web attacks; DDoS mitigation delegated to edge/provider where appropriate;
- bot/credential-stuffing detection hooks;
- safe error pages without stack traces/secrets;
- normalized edge security events exported to SIEM.

## Architecture

Community may use a hardened Nginx/Traefik example. Enterprise/cloud may use managed LB/WAF/DDoS services. Application authorization never trusts the edge as a substitute for authn/authz.

## Distributed API rate limits

Production API replicas use one Valkey namespace and atomic fixed-window
counters (ADR 0192). Scaling or rolling the API must not create a new allowance.
The application applies independent budgets to the validated pre-auth client
IP, the authenticated organization/workspace/principal, and public webhook
source IPs. Raw identity and IP values are SHA-256 digested before ephemeral
storage.

The local sharded limiter is permitted only for development, tests or an
explicit single-process topology. Production configuration rejects it. Active
key cardinality and the connection pool are bounded.

A confirmed exhausted budget returns `429 Too Many Requests`; `Retry-After` is
the remaining window rounded up to seconds. Startup fails when Valkey cannot be
reached. A request-time Valkey timeout, protocol error or connection failure
returns `503 Service Unavailable` with `Retry-After: 1`; no protected handler is
called. The limiter does not retry an ambiguous mutation.

Both limiter implementations expose monotonic, label-free `Allowed`, `Limited`,
`Unavailable` and `CapacityLimited` counters. Alert immediately on
`Unavailable > 0` or the `security.rate_limiter_unavailable` structured event,
and alert on sustained `CapacityLimited > 0`. Do not add IP, tenant or subject
as metric labels.
