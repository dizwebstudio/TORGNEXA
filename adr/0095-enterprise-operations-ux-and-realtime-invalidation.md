# ADR-0095: Enterprise operations UX and metadata-only realtime invalidation

Status: Accepted

## Context

Task 119 made the frontend operator-oriented, but the primary Catalog and Orders tables still behaved like browser-local datasets, the dashboard derived business KPIs from bounded list samples, incident evidence was spread across several screens, entity drawers were not durable URLs, command search only inspected preloaded rows, and operational changes required manual/polling refresh. These limits become misleading at enterprise data volumes and during warehouse/connector incidents.

The backend already owns tenant-scoped PostgreSQL cursor search for products/orders, replay-safe ClickHouse reporting projections, durable warehouse/reconciliation/approval state and immutable audit records. The frontend should compose those authoritative capabilities instead of creating a browser-side index, event store or analytics truth.

## Decision

Task 120 introduces five coordinated operator-surface changes.

First, Catalog and Orders use a controlled `ServerDataGrid`: query, status filter and cursor pagination are sent to the existing PostgreSQL search endpoints. The grid intentionally preserves canonical backend ordering; arbitrary browser sort is not presented as authoritative when it is not part of the cursor contract.

Second, the API exposes protected `GET /api/v1/realtime` (`operations.realtime.read`). It is a long-lived SSE invalidation channel over the normal authentication/tenant/authorization composition. The server polls the tenant-scoped immutable audit head and emits only `{reason,cursor,at}` metadata. It never streams audit summaries, entity payloads, connector credentials or tenant selectors. The browser reacts to explicit `invalidate` frames by invalidating TanStack Query entries and rereading normal capability-protected APIs.

Third, `/incidents` composes warehouse incidents, open reconciliation drift, degraded connectors and pending approvals into one operator queue. It does not persist a new incident model; each source domain remains authoritative.

Fourth, entity drawers become route-controlled (`/catalog/{id}`, `/orders/{id}`, `/incidents/{kind}/{id}`). The Command Palette performs debounced server searches for products/orders. TORGNEXA deliberately does not create one universal global-search endpoint with a single broad permission because doing so could weaken future fine-grained authorization across product, order and connector domains.

Fifth, Reports use an accessible dependency-free SVG chart primitive with period presets and KPI summaries, and Dashboard order/GMV KPIs are read from the existing replay-safe reporting projection rather than a bounded order-list page.

## Migration and data impact

No database migration is introduced. Migration history remains at `000074`. No new browser-persisted state is introduced.

## Compatibility impact

The change is additive: `GET /api/v1/realtime` adds `streamRealtimeInvalidations`; OpenAPI moves from 0.14.0 to 0.15.0 and generated SDKs expose 108 operations. Existing product/order cursor contracts remain compatible.

## Security and privacy impact

Realtime remains inside the production protected-route composition and requires a read capability. SSE content is metadata-only and is never treated as authorization evidence. Browser refetches still pass OIDC, canonical tenant resolution, authorization and RLS. Deep links carry entity IDs only and cannot bypass server permissions. No local/session storage is added for auth or tenant data.

A dedicated unified global-search endpoint was rejected because a single route permission could expose entities whose original domain permissions differ. The palette therefore fans out to already-authorized server endpoints.

## Operational impact

Operators can work on cursor-sized pages regardless of tenant dataset size, receive near-realtime invalidation, triage one incident queue, copy durable entity links and use reporting-backed KPIs. Realtime is deliberately invalidation-based rather than payload replication, reducing browser consistency and privacy risk.

The audit-head poll gives low-latency refresh for audited API mutations. Heartbeats provide connection liveness without triggering data reads. If worker-originated notifications become an SLO later, a domain-specific invalidation source or durable event-to-realtime gateway may be introduced under a separate event-platform review rather than coupling browser delivery directly to Kafka.

## Testing and rollback

Frontend regression coverage includes a deterministic event-classification test proving that heartbeat and ready frames do not invalidate queries. A focused API handler test proves that a scoped realtime stream emits a ready frame and does not leak raw audit content. The generated SDK gate confirms the additive operation/version. Rollback is the previous API/frontend pair; there is no schema rollback.

## Consequences

Task 120 removes sample-based daily UX without inventing a second source of truth. The trade-off is that Incident Center still composes bounded existing domain reads and SSE is near-realtime invalidation, not a business event feed. Those constraints are explicit and safer than silently broadening permissions or exposing Kafka/audit payloads to browsers.

## Alternatives considered

A large third-party grid/chart suite was rejected to avoid expanding JS supply-chain scope for functionality that can be implemented with current primitives. WebSocket payload replication was rejected because it would require another authorization/data-retention boundary. Browser-side global indexing was rejected because it cannot be complete or tenant-safe at enterprise volume. A single broad global-search endpoint was rejected because it would collapse domain-specific authorization.
