# Task 119: UI Product Experience Closure

## Status
`done` — repository implementation complete on 2026-08-18.

## Objective
Raise the TORGNEXA frontend from an engineering/admin shell to an operator-oriented commerce orchestration product without weakening tenant, capability, secret or generated-SDK boundaries.

## UI P0 — daily usability
- [x] replace navigation dots/letter glyphs with a dependency-free SVG icon system;
- [x] repair mobile navigation so every icon remains labelled and reachable;
- [x] replace internal-tech dashboard metrics with orders, connector health, reconciliation drift, warehouse incidents and human-attention KPIs;
- [x] add a live commerce-orchestration flow summary;
- [x] add global toast feedback with polite `aria-live` delivery;
- [x] add structural skeleton loading states;
- [x] add Drawer and Dialog primitives with Escape/backdrop behavior;
- [x] introduce semantic design tokens, focus-visible treatment and dark mode;
- [x] support reduced motion and responsive mobile layouts.

## UI P1 — operator workflows
- [x] add a reusable DataTable with search, sorting, pagination, row selection, configurable columns and bookmarkable views;
- [x] move Catalog and Order detail into focused drawers rather than inline pages;
- [x] move inventory position detail/history into a drawer;
- [x] expose warehouse incidents with routing/reroute/attention counters;
- [x] expose durable fulfillment-allocation replacement lineage;
- [x] expose reconciliation drift comparison in a focused drawer;
- [x] redesign Integration Catalog into overview cards plus per-connector/per-account setup drawer;
- [x] humanize connector capabilities while retaining exact manifest values for server writes;
- [x] add confirmation before executing a previously approved sensitive operation;
- [x] preserve existing report charts/exports and normalize report iconography.

## UI P2 — product quality
- [x] add first-run onboarding derived from real connector/warehouse/sync state;
- [x] add `Ctrl/Cmd+K` command palette and capability-aware global search across navigation, products, orders and connector accounts;
- [x] add keyboard `G <key>` navigation shortcuts outside editable controls;
- [x] add an Activity Center combining unread notifications and pending approvals;
- [x] add light/dark theme toggle without persisting tenant data in browser storage;
- [x] add comfortable/compact density toggle;
- [x] implement bookmarkable saved views through URL query state rather than local/session storage;
- [x] add UI experience regression tests for navigation, operational dashboard, tables, incidents, integration drawers, accessibility and responsive theming.

## Safety invariants
- no `organization_id` or `workspace_id` browser selector is added;
- no token, connector credential or tenant payload is written to `localStorage`, `sessionStorage` or cookies;
- global search uses only capability-authorized public API methods;
- server-side RBAC/RLS/approval remains authoritative; UI hiding is presentation only;
- connector provider branching remains generated-manifest data rather than handwritten provider logic;
- dangerous operations still require existing server approval/idempotency semantics; Dialog is an extra UX guard, not an authorization boundary;
- saved views contain only display filter/sort/column state and are bookmarkable URLs, not authoritative user data.

## Acceptance
- `./scripts/check-frontend-shell.sh` passes including all UI experience tests;
- `tsc -p frontend/tsconfig.repository.json` passes;
- connector catalog generation remains deterministic for 32 connectors;
- frontend static policy continues to reject browser token/tenant persistence and handwritten provider-specific branching;
- no database migration, OpenAPI contract or generated SDK operation is added by Task 119;
- P4 go-live/release qualification semantics remain unchanged.
