# Frontend Auth Adapter v1

The React shell does not implement a second identity system. Production hosting provides `window.__TORGNEXA_AUTH_ADAPTER__` with `getSession`, `login`, `logout`, and optional change subscription methods. The adapter is responsible for Keycloak/OIDC authorization-code + PKCE integration or an equivalent enterprise OIDC flow.

`getSession()` returns authenticated principal identity, display name, capability claims, optional UTC expiry, and a short-lived bearer access token. `getSession({forceRefresh: true})` asks the host adapter to renew the access token before retrying a failed authenticated request. The bearer token is runtime-only secret material: it is never serialized into the public session projection, DOM, logs, URL, localStorage, sessionStorage, cookies, source configuration, or frontend contracts. A host adapter may retain an OIDC refresh token only inside its in-memory closure; it never crosses into React session state or browser persistence. Downstream-provider credentials remain outside the shell boundary entirely.

The Community adapter renews the access token before expiry. On a fresh page load it uses authorization-code + PKCE with `prompt=none` and the static `/oidc/silent-callback.html` redirect to recover an existing provider SSO session without persisting tokens. A missing provider session resolves as anonymous; invalid state, malformed tokens and unexpected provider failures fail closed.

The opaque OIDC subject is identity-mapping data, not a display-name fallback. A display claim that is empty, UUID-shaped, or equal to the subject is replaced in the UI session with a role-derived neutral label (or `Пользователь TORGNEXA` when no known role is present), so the sidebar and account profile never render the provider subject.

The shell treats missing, expired, malformed or failed sessions as anonymous. Capability checks only decide UI visibility/navigation and are never authorization evidence; every API request still relies on the server-side authenticated scope/RBAC boundary. Direct navigation to a route without its required capability fails closed in the shell.

The API client uses the generated `@torgnexa/sdk` package against same-origin `/api/v1`. A `401` triggers one forced renewal and at most one replay with the replacement access token, only within the same active session context described below. A second `401` clears that local application session; requests never enter an unbounded authentication retry loop. The browser transport rejects redirects and uses same-origin credentials semantics; client-controlled organization/workspace selectors are not synthesized.

## Session and cache lifetime (ADR-0186)

`getSession()` may additionally return `cacheScope`, a non-empty opaque string of
at most 2048 characters. It describes the host-owned issuer, organization,
workspace and login-session partition. It must change whenever any part of that
context changes, even for the same subject. It must remain stable across ordinary
token renewal within that context. It is optional for v1 adapter compatibility;
without it the shell conservatively starts a new cache lifetime on token change.
Adding or removing the scope also starts a new lifetime.

The Community adapter derives this partition from its configured issuer and
the `organization_id`, `workspace_id`, `sid` (or `session_state`) token claims.
An enterprise host with additional scope outside these claims must supply its
own complete partition. This value is identity-mapping metadata held in memory,
never a token, query key, DOM attribute, URL, public session field or persisted
record. It conveys no authorization and is never sent as an API tenant selector.
Decoded token claims are UI/cache hints; the API independently verifies identity
and derives authoritative tenant/workspace context.

The shell atomically publishes the normalized subject, scope, roles,
capabilities and an internal lifetime. Each lifetime owns a separate QueryClient
and authenticated component tree. Logout retires it before awaiting the adapter.
A change notification immediately hides the old tree while `getSession()` is
pending. Missing, malformed, failed or expired sessions retire it as well.
Changing subject, scope, roles or capabilities cancels and clears the old cache
and remounts local UI state. Display/profile changes and ordinary scoped token
renewal retain the lifetime.

Adapters must notify subscribers promptly when identity, workspace, permissions
or session validity changes; the shell cannot infer an unreported host change.
Adapter implementations must prevent stale login/renewal work from restoring a
session after logout or overwriting a later login. The shell also discards stale
adapter results. Renewal is attempted before expiry, and the hard expiry removes
the authenticated tree even while renewal is unresolved.

Every generated API request carries the lifetime's AbortSignal in addition to
its caller's signal. A retired request cannot publish a late response, renew or
log out the next session, or replay an earlier write as another identity. Cache
clearing does not undo a mutation already accepted by the server; existing
server-side authorization, audit and idempotency remain authoritative.
