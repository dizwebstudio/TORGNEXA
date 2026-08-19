# ADR 0047: Frontend shell runtime and server-state boundary

Status: Accepted

## Context

The frozen developer-surface architecture already requires a React + TypeScript + Vite web UI, OIDC bearer authentication, capability-aware actions, and generated public API clients. Task 032 is the first implementation point where the shell must choose how it handles server state, routing, authentication ownership, and SDK composition without creating a second authorization model or copying internal/database contracts into the browser.

A browser shell also introduces a package ecosystem that is operationally different from the Go runtime. That ecosystem must remain subordinate to the existing release/supply-chain gate; repository implementation of the shell must not imply that Node dependency publication or public release qualification is complete.

## Decision

Use React 19 with TypeScript and Vite as the frontend runtime. Use TanStack Query v5 for remote/server-state caching and mutation invalidation; local shell state remains ordinary React state. Keep routing deliberately small and browser-native for the shell phase: navigation metadata and `history.pushState`/`popstate` form a provider-neutral route layer without adding a second router dependency before route complexity requires one.

Authentication remains host-owned. Production hosting injects a narrow OIDC adapter that returns a short-lived bearer access token in memory together with principal identity and capability claims. The shell never owns refresh tokens, downstream provider credentials, tenant selectors, or authorization decisions. Direct route access and visible navigation both fail closed when the session lacks the required UI capability, while the Go API remains the authoritative RBAC/tenant boundary.

All REST traffic uses the generated `@torgnexa/sdk` package from Task 062 against same-origin `/api/v1`; frontend code must not copy generated clients or internal database models. Catalog, Orders, and Notifications are the first API-backed screens. Other frozen navigation areas may render capability-aware placeholders until their atomic tasks provide business behavior.

## Consequences

The shell gets one consistent server-state lifecycle and a stable generated client boundary while avoiding duplicate endpoint models. Auth and tenant scope remain outside browser persistence. Capability metadata becomes centralized and testable, including direct-route denial instead of navigation-only hiding.

The repository can validate pure shell logic and TypeScript integration offline, but a production Vite bundle still requires the pinned Node dependency graph to be installed in a dependency-enabled CI/staging environment. Node package-manager supply-chain registration remains part of the earlier Task 065 release gate and must fail closed before publication if the ecosystem is not fully locked/scanned.

## Alternatives considered

Embedding Keycloak-specific login code and refresh-token storage directly in the shell was rejected because it would couple the UI to one identity implementation and expand browser secret exposure. Hand-written fetch clients were rejected because Task 062 already defines the generated OpenAPI client surface. Redux was rejected for the shell because current state is primarily remote/server state and does not justify a second global state model. Adding React Router immediately was rejected because the current route graph is flat and browser-native routing is sufficient; a later change may add a router through architecture review when nested/data routes justify it.

## Compatibility impact

No REST, webhook, event, protobuf, connector SDK, or database contract is changed. A new public frontend session projection contract documents only non-secret principal/capability metadata. The UI consumes existing OpenAPI operations through the generated TypeScript SDK.

## Migration and data impact

No database migration is introduced. Browser state is non-authoritative and no application data is persisted in localStorage/sessionStorage by the shell. Deployment needs SPA fallback routing and same-origin `/api/v1` reverse proxying.

## Security and privacy impact

Bearer access tokens remain memory-only and are excluded from public session projection, DOM, URL, logs, local/session storage and source configuration. Refresh tokens and downstream provider credentials are not part of the shell contract. Missing/expired/malformed sessions and missing capabilities fail closed. UI capability checks are presentation controls only; server-side OIDC/RBAC, authenticated tenant resolution, RLS and approval policies remain authoritative.

The API transport is same-origin, rejects redirects, and does not synthesize organization/workspace query parameters. Frontend static checks reject browser-persistence APIs, client tenant selectors, copied generated clients, and provider-specific marketplace branches in the shell.

## Operational impact

`make frontend-check` performs deterministic logic tests, repository TypeScript validation and static security checks without requiring downloaded npm packages; when `frontend/node_modules` exists it also executes the real Vite production build. Dependency-enabled CI/staging must install the exact package versions and run the production build before deployment. Task 065 remains the authority for lockfile/scanner/SBOM/license qualification and public-release readiness of the Node ecosystem.
