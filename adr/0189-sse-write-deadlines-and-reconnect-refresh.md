# ADR-0189 — SSE write deadlines and reconnect refresh

Status: Accepted

## Context

Audit A10 reproduced an SSE connection losing frames after the ordinary HTTP
WriteTimeout. The default response deadline was 10 seconds, before the first
15-second heartbeat. Calling Flusher.Flush discarded delivery errors and let
failed streams continue polling. A reconnect started at the current audit head;
the ready frame alone did not make cached data fresh after a disconnected gap.

## Decision

Only after the existing auth/tenant/authz composition admits the realtime route,
use http.ResponseController through the existing ResponseWriter Unwrap chain.
Clear the ordinary response write deadline. Each frame receives a five-second
write-and-flush deadline, which is cleared after successful flush while waiting
for the next poll/heartbeat. End the handler on context cancellation, deadline
control failure, write failure or Flush error. Keep the common HTTP server
timeouts and all other routes unchanged.

Require streaming and write-deadline support; do not silently run an unbounded
writer. Before starting SSE, unsupported controls yield 501 and another control
error yields a generic 503. After streaming starts, failure closes the stream
instead of appending a JSON error to it. A slow writer retains its deadline
when the handler exits; clearing a failed write deadline could let the server's
final flush block again.

Each connection emits ready followed by an explicit invalidate frame with
reason connected and the same optional audit cursor. This requests one refresh
through normal authorized APIs, including after any disconnected gap, unchanged
audit head or absent audit history. Cursor remains an opaque baseline, not a
replay token; Last-Event-ID does not resume a durable history. Audit changes
still emit invalidate/reason audit. Heartbeats remain liveness-only.

Keep the existing frontend event parser and 150 ms coalescing. A connected
invalidation works with already deployed clients that understand invalidate,
without making every heartbeat trigger API reads. A burst requests one cache
invalidation, and session retirement cancels its pending work.

## Compatibility and migration

No migration, new dependency, durable field, browser persistence, event-bus
schema or public API/SDK signature change. OpenAPI describes the stream frames,
per-write deadline and pre-stream error statuses; generated SDK source hashes
are refreshed. Update every API instance: old instances can still interrupt
SSE and omit the connected invalidation. No new frontend runtime is required.

The additional invalidate frame can cause one extra refresh on first connect
and token-renewal reconnect. This is deliberate recovery work; heartbeat-driven
periodic refetch is not introduced. The stream remains a best-effort signal to
re-read authoritative APIs, not a source of business state or a durable replay
feed. Existing API error/retry handling still applies if the refetch fails.

## Security, privacy and operational scope

The read capability and canonical tenant context are checked before deadline
changes. SSE carries only reason/cursor/at, no entity/audit payload, credentials
or client-selected tenant. API reads after invalidation retain their normal
authorization and RLS. Application-level slow writes are bounded; intermediate
proxy buffering/idle timeouts require deployment qualification.

Connection establishment retains the existing authentication policy. Ongoing
revalidation of an already open stream on token expiry, session revocation or
permission changes is a separate lifecycle follow-up in Task 234.7. This change
does not claim instantaneous revocation of previously authorized streams.

Per-tenant fan-out, connection limits, shared watchers and production load
qualification remain Task 234.7; the existing indexed audit-head polling stays
in place. There is no production capacity claim or deployment in this change.

## Validation

Before the fix, real TLS HTTP/1.1 and HTTP/2 connections fail after the configured
response deadline. With scaled deterministic intervals, they now receive two
idle heartbeats and a later audit invalidation after both the ordinary deadline
and one frame's write budget have elapsed. Cancellation ends each handler.
Reconnect with changed, identical and absent heads always requests a refresh.

A real http.Server over synchronous net.Pipe demonstrates slow-client
backpressure and handler release on the next frame's deadline, without client
close/cancel as the escape path. Other real HTTP routes retain their normal
timeouts; 401/403 do not enter the SSE handler. Failure-injection tests cover
write, error-reporting Flush, deadline setup/clear and unsupported writers.

An isolated Chrome test runs the actual React/Query/SDK and realtime hook with
synthetic auth/API/SSE: changes during disconnect become visible on reconnect,
split frames are parsed, eight invalidations coalesce into one refetch, heartbeat
does not refetch, and logout cancels the stream and retry work. This browser
fixture and the Go HTTP network tests are separate, not a live IdP/database test.
