# ADR-0094: Operator-oriented UI and non-authoritative browser state

Status: Accepted

## Context

The ADR-0047 frontend shell established the correct security/runtime boundary, but its initial presentation remained an engineering/admin console. Navigation used placeholder glyphs, the dashboard exposed implementation facts instead of operational risk, detail screens competed with list context, loading/feedback states were minimal, and mobile navigation became effectively unlabeled. TORGNEXA now has enough runtime, reconciliation, warehouse-incident and approval data that the browser can present an operator-oriented commerce orchestration experience without inventing new backend state.

A richer frontend also creates a temptation to persist filters, tokens, tenant selectors or cached business state in browser storage. That would conflict with the existing in-memory authentication and server-authoritative tenancy model.

## Decision

Task 119 keeps React, TanStack Query, generated TypeScript SDK consumption and host-owned OIDC unchanged, but introduces a shared product-experience layer: semantic design tokens, dependency-free SVG icons, labelled responsive navigation, dark/density presentation state, structural skeletons, toast feedback, Drawer/Dialog primitives and a reusable DataTable.

The dashboard is operational rather than architectural: it derives bounded order, connector-health, synchronization, approval and warehouse-incident indicators from the same capability-authorized public APIs already used by feature pages. Inventory surfaces durable incident and fulfillment allocation lineage; synchronization surfaces drift comparison; integration configuration moves into focused drawers rather than expanding every connector card inline.

Global search is capability-aware and queries only bounded public projections for products, orders and connector accounts. It is not a new cross-tenant search service. Keyboard navigation is ignored inside editable controls.

Bookmarkable table views encode only display state (`q`, sort direction and hidden columns) in the URL. Theme/density state is process-memory presentation state. TORGNEXA does not use `localStorage`, `sessionStorage` or cookies for tokens, tenant identity, saved views or authoritative business data.

## Migration and data impact

No database migration is introduced. Browser state remains non-authoritative and disposable. The migration catalog remains at 000074.

## Compatibility impact

No OpenAPI operation, generated SDK contract, connector manifest or event schema changes. Existing routes remain stable. The UI consumes additional already-existing warehouse incident and fulfillment allocation operations.

## Security and privacy impact

Server RBAC, RLS, approval and idempotency remain authoritative. UI hiding, confirmation dialogs and activity counts are usability features rather than authorization controls. Global search is capability gated. Connector credential material remains transient form input sent to the existing SecretProvider-backed enrollment API and is not retained by the new UI framework.

Saved view URLs contain display-only filter/sort/column identifiers and may contain operator-entered search text; users control whether they bookmark/share those URLs. No tenant selector, bearer token or credential is synthesized into them.

## Operational impact

Operators receive faster risk triage: connector degradation, reconciliation drift, warehouse incidents and pending approvals are visible from the dashboard and activity center. Detail drawers preserve list context. Mobile navigation keeps labels visible. Reduced-motion and focus-visible behavior improve keyboard/accessibility operation.

The frontend repository gate gains deterministic source-level regression coverage for these UX/security invariants. A real Vite production bundle remains part of the existing JS release/supply-chain gate when installed dependencies are available.

## Consequences

TORGNEXA's web surface becomes product-oriented without creating a second state machine in the browser. URL-backed views are intentionally simpler than a server-persisted user preference system; if multi-device named views are later required, they must be added as an explicit tenant-scoped API/domain capability rather than smuggled into browser storage.

## Alternatives considered

Adding a large component framework was rejected because the required primitives are small and a new dependency would expand JS supply-chain/release review. Persisting preferences in local storage was rejected because it weakens the existing browser-state policy and complicates shared-device behavior. Adding a generic global-search backend was rejected because current bounded entity searches are sufficient for the operator shell and a real search service would require tenancy, indexing, retention and SLO design of its own.
