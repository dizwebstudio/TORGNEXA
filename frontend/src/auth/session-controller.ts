import type {AuthAdapter} from "./auth-adapter.js";
import {normalizeSession, sessionExpired, type AuthSession} from "./session-model.js";
import {sameSessionContext, SessionLifetime} from "./session-lifetime.js";

export interface AuthSnapshot {
  readonly status: "loading" | "anonymous" | "authenticated" | "error";
  readonly session: AuthSession | null;
  readonly lifetime: SessionLifetime | null;
  readonly error: string | null;
}

// A single snapshot publishes session and cache lifetime together. Request and
// interaction revisions prevent late refresh/login results from restoring a
// retired identity, including after logout or an adapter change notification.
export class SessionController {
  private snapshot: AuthSnapshot = {status: "loading", session: null, lifetime: null, error: null};
  private readonly listeners = new Set<() => void>();
  private request = 0;
  private interaction = 0;
  private loggingOut = false;

  constructor(private readonly adapter: AuthAdapter) {}

  getSnapshot = (): AuthSnapshot => this.snapshot;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  private publish(status: AuthSnapshot["status"], session: AuthSession | null, error: string | null = null): void {
    const previous = this.snapshot;
    const retained = session && previous.session && previous.lifetime && !previous.lifetime.signal.aborted
      && sameSessionContext(previous.session, session);
    const lifetime = retained ? previous.lifetime : session ? new SessionLifetime() : null;
    if (previous.lifetime !== lifetime) previous.lifetime?.retire();
    this.snapshot = {status, session, lifetime, error};
    this.listeners.forEach(listener => listener());
  }

  refresh = async (options?: {forceRefresh?: boolean}): Promise<AuthSession | null> => {
    if (this.loggingOut) return null;
    const request = ++this.request;
    if (this.snapshot.status !== "authenticated") this.publish("loading", null);
    try {
      const session = await this.adapter.getSession(options);
      if (request !== this.request || this.loggingOut) return null;
      const normalized = session ? normalizeSession(session) : null;
      if (!normalized || sessionExpired(normalized)) {
        this.publish("anonymous", null);
        return null;
      }
      this.publish("authenticated", normalized);
      return normalized;
    } catch {
      if (request === this.request) this.publish("error", null, "Не удалось проверить сессию. Повторите попытку.");
      return null;
    }
  };

  login = async (returnTo: string): Promise<void> => {
    const interaction = ++this.interaction;
    ++this.request;
    this.loggingOut = false;
    this.publish("loading", null);
    try {
      await this.adapter.login(returnTo);
      if (interaction === this.interaction) await this.refresh();
    } catch {
      if (interaction === this.interaction) this.publish("error", null, "Вход недоступен: OIDC-адаптер не настроен или отклонил запрос.");
    }
  };

  logout = async (): Promise<void> => {
    const interaction = ++this.interaction;
    ++this.request;
    this.loggingOut = true;
    // Retire before awaiting any remote logout operation.
    this.publish("anonymous", null);
    try { await this.adapter.logout(); } finally {
      if (interaction === this.interaction) this.loggingOut = false;
    }
  };

  expire = (lifetime: SessionLifetime): void => {
    // A renewal can retain this lifetime before React cleans up the old timer.
    if (this.snapshot.lifetime !== lifetime || !this.snapshot.session || !sessionExpired(this.snapshot.session)) return;
    ++this.request;
    this.publish("anonymous", null);
  };

  manageAccount = async (): Promise<void> => {
    const lifetime = this.snapshot.lifetime;
    try {
      if (!this.adapter.manageAccount) throw new Error("OIDC account management is not configured");
      await this.adapter.manageAccount();
    } catch {
      if (lifetime === this.snapshot.lifetime) this.publish(this.snapshot.status, this.snapshot.session, "Не удалось открыть управление учётной записью OIDC.");
    }
  };

  start = (): (() => void) => {
    const unsubscribe = this.adapter.subscribe?.(() => {
      if (this.loggingOut) return;
      ++this.request;
      this.publish("loading", null);
      void this.refresh();
    });
    void this.refresh();
    return () => {
      unsubscribe?.();
      ++this.request;
      ++this.interaction;
      this.publish("loading", null);
    };
  };
}
