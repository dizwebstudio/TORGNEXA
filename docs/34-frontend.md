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

### Public documentation rendering and indexing

The public `/docs` hub and its 17 topical routes are prerendered during
`npm run build` into `frontend/dist/docs/**/index.html`. Each page contains its
own content, H1, title, description, canonical URL, Open Graph metadata,
`TechArticle` and `BreadcrumbList` JSON-LD before the browser executes
JavaScript; the small static server serves each directory index before falling
back to the authenticated SPA. Native anchors and `<details>` blocks keep the
public pages useful without a module bundle.

The production overlay requires `TORGNEXA_PUBLIC_URL` and bakes that HTTPS URL
into the canonical metadata, `robots.txt` and `sitemap.xml`. When the content or
public host changes, rebuild the frontend and run `npm run test:docs` from
`frontend/`; the check verifies all 18 rendered URLs, unique metadata,
breadcrumbs, indexing policy and absence of the SPA module from public HTML.

### Public documentation reading quality

Every topical page starts with a compact reader guide: who the page is for,
what must be ready before starting, what result to expect and where to go next.
The overview also contains a plain-language glossary for terms such as
`кабинет`, `возможность`, `ATP`, `сверка` and `idempotency key`. This keeps
technical constraints understandable without removing the exact API and
security terminology from the detailed sections.

Screenshots use one `DocsScreenshot` component with explicit dimensions,
descriptive Russian `alt` text, captions, asynchronous decoding and lazy
loading. When an interface, connector drawer or smoke-test flow changes, update
the corresponding image and caption in `PublicDocumentationPage.tsx`; do not
add an unlabelled image directly to the page.

The troubleshooting page keeps its six questions in one source used both by
the visible FAQ and by `FAQPage` JSON-LD. The static documentation check
asserts that topical guides, FAQ markup and screenshot accessibility attributes
are present, so a content edit cannot silently remove the reader or crawler
layer.
