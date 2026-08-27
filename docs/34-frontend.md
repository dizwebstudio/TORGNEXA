# Frontend Architecture

Runtime: React 19 + TypeScript + Vite. ADR-0047 selects TanStack Query v5 for remote/server state and a browser-history route layer for the initial flat shell. A later router dependency requires architecture review when route complexity justifies it.

Primary areas: Dashboard; Catalog; Orders; Inventory; Connectors; Sync/Reconciliation; Social Calendar; Campaigns; Reports; Approvals; Compliance; Fulfillment/PUDO; Integrations; Audit; Settings.

Frontend never embeds downstream provider tokens. Production identity is supplied through the host-owned OIDC adapter; bearer and refresh tokens stay only in adapter memory and the Go API remains the authoritative authorization/tenant boundary. The adapter renews short-lived access tokens before expiry and may recover an existing provider SSO session through a hidden authorization-code + PKCE `prompt=none` flow. No token is written to browser storage, cookies, URLs, logs or the DOM. The shell calls TORGNEXA through the Task-062 generated TypeScript SDK and displays capability-aware actions/routes. Sensitive actions show risk/approval status when their business screens are implemented.

Task 032 provides real API-backed Catalog, Orders and Notifications screens plus guarded placeholders for later atomic domains. See `docs/69-frontend-shell.md` and `contracts/frontend/auth-adapter-v1.md`.
