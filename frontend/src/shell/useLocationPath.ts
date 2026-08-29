import {useSyncExternalStore} from "react";

function subscribe(listener: () => void): () => void {
  window.addEventListener("popstate", listener);
  return () => window.removeEventListener("popstate", listener);
}
function snapshot(): string { return window.location.pathname; }

export function useLocationPath(): string {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}

export function navigate(path: string): void {
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  const target = new URL(path, window.location.href);
  const next = `${target.pathname}${target.search}${target.hash}`;
  if (next === current) return;
  window.history.pushState(null, "", next);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
