// Heartbeats confirm that the SSE connection is alive. Only an explicit
// invalidation event represents a change that may make query data stale.
export function shouldInvalidateRealtimeEvent(event: string | undefined): boolean {
  return event === "invalidate";
}
