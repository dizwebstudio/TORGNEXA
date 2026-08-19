# Frontend Auth Adapter v1

The React shell does not implement a second identity system. Production hosting provides `window.__TORGNEXA_AUTH_ADAPTER__` with `getSession`, `login`, `logout`, and optional change subscription methods. The adapter is responsible for Keycloak/OIDC authorization-code + PKCE integration or an equivalent enterprise OIDC flow.

`getSession()` returns authenticated principal identity, display name, capability claims, optional UTC expiry, and a short-lived bearer access token. The bearer token is runtime-only secret material: it is never serialized into the public session projection, DOM, logs, URL, localStorage, sessionStorage, cookies, source configuration, or frontend contracts. Refresh tokens and downstream-provider credentials are outside the shell boundary entirely.

The opaque OIDC subject is identity-mapping data, not a display-name fallback. A display claim that is empty, UUID-shaped, or equal to the subject is replaced in the UI session with a role-derived neutral label (or `Пользователь TORGNEXA` when no known role is present), so the sidebar and account profile never render the provider subject.

The shell treats missing, expired, malformed or failed sessions as anonymous. Capability checks only decide UI visibility/navigation and are never authorization evidence; every API request still relies on the server-side authenticated scope/RBAC boundary. Direct navigation to a route without its required capability fails closed in the shell.

The API client uses the generated `@torgnexa/sdk` package against same-origin `/api/v1`. A `401` triggers session re-evaluation. The browser transport rejects redirects and uses same-origin credentials semantics; client-controlled organization/workspace selectors are not synthesized.
