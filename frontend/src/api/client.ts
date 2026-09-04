import {TorgnexaClient} from "@torgnexa/sdk";
import type {AuthSession} from "../auth/session-model.js";
import {fetchWithSessionRefresh} from "./session-fetch.js";

export {fetchWithSessionRefresh} from "./session-fetch.js";

type SessionRefresher = () => Promise<AuthSession | null>;

export function apiBaseURL(location: Location = window.location): string {
  return new URL("/api/v1", location.origin).toString().replace(/\/$/, "");
}

export function createApiClient(session: AuthSession, refreshSession: SessionRefresher, rejectSession: () => Promise<void>): TorgnexaClient {
  const guardedFetch: typeof fetch = (input, init) => fetchWithSessionRefresh(input, init, session.accessToken, refreshSession, rejectSession);
  return new TorgnexaClient({baseURL: apiBaseURL(), bearerToken: session.accessToken, fetch: guardedFetch});
}
