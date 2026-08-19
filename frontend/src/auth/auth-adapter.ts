import type {AuthSession} from "./session-model";
import {createKeycloakAdapter} from "./keycloak-adapter";

export interface AuthAdapter {
  getSession(): Promise<AuthSession | null>;
  login(returnTo: string): Promise<void>;
  logout(): Promise<void>;
  manageAccount?(): Promise<void>;
  subscribe?(listener: () => void): () => void;
}

declare global {
  interface Window {
    __TORGNEXA_AUTH_ADAPTER__?: AuthAdapter;
  }
}

const unavailableAdapter: AuthAdapter = {
  async getSession() { return null; },
  async login() { throw new Error("OIDC auth adapter is not configured"); },
  async logout() {},
  async manageAccount() { throw new Error("OIDC account management is not configured"); },
};

// The Community API validates the token issuer exactly. Keep the browser-side
// issuer identical even when the UI itself was opened through localhost.
export const communityOIDCIssuer = "http://127.0.0.1:8081/realms/torgnexa";

export function runtimeAuthAdapter(): AuthAdapter {
  if (window.__TORGNEXA_AUTH_ADAPTER__) return window.__TORGNEXA_AUTH_ADAPTER__;
  if (window.location.hostname === "127.0.0.1" || window.location.hostname === "localhost") {
    return createKeycloakAdapter({
      issuer: communityOIDCIssuer,
      clientId: "torgnexa-web",
    });
  }
  return unavailableAdapter;
}
