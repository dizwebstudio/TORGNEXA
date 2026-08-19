# ADR 0082: Vendor-neutral security edge contract

## Status
Accepted

## Context
Task 092 must harden reverse-proxy/browser/API ingress while preserving application authentication and Task 088 upload policy.

## Decision
Validate trusted proxy chains before honoring forwarded headers, enforce HSTS/security headers, request/upload limits, CORS/CSRF, rate limits and admin allowlists, and emit minimized edge security signals to Task 085.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Trusting X-Forwarded-For from arbitrary clients and using WAF decisions as application authorization were rejected.

## Compatibility impact
No public API shape changes; limits and headers are deployment policy around existing endpoints.

## Migration and data impact
No durable edge table is required; rate counters are ephemeral and security evidence is exported through SIEM/audit paths.

## Security and privacy impact
Spoofed forwarding is rejected, browser unsafe methods require allowed origin, upload limits cannot exceed the released-media policy, and edge controls do not replace OIDC/RBAC.

## Operational impact
Community deployment receives a reverse-proxy example; production deployments qualify WAF/DDoS vendor hooks separately while preserving the same contract.
