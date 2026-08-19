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
