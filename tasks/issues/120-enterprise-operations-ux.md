# Task 120: Enterprise Operations UX

## Status
`done` — repository implementation complete on 2026-08-18.

## Objective
Turn the Task 119 operator shell into an enterprise operations surface that remains useful at production data volumes and during active incidents: server-owned list queries, near-realtime invalidation, one incident queue, durable deep links/global search and analytics backed by the reporting projection.

## Deliverables

### 1. Server-side DataGrid
- [x] add a controlled `ServerDataGrid` primitive with server-owned query/filter/cursor state;
- [x] move Catalog to PostgreSQL-backed `q`/status/cursor pages (`limit=25`);
- [x] move Orders to PostgreSQL-backed `q`/status/cursor pages (`limit=25`);
- [x] preserve explicit previous-cursor history in the UI without materializing the complete tenant dataset;
- [x] keep canonical backend ordering rather than pretending arbitrary client sort is authoritative.

### 2. Realtime operations
- [x] add protected `GET /api/v1/realtime` as an authenticated SSE invalidation channel;
- [x] stream metadata-only `ready`/`invalidate`/`heartbeat` frames, never entity/audit payloads;
- [x] drive invalidation from the tenant-scoped PostgreSQL audit watermark and retain a heartbeat fallback for non-audited worker-originated state;
- [x] reconnect with bounded backoff and refetch through the normal generated/API capability boundary;
- [x] show live/connecting/offline state in the application shell.

### 3. Incident Center
- [x] add `/incidents` and deep incident routes;
- [x] aggregate warehouse incidents, open reconciliation drift, degraded connector accounts and pending approvals;
- [x] normalize severity, status, entity, opened time and next-action guidance;
- [x] expose reroute/attention evidence for warehouse incidents and local/remote comparison for drift;
- [x] keep source APIs/RBAC authoritative instead of introducing a browser incident database.

### 4. Deep links and global server search
- [x] make `/catalog/{id}` and `/orders/{id}` durable route-controlled drawers backed by direct entity reads;
- [x] add `/incidents/{kind}/{id}` links for operator handoff/bookmarks;
- [x] make Command Palette product/order search execute debounced server queries instead of searching a preloaded sample;
- [x] preserve capability-aware navigation and same-origin authenticated API access;
- [x] do not add a universal backend endpoint that could collapse distinct product/order/connector permissions into one weaker authorization decision.

### 5. Professional analytics
- [x] replace CSS/div charts with an accessible dependency-free SVG analytics component;
- [x] add 7/30/90-day presets, KPI summaries, multiple series, grid/legend and point tooltips;
- [x] make the Dashboard order/GMV KPI use the replay-safe reporting projection rather than the first 100 orders;
- [x] retain report export/filter behavior and capability gating.

## Safety invariants
- server-side grid/search remains tenant-scoped through existing PostgreSQL RLS and authenticated API composition;
- SSE sends only invalidation metadata and never raw business/audit records;
- browser realtime signals are hints only: normal APIs remain the source of truth and authorization boundary;
- no token, tenant identifier, credential or business payload is persisted in browser storage;
- deep links do not bypass backend RBAC/RLS;
- Incident Center composes existing authorized projections and does not create a second workflow/state machine;
- analytics claims are limited to the reporting projection and selected period;
- Task 120 introduces no database migration or event-schema change.

## Contract impact
- additive OpenAPI operation: `streamRealtimeInvalidations`;
- OpenAPI package version: `0.15.0`;
- generated Go/Python/TypeScript SDK operation count: `108`;
- migration count remains `74`, latest `000074`.

## Acceptance
- `./scripts/check-frontend-shell.sh` passes with 23 deterministic UX tests;
- generated SDK gate passes with 108 operations / OpenAPI 0.15.0;
- JS supply-chain and Community deployment policies remain green;
- architecture review covers the exact Task 119 → Task 120 diff;
- the realtime handler has a repository test proving metadata-only streaming and tenant scope;
- full Go 1.26.5 API test execution remains a release-host qualification when the pinned toolchain/modules are available.
