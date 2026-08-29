# Frontend Architecture

Runtime: React 19 + TypeScript + Vite. ADR-0047 selects TanStack Query v5 for remote/server state and a browser-history route layer for the initial flat shell. A later router dependency requires architecture review when route complexity justifies it.

Primary areas: Dashboard; Catalog; Orders; Inventory; Connectors; Sync/Reconciliation; Social Calendar; Campaigns; Reports; Approvals; Compliance; Fulfillment/PUDO; Integrations; Audit; Settings.

Frontend never embeds downstream provider tokens. Production identity is supplied through the host-owned OIDC adapter; bearer and refresh tokens stay only in adapter memory and the Go API remains the authoritative authorization/tenant boundary. The adapter renews short-lived access tokens before expiry and may recover an existing provider SSO session through a hidden authorization-code + PKCE `prompt=none` flow. No token is written to browser storage, cookies, URLs, logs or the DOM. The shell calls TORGNEXA through the Task-062 generated TypeScript SDK and displays capability-aware actions/routes. The Settings profile uses `/api/v1/me/profile` for versioned personal fields and avatar upload/removal; passwords and provider-owned identity remain in Keycloak. A user can submit export or deletion requests for their own profile into the durable privacy workflow, while administrators can review and edit workspace-member profile fields. Sensitive actions show risk/approval status when their business screens are implemented.

Task 032 provides real API-backed Catalog, Orders and Notifications screens plus guarded placeholders for later atomic domains. See `docs/69-frontend-shell.md` and `contracts/frontend/auth-adapter-v1.md`.

Route screens and public documentation are loaded with React `lazy`/`Suspense`.
The initial shell therefore does not download heavy Settings, Integrations,
Catalog or documentation modules before they are opened. The production build
keeps the critical JavaScript chunk below 250 KiB; the loading fallback is
limited to the content area so navigation remains responsive while a route
chunk arrives.
