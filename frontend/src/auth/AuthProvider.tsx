import {createContext, useCallback, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode} from "react";
import type {AuthAdapter} from "./auth-adapter";
import {SessionController, type AuthSnapshot} from "./session-controller";
import type {AuthSession} from "./session-model";

interface AuthContextValue extends AuthSnapshot {
  refresh(options?: {forceRefresh?: boolean}): Promise<AuthSession | null>;
  login(): Promise<void>;
  logout(): Promise<void>;
  manageAccount(): Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({adapter, children}: {adapter: AuthAdapter; children: ReactNode}) {
  const controller = useMemo(() => new SessionController(adapter), [adapter]);
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot);
  useEffect(controller.start, [controller]);

  useEffect(() => {
    if (!snapshot.session?.expiresAt || !snapshot.lifetime) return;
    const expires = Date.parse(snapshot.session.expiresAt);
    const lifetime = snapshot.lifetime;
    const refresh = window.setTimeout(() => { void controller.refresh({forceRefresh: true}); }, Math.max(15_000, expires - Date.now() - 60_000));
    const expiry = window.setTimeout(() => { controller.expire(lifetime); }, Math.max(0, expires - Date.now()));
    return () => { window.clearTimeout(refresh); window.clearTimeout(expiry); };
  }, [snapshot.session, snapshot.lifetime, controller]);

  useEffect(() => {
    const resume = () => {
      if (document.visibilityState === "visible") void controller.refresh();
    };
    document.addEventListener("visibilitychange", resume);
    return () => document.removeEventListener("visibilitychange", resume);
  }, [controller]);

  const login = useCallback(() => controller.login(window.location.pathname + window.location.search), [controller]);
  const value = useMemo<AuthContextValue>(() => ({...snapshot, refresh: controller.refresh, login, logout: controller.logout, manageAccount: controller.manageAccount}), [snapshot, controller, login]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthProvider");
  return value;
}
