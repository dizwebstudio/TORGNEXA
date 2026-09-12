# ADR-0196 — Tenant-scoped SSE broadcaster and bounded fan-out

Status: Accepted

## Context

ADR-0095 introduced a metadata-only realtime stream by polling the newest
tenant audit identifier every two seconds. Every connected browser performed
that query independently. Database work therefore grew with client count, and
the process had no explicit limit for open streams or queued invalidations.
ADR-0189 bounded socket writes and ADR-0190 added periodic authorization, but
neither changed this fan-out topology.

Reconnects are expected and intermediate invalidation cursors are not replay
evidence. The browser always refreshes authorized APIs after connection, while
an audit invalidation only means that the newest authoritative state should be
read. Retaining every intermediate signal would add memory and backpressure
without improving correctness.

## Decision

Create one process-local watcher for each organization/workspace that has at
least one admitted SSE client. Concurrent first clients share the same initial
audit-head lookup. The watcher then performs one bounded lookup every two
seconds and fans a changed opaque cursor out to all admitted clients for that
tenant. A watcher is removed and its context is cancelled as soon as its last
client leaves. Multiple API replicas each keep their own watcher for tenants
connected to that replica; database query load scales with active
tenant/replica pairs rather than browsers.

The audit repository remains the durable source of the signal and uses the
existing forced-RLS, tenant-scoped newest-ID query. The broadcaster stores only
the current cursor. It does not read or retain audit summaries, business
payloads or event history. SSE remains a best-effort metadata hint; clients
re-read normal authorized APIs and the connected invalidation covers any gap.

Admit at most 64 clients for one tenant and 1024 clients in one API process.
Reject excess clients before SSE headers with HTTP 429 and `Retry-After: 5`.
Each admitted client has a one-element notification queue. If another audit
change arrives while that slot is occupied, replace the pending cursor with
the newest cursor. This level-triggered coalescing is safe because clients do
not replay intermediate cursors. ADR-0189's five-second frame deadline still
disconnects a client whose network writer cannot make progress.

The broadcaster exposes a label-free process snapshot: active/peak clients,
active tenants, accepted and rejected clients, audit-head query/error counts,
published signals, queued deliveries and coalesced deliveries. Tenant,
workspace, identity, cursor and error values are never metric labels.
Per-connection periodic authorization from ADR-0190 remains independent and
authoritative because identity, session and membership cannot be shared across
clients.

## Compatibility and rollout

There is no database migration, durable schema, event schema or SSE payload
change. HTTP 429 is an additive pre-stream response. OpenAPI documents the
fixed limits and coalescing behavior; generated SDK contract hashes are
refreshed. A rolling deployment is data-compatible, although old replicas
continue one audit-head query per connected client until drained.

The broadcaster is deliberately process-local. Introducing Valkey Pub/Sub as
the sole signal path would lose durable signals during disconnects, while
connecting browsers directly to Kafka would create a new authorization and
data-exposure boundary. A future event-to-realtime gateway may wake these
watchers, but audit/outbox durability and reconnect refresh must remain the
recovery path.

## Security, privacy and failure behavior

Only canonical tenant scope from the protected route selects a watcher.
Authentication, tenant resolution and permission checks occur before
admission, and open streams retain ADR-0190 reauthorization. Tenant keys are
bounded UUID pairs held only in process memory. A signal crosses neither a
tenant boundary nor an API replica boundary.

Audit lookup failure emits no frame and is counted; the next bounded poll can
recover. It does not grant access or substitute cached business data. Initial
lookup failure still permits the connected refresh, which rereads authorized
APIs and preserves the previous availability behavior. Query contexts time out
after five seconds. Client queues never block the tenant watcher and cannot
grow beyond one item.

## Validation

Race tests cover tenant/process admission, HTTP 429 before stream start,
one-element newest-cursor coalescing, slow-writer cleanup and tenant watcher
lifecycle. A reconnect-storm profile runs six waves of 48 concurrent handlers:
288 connections produce six initial audit-head calls, one per wave, and leave
zero active clients/watchers after cancellation.

The forced-RLS PostgreSQL profile attaches 32 concurrent SSE handlers to one
tenant. They produce one initial newest-ID query. After one immutable audit
record is appended, one more query fans the invalidation to all 32 clients;
the measured total is two queries rather than 64. Existing real HTTP/1.1,
HTTP/2, net.Pipe backpressure, reconnect and periodic authorization scenarios
remain in the same regression gate.
