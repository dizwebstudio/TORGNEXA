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
  if (path === window.location.pathname) return;
  window.history.pushState(null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
