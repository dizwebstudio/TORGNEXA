# Frontend Shell

Task 032 implements the first TORGNEXA web surface under ADR-0047. The shell is React + TypeScript + Vite, with TanStack Query as the remote/server-state layer and a small browser-history router for the initial flat route graph.

## Identity boundary

The shell does not own identity. Hosting supplies `window.__TORGNEXA_AUTH_ADAPTER__`; production adapters integrate Keycloak/OIDC or another supported enterprise OIDC provider. The adapter returns short-lived access-token material only to the in-memory React auth context. It also returns principal identity, role and capability claims used for authorization and presentation. The opaque OIDC subject remains internal to the session projection and is not rendered in the sidebar or account profile. It is never used as a display-name fallback: an empty, UUID-shaped or subject-equal display claim becomes a role-derived neutral label. The token is not copied into the public UI-session projection or browser persistence. Task 098 adds optional provider-owned account management: the local Keycloak adapter opens the realm account console for password/security changes, while TORGNEXA never accepts password material. Non-loopback account-management issuers must use HTTPS and cannot contain credentials, query parameters or fragments.

The account profile presents the provider-owned `picture`, `email`, `birthdate`, `job_title`/`position`, `department` and `phone_number` claims when available, with initials and explicit “not specified” states as safe fallbacks. The demo Keycloak user supplies synthetic values and a local avatar so the Community contour remains usable offline. These fields stay in the in-memory session and are not persisted by TORGNEXA.

Missing, expired or invalid session state is anonymous. Missing route capability is denied both in navigation and direct URL access. These checks do not authorize API calls: the Go API remains responsible for bearer verification, RBAC/scopes, tenant/workspace derivation, approval policy and RLS.

## Generated API boundary

`frontend/src/api/client.ts` creates `@torgnexa/sdk` from Task 062 at same-origin `/api/v1`. A 401 causes session re-evaluation. Redirects are rejected. No `organization_id` or `workspace_id` selectors are constructed by the shell.

The first real screens are:

- Catalog → `listProducts`;
- Orders → `listOrders`;
- Notifications → `listNotifications` / `markNotificationRead`.
- Settings → tabbed account/workspace administration plus a dedicated `Каналы и важность` notification-preference tab; current OIDC principal, roles/capabilities, session expiry and provider-owned password/security management remain under `Основные`.

Task 104 adds the Settings integration catalog as a deterministic, non-secret projection of canonical `connectors/*/*/manifest.json` files. Task 130 adds a second generated input, `contracts/connectors/builtin-runtime-support-v1.json`, so the UI distinguishes declared SDK coverage from operations that the current production runtime can actually execute. Ready connectors expose only their exact working capabilities and product-sync directions. Planned connectors remain discoverable but cannot create an account, enable a capability or start synchronization. AI connectors direct operators to the dedicated AI-provider settings instead of pretending to be generic commerce accounts. `scripts/generate-frontend-connector-catalog.py --check` fails when any manifest, runtime declaration, TypeScript projection or Go admission table drifts. Tasks 105–107 add authenticated tenant-scoped account creation, credential enrollment, PKCE OAuth, real remote health and cabinet-specific capability settings. Authorization-code manifests show a provider-neutral OAuth action; client-credentials manifests remain server-only. The exact `/oauth/connectors/callback` page sends code/state to the authenticated API, removes them from browser history after success and never receives downstream tokens. Every account capability starts denied; the UI displays host-owned read/write and approval classification and saves an exact runtime-supported selection. Task 108 adds per-cabinet dry-run evidence, one-time initial-import dispatch, durable incremental/full interval schedules and observable job progress. These controls are available both in the integration account card and on the main Synchronization page next to manual policy operations; the browser stores no authoritative schedule. Task 109 adds the bounded health/reauthorization history and remediation controls; it is repository-complete and integrated into Settings. Task 134 makes access-token renewal host-owned: the browser repeats OAuth only when health reports `oauth_reauthorization_required`, not for routine token expiry.

Task 131 adds `finance` as a working separate surface. The CBR FX card explains
that the source is public and worker-managed, does not offer a tenant cabinet or
credential form, and sends the operator to `/finance`, where persisted official
rates are displayed. Separate-surface copy is selected from generated runtime
metadata rather than from a provider-ID conditional.

Bitrix24 is now a working `crm` separate surface. Its catalog drawer supports
the normal tenant-scoped account lifecycle, OAuth sign-in and reauthorization,
the non-secret `portal_host` runtime configuration, health checks and exact CRM
capability selection. It deliberately does not appear in generic product
sync: CRM entity and product-row operations stay behind the provider-neutral
CRM registry bridge and the four declared CRM capabilities.

Responses remain `unknown` at the generated transport boundary today, so the shell applies small bounded decoders based on public JSON Schema before rendering. Other architecture areas are capability-aware placeholders until their atomic business tasks land.

## Local validation

`make frontend-check` runs TypeScript logic compilation, Node tests, repository TSX validation and static security checks without downloaded npm packages. If `frontend/node_modules` is present, the same target also runs the actual `tsc --noEmit && vite build` production bundle.

The sandbox used for Task 032 cannot reach the npm registry, so a real dependency install/build is not claimed here. Task 065 already requires any supported package ecosystem to have lockfile/scanner/license/SBOM policy before release. This operational release gate remains fail-closed and does not turn the repository shell into a release candidate.

## Hosting

Production hosting needs SPA fallback routing, strict security headers/CSP at the reverse proxy, same-origin `/api/v1`, and the OIDC adapter bootstrap before the application module runs. Downstream provider tokens are never frontend configuration.

## Task 119 — operator product experience

Task 119 keeps the ADR-0047 runtime/security boundaries but replaces the original admin-shell presentation with an operator-oriented UI layer.

The primary dashboard now answers operational questions rather than exposing implementation details: current order volume, connector degradation, reconciliation drift, warehouse incidents, pending approvals, and a channel → orchestration → fulfillment state flow. First-run onboarding is derived from the existing connector, warehouse and synchronization APIs; it is not persisted as a browser-side source of truth.

The shared UI layer now includes semantic design tokens, dependency-free SVG icons, light/dark appearance, comfortable/compact density, skeleton loading, toast feedback, Drawer/Dialog primitives, `focus-visible`, reduced-motion support, labelled mobile navigation and keyboard navigation. `Cmd/Ctrl+K` opens a capability-aware command palette that can search permitted navigation plus bounded public product/order/connector projections. No global search endpoint or cross-tenant index is introduced.

`DataTable` provides client-side search, sort, pagination, row selection and column visibility for bounded API pages. Saved views are bookmarkable URL state (`q`, sort direction and hidden-column identifiers); TORGNEXA deliberately does not use local/session storage or cookies for these views.

Daily workflows are focused rather than nested inline: product, order and inventory details use drawers; synchronization drift has a side-by-side state comparison; Inventory exposes persistent warehouse incident counters and fulfillment replacement lineage; Integration Catalog is an overview first, with account credentials/capabilities/bootstrap controls moved into a focused connector drawer. Existing API idempotency, approval and RLS boundaries remain authoritative.

`frontend/test/ui-experience.test.mjs` prevents regression of these UX/security invariants in the repository-only frontend gate.

## Task 120 — Enterprise Operations UX

The operator shell no longer treats bounded API pages as the complete tenant dataset. Catalog and Orders use `ServerDataGrid`, which sends text/status filters and opaque cursors to the existing PostgreSQL search APIs. Canonical backend order is preserved; the browser does not advertise unsupported arbitrary server sorts.

`GET /api/v1/realtime` is an authenticated SSE **invalidation** channel. Frames contain only liveness/change metadata. The browser invalidates TanStack Query data only for explicit `invalidate` frames and rereads the same capability-protected APIs used by normal navigation. `ready` and `heartbeat` frames report connection health only and never invalidate the query cache. This avoids duplicating authorization or business state in the streaming layer and prevents periodic refetch storms.

`/incidents` composes warehouse incidents, open reconciliation drift, degraded connector accounts and pending approvals into one triage surface. `/catalog/{id}` and `/orders/{id}` are durable route-controlled drawers; incident rows also receive bookmarkable routes. `Ctrl/Cmd+K` sends product/order searches to server endpoints rather than searching a fixed browser sample.

Reports use the dependency-free `AnalyticsChart` SVG primitive with 7/30/90-day presets, KPI summaries and accessible point labels. Dashboard order/GMV cards use the replay-safe reporting projection when the caller has `reports.read`; they no longer total the first 100 orders.

Task 120 adds no browser persistence and no tenant selector. OpenAPI adds only `streamRealtimeInvalidations` and moves to 0.15.0 / 108 generated operations; database migrations remain at 74.
