# Task 103: Settings Security and Audit View

## Status
`implemented`

## Objective
Provide a read-only security overview for privileged settings changes, active identity configuration and recent audit evidence.

## Dependencies
099, 100, 101, 102, 060, 085

## Acceptance
- read-only, tenant-scoped audit timeline with cursor pagination;
- sensitive values and tokens are minimized/redacted;
- each settings mutation exposes correlation and actor evidence;
- SIEM export remains asynchronous and authoritative commits do not depend on the sink;
- UI clearly distinguishes configuration state from externally verified provider health.

## Implementation

- `GET /api/v1/settings/security/configuration`, `/sessions`, `/logins` and `/audit` expose a tenant-scoped read model; all list endpoints are cursor-paginated.
- validated OIDC sessions are registered from `sid` (or issuance time fallback) using SHA-256 references. Raw `sid`, `sub`, bearer tokens, IP addresses and User-Agent values are not persisted.
- `POST /api/v1/settings/security/sessions/{session_ref}:revoke` atomically revokes TORGNEXA API access and appends actor/correlation audit evidence; it does not claim to terminate provider-wide SSO.
- the Settings UI labels observed login evidence separately from Keycloak history and labels runtime configuration separately from unverified provider health.
- authoritative audit remains in PostgreSQL; the existing SIEM delivery path remains asynchronous and cannot block the settings transaction.
