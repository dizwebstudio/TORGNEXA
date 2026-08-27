# Frontend Auth Adapter v1

The React shell does not implement a second identity system. Production hosting provides `window.__TORGNEXA_AUTH_ADAPTER__` with `getSession`, `login`, `logout`, and optional change subscription methods. The adapter is responsible for Keycloak/OIDC authorization-code + PKCE integration or an equivalent enterprise OIDC flow.

`getSession()` returns authenticated principal identity, display name, capability claims, optional UTC expiry, and a short-lived bearer access token. `getSession({forceRefresh: true})` asks the host adapter to renew the access token before retrying a failed authenticated request. The bearer token is runtime-only secret material: it is never serialized into the public session projection, DOM, logs, URL, localStorage, sessionStorage, cookies, source configuration, or frontend contracts. A host adapter may retain an OIDC refresh token only inside its in-memory closure; it never crosses into React session state or browser persistence. Downstream-provider credentials remain outside the shell boundary entirely.

The Community adapter renews the access token before expiry. On a fresh page load it uses authorization-code + PKCE with `prompt=none` and the static `/oidc/silent-callback.html` redirect to recover an existing provider SSO session without persisting tokens. A missing provider session resolves as anonymous; invalid state, malformed tokens and unexpected provider failures fail closed.

The opaque OIDC subject is identity-mapping data, not a display-name fallback. A display claim that is empty, UUID-shaped, or equal to the subject is replaced in the UI session with a role-derived neutral label (or `Пользователь TORGNEXA` when no known role is present), so the sidebar and account profile never render the provider subject.

The shell treats missing, expired, malformed or failed sessions as anonymous. Capability checks only decide UI visibility/navigation and are never authorization evidence; every API request still relies on the server-side authenticated scope/RBAC boundary. Direct navigation to a route without its required capability fails closed in the shell.

The API client uses the generated `@torgnexa/sdk` package against same-origin `/api/v1`. A `401` triggers one forced renewal and one replay with the replacement access token. A second `401` clears the local application session; requests never enter an unbounded authentication retry loop. The browser transport rejects redirects and uses same-origin credentials semantics; client-controlled organization/workspace selectors are not synthesized.
