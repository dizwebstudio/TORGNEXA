import {createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode} from "react";
import type {AuthAdapter} from "./auth-adapter";
import {normalizeSession, sessionExpired, type AuthSession} from "./session-model";

type AuthStatus = "loading" | "anonymous" | "authenticated" | "error";

interface AuthContextValue {
  readonly status: AuthStatus;
  readonly session: AuthSession | null;
  readonly error: string | null;
  refresh(): Promise<void>;
  login(): Promise<void>;
  logout(): Promise<void>;
  manageAccount(): Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({adapter, children}: {adapter: AuthAdapter; children: ReactNode}) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [session, setSession] = useState<AuthSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setStatus("loading");
    setError(null);
    try {
      const next = await adapter.getSession();
      if (!next) {
        setSession(null);
        setStatus("anonymous");
        return;
      }
      const normalized = normalizeSession(next);
      if (sessionExpired(normalized)) {
        setSession(null);
        setStatus("anonymous");
        return;
      }
      setSession(normalized);
      setStatus("authenticated");
    } catch {
      setSession(null);
      setError("Не удалось проверить сессию. Повторите попытку.");
      setStatus("error");
    }
  }, [adapter]);

  useEffect(() => {
    void refresh();
    return adapter.subscribe?.(() => { void refresh(); });
  }, [adapter, refresh]);

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
