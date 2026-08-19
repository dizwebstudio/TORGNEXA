# ADR 0034: Define a Vendor-Neutral Security Edge Contract

## Decision
Specify TLS/trusted-proxy/rate-limit/CORS-CSRF/WAF/upload-limit/DDoS/edge-event requirements independent of Nginx, ingress or cloud vendor.

## Consequences
Community and Enterprise deployments differ in implementation but meet the same security baseline.
