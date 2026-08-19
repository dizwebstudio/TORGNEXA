# Settings security

Task 103 adds an administrator-only security section to Settings. PostgreSQL is the authoritative store for minimized application session state and immutable login/audit evidence. Keycloak remains the identity provider and owns passwords, authentication methods and provider-wide SSO sessions.

## Session semantics

After UserInfo validates a bearer token, the API derives SHA-256 references from the configured issuer, OIDC `sub` and `sid` (with issuance time as a bounded fallback). Only those references, timestamps and a bounded client class (`browser`, `mobile`, `api`, `unknown`) are stored. Tokens, raw provider identifiers, IP addresses and raw User-Agent values are prohibited.

Revocation is application-enforced: the session row becomes `revoked` and subsequent requests using that OIDC session receive `401`. The same transaction appends `settings.security.session_revoked` audit evidence with actor and correlation ID. Provider-wide logout is intentionally delegated to the Keycloak account console.

The login timeline means “first observed by TORGNEXA after successful OIDC validation”; it is not presented as a complete Keycloak authentication history. Runtime configuration is shown as `configured`, while provider health is explicitly `not_verified` until a separate external probe has run.

## Data governance and SIEM

Session history is tenant-scoped under forced RLS and append-only. It has security-evidence retention: at least 180 days, then the deployment retention policy applies. SIEM export consumes authoritative audit asynchronously; sink failure never participates in the session revocation commit.
