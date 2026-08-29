import type {QueryClient} from "@tanstack/react-query";

// The demo endpoint fills several independent read models in one transaction.
// Invalidate the complete client cache so this list cannot drift when a new
// demo-backed section is added to the application.
export function refreshDemoDataset(cache: QueryClient) {
  return cache.invalidateQueries();
}
