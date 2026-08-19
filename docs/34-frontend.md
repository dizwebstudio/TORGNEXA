# Frontend Architecture

Runtime: React 19 + TypeScript + Vite. ADR-0047 selects TanStack Query v5 for remote/server state and a browser-history route layer for the initial flat shell. A later router dependency requires architecture review when route complexity justifies it.

Primary areas: Dashboard; Catalog; Orders; Inventory; Connectors; Sync/Reconciliation; Social Calendar; Campaigns; Reports; Approvals; Compliance; Fulfillment/PUDO; Integrations; Audit; Settings.

Frontend never embeds downstream provider tokens. Production identity is supplied through the host-owned OIDC adapter; bearer access tokens stay in memory and the Go API remains the authoritative authorization/tenant boundary. The shell calls TORGNEXA through the Task-062 generated TypeScript SDK and displays capability-aware actions/routes. Sensitive actions show risk/approval status when their business screens are implemented.

Task 032 provides real API-backed Catalog, Orders and Notifications screens plus guarded placeholders for later atomic domains. See `docs/69-frontend-shell.md` and `contracts/frontend/auth-adapter-v1.md`.
