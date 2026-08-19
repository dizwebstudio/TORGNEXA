import type {AuthAdapter} from "./auth-adapter";
import type {AuthSession} from "./session-model";
import {accountConsoleURL} from "./oidc-urls";

interface TokenClaims {
  sub?: string;
  name?: string;
  preferred_username?: string;
  exp?: number;
  iss?: string;
  realm_access?: {roles?: string[]};
}

interface TokenResponse {
  access_token?: string;
  expires_in?: number;
}

interface KeycloakConfig {
  issuer: string;
  clientId: string;
}

const roleCapabilities: Readonly<Record<string, readonly string[]>> = {
  admin: ["operations.realtime.read", "products.read", "products.write", "orders.read", "stock.read", "stock.write", "connectors.read", "connectors.accounts.read", "connectors.accounts.write", "sync.read", "sync.write", "approvals.read", "approvals.write", "compliance.read", "notifications.read", "reports.read", "audit.read", "settings.read", "settings.members.read", "settings.members.write", "settings.security.read", "settings.security.write", "settings.identity_providers.read", "settings.identity_providers.write", "settings.ai_providers.read", "settings.ai_providers.write", "ai.analyze"],
  manager: ["operations.realtime.read", "products.read", "orders.read", "stock.read", "connectors.read", "sync.read", "approvals.read", "compliance.read", "notifications.read", "reports.read", "settings.ai_providers.read", "ai.analyze"],
  operator: ["operations.realtime.read", "products.read", "orders.read", "stock.read", "connectors.read", "sync.read", "notifications.read", "settings.ai_providers.read", "ai.analyze"],
  viewer: ["operations.realtime.read", "products.read", "orders.read", "stock.read", "notifications.read", "reports.read", "settings.ai_providers.read"],
};

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function randomValue(bytes = 32): string {
  const value = new Uint8Array(bytes);
  crypto.getRandomValues(value);
  return base64url(value);
}

async function sha256(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return base64url(new Uint8Array(digest));
}

function decodeClaims(token: string): TokenClaims {
  const encoded = token.split(".")[1];
  if (!encoded) throw new Error("OIDC access token is malformed");
  const normalized = encoded.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
  const bytes = Uint8Array.from(atob(padded), (char) => char.charCodeAt(0));
  return JSON.parse(new TextDecoder("utf-8").decode(bytes)) as TokenClaims;
}

function capabilitiesFor(claims: TokenClaims): string[] {
  const roles = claims.realm_access?.roles ?? [];
  return [...new Set(roles.flatMap((role) => roleCapabilities[role] ?? []))].sort();
}

async function waitForCallback(popup: Window, origin: string, state: string): Promise<string> {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    if (popup.closed) throw new Error("OIDC login window was closed");
    try {
      const url = new URL(popup.location.href);
      if (url.origin === origin && url.searchParams.has("code")) {
        if (url.searchParams.get("state") !== state) throw new Error("OIDC state mismatch");
        const code = url.searchParams.get("code");
        if (!code) throw new Error("OIDC code is missing");
        popup.close();
        return code;
      }
      if (url.origin === origin && url.searchParams.has("error")) {
        throw new Error("OIDC provider rejected login");
      }
    } catch (error) {
      if (error instanceof Error && error.message.startsWith("OIDC")) throw error;
      // Cross-origin popup access is expected until Keycloak redirects home.
    }
    await new Promise((resolve) => window.setTimeout(resolve, 150));
  }
  popup.close();
  throw new Error("OIDC login timed out");
}

export function createKeycloakAdapter(config: KeycloakConfig): AuthAdapter {
  const issuer = config.issuer.replace(/\/$/, "");
  const manageAccountURL = accountConsoleURL(issuer);
  let session: AuthSession | null = null;
  const listeners = new Set<() => void>();
  const notify = () => listeners.forEach((listener) => listener());

  return {
    async getSession() {
      if (session?.expiresAt && Date.parse(session.expiresAt) <= Date.now()) session = null;
      return session;
    },
    async login(returnTo: string) {
      const verifier = randomValue(64);
      const state = randomValue();
      const redirectURI = new URL("/oidc/callback", window.location.origin).toString();
      const authorize = new URL(`${issuer}/protocol/openid-connect/auth`);
      authorize.search = new URLSearchParams({
        client_id: config.clientId,
        redirect_uri: redirectURI,
        response_type: "code",
        scope: "openid profile",
        code_challenge: await sha256(verifier),
        code_challenge_method: "S256",
        state,
      }).toString();
      const popup = window.open(authorize, "torgnexa-oidc", "popup,width=520,height=720");
      if (!popup) throw new Error("OIDC popup was blocked");
      const code = await waitForCallback(popup, window.location.origin, state);
      const tokenResponse = await fetch(`${issuer}/protocol/openid-connect/token`, {
        method: "POST",
        headers: {"Content-Type": "application/x-www-form-urlencoded"},
        body: new URLSearchParams({
          grant_type: "authorization_code",
          client_id: config.clientId,
          redirect_uri: redirectURI,
          code,
          code_verifier: verifier,
        }),
        credentials: "omit",
        redirect: "error",
      });
      if (!tokenResponse.ok) throw new Error("OIDC token exchange failed");
      const tokens = await tokenResponse.json() as TokenResponse;
      if (!tokens.access_token) throw new Error("OIDC access token is missing");
      const claims = decodeClaims(tokens.access_token);
      if (!claims.sub || claims.iss !== issuer) throw new Error("OIDC claims are invalid");
      const expiresAt = new Date((claims.exp ?? Math.floor(Date.now() / 1000) + (tokens.expires_in ?? 300)) * 1000).toISOString();
      session = {
        subject: claims.sub,
        // Never promote the opaque OIDC subject to presentation data. The
        // session normalizer also rejects UUID-shaped/equal display claims and
        // uses a role-derived neutral label when no human name is available.
        displayName: claims.name || claims.preferred_username || "",
        accessToken: tokens.access_token,
        capabilities: capabilitiesFor(claims),
        roles: [...new Set(claims.realm_access?.roles ?? [])].sort(),
        expiresAt,
      };
      window.history.replaceState({}, "", returnTo.startsWith("/") ? returnTo : "/");
      notify();
    },
    async logout() {
      session = null;
      notify();
    },
    async manageAccount() {
      // With `noopener`, browsers are allowed to return null even when the new
      // tab was opened successfully. The return value therefore cannot be used
      // as a popup-blocking signal here.
      window.open(manageAccountURL, "_blank", "noopener,noreferrer");
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}
