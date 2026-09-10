import type {AuthSession} from "../auth/session-model.js";
import {sameSessionContext} from "../auth/session-lifetime.js";

type SessionRefresher = () => Promise<AuthSession | null>;

export async function fetchWithSessionRefresh(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  currentSession: AuthSession,
  refreshSession: SessionRefresher,
  rejectSession: () => Promise<void>,
  transport: typeof fetch = fetch,
  lifetimeSignal?: AbortSignal,
): Promise<Response> {
  const original = new Request(input, {...init, credentials: "same-origin", redirect: "error"});
  const signal = lifetimeSignal ? AbortSignal.any([original.signal, lifetimeSignal]) : original.signal;
  const request = new Request(original, {signal});
  signal.throwIfAborted();
  let response = await transport(request.clone());
  // Also fence transports that completed despite abort, including a delayed 401
  // that otherwise could refresh/log out the next user or replay an old write.
  signal.throwIfAborted();
  if (response.status !== 401) return response;

  const refreshed = await refreshSession();
  signal.throwIfAborted();
  if (!refreshed) return response;
  if (!sameSessionContext(currentSession, refreshed)) throw new DOMException("Session changed", "AbortError");
  if (refreshed.accessToken === currentSession.accessToken) return response;

  const headers = new Headers(request.headers);
  headers.set("Authorization", `Bearer ${refreshed.accessToken}`);
  response = await transport(new Request(request, {headers}));
  signal.throwIfAborted();
  if (response.status === 401) await rejectSession();
  return response;
}
