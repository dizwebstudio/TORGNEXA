# Task 098: Account Settings

## Status
`repository-complete` — 2026-08-15.

## Objective
Replace the Settings placeholder with a secure account overview backed by the current in-memory OIDC session and provider-owned password management.

## Dependencies
065, 084, 092, 093

## Deliverables
- capability-guarded `/settings` account screen;
- current principal, granted roles/capabilities and session-expiry presentation;
- explicit transition to the configured OIDC provider account console for password changes;
- no password, refresh token or access token persistence or rendering;
- deterministic frontend tests and updated frontend/security documentation.

## Acceptance
1. An authenticated principal with `settings.read` sees a functional account settings screen instead of the shell placeholder.
2. Password management stays at the OIDC identity boundary; TORGNEXA never receives password material.
3. Account-console navigation is restricted to the adapter's configured HTTPS issuer, except loopback HTTP used by Community development.
4. Access tokens are never rendered, logged or persisted.
5. Frontend lint, typecheck, tests, build, contracts and architecture checks pass.
