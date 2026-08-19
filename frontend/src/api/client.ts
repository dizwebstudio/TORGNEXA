import {TorgnexaClient} from "@torgnexa/sdk";
import type {AuthSession} from "../auth/session-model";

export function apiBaseURL(location: Location = window.location): string {
  return new URL("/api/v1", location.origin).toString().replace(/\/$/, "");
}

export function createApiClient(session: AuthSession, onUnauthorized: () => void): TorgnexaClient {
  const guardedFetch: typeof fetch = async (input, init) => {
    const response = await fetch(input, {...init, credentials: "same-origin", redirect: "error"});
    if (response.status === 401) onUnauthorized();
    return response;
  };
  return new TorgnexaClient({baseURL: apiBaseURL(), bearerToken: session.accessToken, fetch: guardedFetch});
}
