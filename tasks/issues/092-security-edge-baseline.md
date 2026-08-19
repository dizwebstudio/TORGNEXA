# Task 092: Security Edge Baseline

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- trusted-proxy validation, HSTS/security headers, request/upload/rate/CORS/CSRF/admin policies and nginx example.
- Architecture review `ARCH-092` and executable tests are included.

## Objective
Define and implement deployment security-edge baseline: TLS/HSTS, trusted proxies, request limits, rate limiting, CORS/CSRF, WAF/DDoS hooks, admin allowlists and edge security event export.

## Dependencies
021, 060, 065, 066, 077, 085, 088

## Deliverables
Vendor-neutral edge contract, Community reverse-proxy example, app trusted-proxy validation, rate-limit policy, security headers/tests/runbook/SIEM wiring.

## Acceptance
Spoofed forwarded headers are rejected; limits match upload policy; browser/API security headers pass tests; edge signals are observable without replacing application auth.

Run required repository checks and report results, risks and follow-ups.