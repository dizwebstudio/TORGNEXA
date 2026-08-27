import {createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode} from "react";
import type {AuthAdapter} from "./auth-adapter";
import {normalizeSession, sessionExpired, type AuthSession} from "./session-model";

type AuthStatus = "loading" | "anonymous" | "authenticated" | "error";

interface AuthContextValue {
  readonly status: AuthStatus;
  readonly session: AuthSession | null;
  readonly error: string | null;
  refresh(options?: {forceRefresh?: boolean}): Promise<AuthSession | null>;
  login(): Promise<void>;
  logout(): Promise<void>;
  manageAccount(): Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({adapter, children}: {adapter: AuthAdapter; children: ReactNode}) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [session, setSession] = useState<AuthSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async (options?: {forceRefresh?: boolean}) => {
    setStatus((current) => current === "authenticated" ? current : "loading");
    setError(null);
    try {
      const next = await adapter.getSession(options);
      if (!next) {
        setSession(null);
        setStatus("anonymous");
        return null;
      }
      const normalized = normalizeSession(next);
      if (sessionExpired(normalized)) {
        setSession(null);
        setStatus("anonymous");
        return null;
      }
      setSession(normalized);
      setStatus("authenticated");
      return normalized;
    } catch {
      setSession(null);
      setError("Не удалось проверить сессию. Повторите попытку.");
      setStatus("error");
      return null;
    }
  }, [adapter]);

  useEffect(() => {
    void refresh();
    return adapter.subscribe?.(() => { void refresh(); });
  }, [adapter, refresh]);

  useEffect(() => {
    if (!session?.expiresAt) return;
    const expires = Date.parse(session.expiresAt);
    if (!Number.isFinite(expires)) return;
    const delay = Math.max(15_000, expires - Date.now() - 60_000);
    const timer = window.setTimeout(() => { void refresh({forceRefresh: true}); }, delay);
    return () => window.clearTimeout(timer);
  }, [session, refresh]);

  useEffect(() => {
    const resume = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    document.addEventListener("visibilitychange", resume);
    return () => document.removeEventListener("visibilitychange", resume);
  }, [refresh]);

  const login = useCallback(async () => {
    setError(null);
    try {
      await adapter.login(window.location.pathname + window.location.search);
      await refresh();
    } catch {
      setError("Вход недоступен: OIDC-адаптер не настроен или отклонил запрос.");
      setStatus("error");
    }
  }, [adapter, refresh]);

  const logout = useCallback(async () => {
    try { await adapter.logout(); } finally {
      setSession(null);
      setStatus("anonymous");
    }
  }, [adapter]);

  const manageAccount = useCallback(async () => {
    setError(null);
    try {
      if (!adapter.manageAccount) throw new Error("OIDC account management is not configured");
      await adapter.manageAccount();
    } catch {
      setError("Не удалось открыть управление учётной записью OIDC.");
    }
  }, [adapter]);

  const value = useMemo<AuthContextValue>(() => ({status, session, error, refresh, login, logout, manageAccount}), [status, session, error, refresh, login, logout, manageAccount]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthProvider");
  return value;
}
