import {TorgnexaClient} from "@torgnexa/sdk";
import type {AuthSession} from "../auth/session-model.js";

export function apiBaseURL(location: Location = window.location): string {
  return new URL("/api/v1", location.origin).toString().replace(/\/$/, "");
}

type SessionRefresher = () => Promise<AuthSession | null>;

export async function fetchWithSessionRefresh(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  currentAccessToken: string,
  refreshSession: SessionRefresher,
  rejectSession: () => Promise<void>,
  transport: typeof fetch = fetch,
): Promise<Response> {
  const request = new Request(input, {...init, credentials: "same-origin", redirect: "error"});
  let response = await transport(request.clone());
  if (response.status !== 401) return response;

  const refreshed = await refreshSession();
  if (!refreshed || refreshed.accessToken === currentAccessToken) return response;

  const headers = new Headers(request.headers);
  headers.set("Authorization", `Bearer ${refreshed.accessToken}`);
  response = await transport(new Request(request, {headers}));
  if (response.status === 401) await rejectSession();
  return response;
}

export function createApiClient(session: AuthSession, refreshSession: SessionRefresher, rejectSession: () => Promise<void>): TorgnexaClient {
  const guardedFetch: typeof fetch = (input, init) => fetchWithSessionRefresh(input, init, session.accessToken, refreshSession, rejectSession);
  return new TorgnexaClient({baseURL: apiBaseURL(), bearerToken: session.accessToken, fetch: guardedFetch});
}
