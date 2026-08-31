import type {AuthAdapter} from "./auth-adapter";
import type {AuthSession, UserProfile} from "./session-model";
import {sessionExpired, sessionNeedsRefresh} from "./session-model";
import {accountConsoleURL} from "./oidc-urls";

interface TokenClaims {
  sub?: string;
  name?: string;
  preferred_username?: string;
  email?: string;
  given_name?: string;
  family_name?: string;
  picture?: string;
  birthdate?: string;
  job_title?: string;
  position?: string;
  title?: string;
  department?: string;
  phone_number?: string;
  locale?: string;
  exp?: number;
  iss?: string;
  realm_access?: {roles?: string[]};
}

interface TokenResponse {
  access_token?: unknown;
  expires_in?: unknown;
  refresh_token?: unknown;
}

interface KeycloakConfig {
  issuer: string;
  clientId: string;
}

class TokenEndpointError extends Error {
  constructor(readonly terminal: boolean) {
    super("OIDC token endpoint rejected the request");
  }
}

const silentErrors = new Set(["login_required", "interaction_required", "consent_required", "session_expired"]);

const roleCapabilities: Readonly<Record<string, readonly string[]>> = {
  admin: ["operations.realtime.read", "products.read", "products.write", "orders.read", "orders.status.write", "orders.returns.read", "orders.returns.write", "payments.refunds.write", "stock.read", "stock.write", "connectors.read", "connectors.accounts.read", "connectors.accounts.write", "connectors.replay.run", "sync.read", "sync.write", "approvals.read", "approvals.write", "compliance.read", "notifications.read", "reports.read", "audit.read", "settings.read", "settings.profile.read", "settings.profile.write", "settings.members.read", "settings.members.write", "settings.security.read", "settings.security.write", "settings.security.posture.read", "settings.security.evidence.read", "settings.identity_providers.read", "settings.identity_providers.write", "settings.ai_providers.read", "settings.ai_providers.write", "settings.ai_governance.read", "settings.ai_governance.write", "settings.mcp_accounts.read", "settings.mcp_accounts.write", "social.post.edit", "social.post.delete", "ai.analyze", "profitability.scenarios.write", "counterparties.read", "webhooks.read", "webhooks.write", "plugins.read", "settlements.read", "fx.read", "cloud.subscription.read", "privacy.requests.write"],
  manager: ["operations.realtime.read", "products.read", "orders.read", "orders.status.write", "orders.returns.read", "orders.returns.write", "payments.refunds.write", "stock.read", "connectors.read", "connectors.replay.run", "sync.read", "sync.write", "approvals.read", "compliance.read", "notifications.read", "reports.read", "settings.profile.read", "settings.profile.write", "settings.ai_providers.read", "settings.ai_governance.read", "settings.mcp_accounts.read", "social.post.edit", "social.post.delete", "ai.analyze", "profitability.scenarios.write", "counterparties.read", "webhooks.read", "plugins.read", "settlements.read", "fx.read"],
  operator: ["operations.realtime.read", "products.read", "orders.read", "orders.status.write", "orders.returns.read", "orders.returns.write", "payments.refunds.write", "stock.read", "connectors.read", "connectors.replay.run", "sync.read", "notifications.read", "settings.profile.read", "settings.profile.write", "settings.ai_providers.read", "settings.ai_governance.read", "settings.mcp_accounts.read", "social.post.edit", "social.post.delete", "ai.analyze", "profitability.scenarios.write", "counterparties.read", "webhooks.read", "plugins.read"],
  viewer: ["operations.realtime.read", "products.read", "orders.read", "orders.returns.read", "stock.read", "notifications.read", "reports.read", "settings.profile.read", "settings.profile.write", "settings.ai_providers.read", "settings.ai_governance.read", "settings.mcp_accounts.read", "counterparties.read", "settlements.read", "fx.read"],
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

function profileFor(claims: TokenClaims): UserProfile | undefined {
  const profile: UserProfile = {
    username: claims.preferred_username,
    email: claims.email,
    givenName: claims.given_name,
    familyName: claims.family_name,
    picture: claims.picture,
    birthdate: claims.birthdate,
    jobTitle: claims.job_title || claims.position || claims.title,
    department: claims.department,
    phoneNumber: claims.phone_number,
    locale: claims.locale,
  };
  return Object.values(profile).some(Boolean) ? profile : undefined;
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

async function waitForSilentCallback(frame: HTMLIFrameElement, origin: string, state: string): Promise<string | null> {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      const url = new URL(frame.contentWindow?.location.href ?? "about:blank");
      if (url.origin === origin && url.searchParams.has("code")) {
        if (url.searchParams.get("state") !== state) throw new Error("OIDC state mismatch");
        const code = url.searchParams.get("code");
        if (!code) throw new Error("OIDC code is missing");
        return code;
      }
      if (url.origin === origin && url.searchParams.has("error")) {
        if (url.searchParams.get("state") !== state) throw new Error("OIDC state mismatch");
        const error = url.searchParams.get("error") ?? "";
        if (silentErrors.has(error)) return null;
        throw new Error("OIDC provider rejected silent login");
      }
    } catch (error) {
      if (error instanceof Error && error.message.startsWith("OIDC")) throw error;
      // Cross-origin frame access is expected until the provider redirects home.
    }
    await new Promise((resolve) => window.setTimeout(resolve, 100));
  }
  throw new Error("OIDC silent login timed out");
}

export function createKeycloakAdapter(config: KeycloakConfig): AuthAdapter {
  const issuer = config.issuer.replace(/\/$/, "");
  const manageAccountURL = accountConsoleURL(issuer);
  let session: AuthSession | null = null;
  let refreshToken: string | null = null;
  let silentAttempted = false;
  let silentInFlight: Promise<AuthSession | null> | null = null;
  let refreshInFlight: Promise<AuthSession | null> | null = null;
  const listeners = new Set<() => void>();
  const notify = () => listeners.forEach((listener) => listener());

  const clearSession = () => {
    session = null;
    refreshToken = null;
  };

  const authorizationURL = async (redirectURI: string, state: string, verifier: string, silent: boolean) => {
    const authorize = new URL(`${issuer}/protocol/openid-connect/auth`);
    const parameters: Record<string, string> = {
      client_id: config.clientId,
      redirect_uri: redirectURI,
      response_type: "code",
      scope: "openid profile",
      code_challenge: await sha256(verifier),
      code_challenge_method: "S256",
      state,
    };
    if (silent) parameters.prompt = "none";
    authorize.search = new URLSearchParams(parameters).toString();
    return authorize;
  };

  const requestTokens = async (parameters: Record<string, string>): Promise<TokenResponse> => {
    const response = await fetch(`${issuer}/protocol/openid-connect/token`, {
      method: "POST",
      headers: {"Content-Type": "application/x-www-form-urlencoded"},
      body: new URLSearchParams({client_id: config.clientId, ...parameters}),
      credentials: "omit",
      redirect: "error",
    });
    if (!response.ok) throw new TokenEndpointError(response.status === 400 || response.status === 401);
    return await response.json() as TokenResponse;
  };

  const applyTokens = (tokens: TokenResponse, preserveRefreshToken: boolean): AuthSession => {
    if (typeof tokens.access_token !== "string" || !tokens.access_token || tokens.access_token.length > 16_384) {
      throw new Error("OIDC access token is missing");
    }
    const claims = decodeClaims(tokens.access_token);
    if (!claims.sub || claims.iss !== issuer) throw new Error("OIDC claims are invalid");
    const expiresIn = typeof tokens.expires_in === "number" && Number.isFinite(tokens.expires_in) ? tokens.expires_in : 300;
    const expiresAt = new Date((claims.exp ?? Math.floor(Date.now() / 1000) + expiresIn) * 1000).toISOString();
    const nextRefreshToken = typeof tokens.refresh_token === "string" && tokens.refresh_token.length > 0 && tokens.refresh_token.length <= 16_384
      ? tokens.refresh_token
      : preserveRefreshToken ? refreshToken : null;
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
      profile: profileFor(claims),
    };
    refreshToken = nextRefreshToken;
    return session;
  };

  const exchangeAuthorizationCode = async (code: string, verifier: string, redirectURI: string) => applyTokens(await requestTokens({
    grant_type: "authorization_code",
    redirect_uri: redirectURI,
    code,
    code_verifier: verifier,
  }), false);

  const silentSignIn = async (): Promise<AuthSession | null> => {
    silentAttempted = true;
    const verifier = randomValue(64);
    const state = randomValue();
    const redirectURI = new URL("/oidc/silent-callback.html", window.location.origin).toString();
    const frame = document.createElement("iframe");
    frame.hidden = true;
    frame.tabIndex = -1;
    frame.setAttribute("aria-hidden", "true");
    frame.title = "Проверка сессии";
    document.body.appendChild(frame);
    try {
      frame.src = (await authorizationURL(redirectURI, state, verifier, true)).toString();
      const code = await waitForSilentCallback(frame, window.location.origin, state);
      return code ? await exchangeAuthorizationCode(code, verifier, redirectURI) : null;
    } finally {
      frame.remove();
    }
  };

  const startSilentSignIn = () => {
    if (!silentInFlight) {
      silentInFlight = silentSignIn().finally(() => { silentInFlight = null; });
    }
    return silentInFlight;
  };

  const renewSession = async (): Promise<AuthSession | null> => {
    if (!refreshToken) return startSilentSignIn();
    try {
      return applyTokens(await requestTokens({grant_type: "refresh_token", refresh_token: refreshToken}), true);
    } catch (error) {
      if (!(error instanceof TokenEndpointError) || !error.terminal) throw error;
      clearSession();
      return startSilentSignIn();
    }
  };

  const startRenewal = () => {
    if (!refreshInFlight) {
      refreshInFlight = renewSession().finally(() => { refreshInFlight = null; });
    }
    return refreshInFlight;
  };

  return {
    async getSession(options) {
      if (!session && !silentAttempted) session = await startSilentSignIn();
      if (!session) return null;
      if (!options?.forceRefresh && !sessionNeedsRefresh(session)) return session;
      try {
        const renewed = await startRenewal();
        if (!renewed) clearSession();
        return renewed;
      } catch (error) {
        if (!sessionExpired(session)) return session;
        clearSession();
        throw error;
      }
    },
    async login(returnTo: string) {
      silentAttempted = true;
      const verifier = randomValue(64);
      const state = randomValue();
      const redirectURI = new URL("/oidc/callback", window.location.origin).toString();
      const authorize = await authorizationURL(redirectURI, state, verifier, false);
      const popup = window.open(authorize, "torgnexa-oidc", "popup,width=520,height=720");
      if (!popup) throw new Error("OIDC popup was blocked");
      const code = await waitForCallback(popup, window.location.origin, state);
      session = await exchangeAuthorizationCode(code, verifier, redirectURI);
      window.history.replaceState({}, "", returnTo.startsWith("/") ? returnTo : "/");
      notify();
    },
    async logout() {
      silentAttempted = true;
      clearSession();
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
