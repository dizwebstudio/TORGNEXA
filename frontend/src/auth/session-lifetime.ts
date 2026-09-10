import type {AuthSession} from "./session-model.js";

// Tokens stay in memory and never become React/query keys. Adapters that cannot
// describe their scope conservatively get a new lifetime on token replacement.
export function sameSessionContext(left: AuthSession, right: AuthSession): boolean {
  return left.subject === right.subject
    && (left.cacheScope || right.cacheScope
      ? left.cacheScope === right.cacheScope
      : left.accessToken === right.accessToken)
    && JSON.stringify(left.capabilities) === JSON.stringify(right.capabilities)
    && JSON.stringify(left.roles ?? []) === JSON.stringify(right.roles ?? []);
}

let nextLifetime = 0;

export class SessionLifetime {
  readonly id = ++nextLifetime;
  private readonly controller = new AbortController();
  private readonly cleanup = new Set<() => void>();
  readonly signal = this.controller.signal;

  onRetire(callback: () => void): () => void {
    if (this.signal.aborted) callback();
    else this.cleanup.add(callback);
    return () => { this.cleanup.delete(callback); };
  }

  retire(): void {
    if (this.signal.aborted) return;
    this.controller.abort();
    for (const callback of this.cleanup) callback();
    this.cleanup.clear();
  }
}
