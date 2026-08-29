import type {QueryClient} from "@tanstack/react-query";

// The demo endpoint fills several independent read models in one transaction.
// Refresh every affected surface so an already-open workspace shows the full
// dataset immediately after the seed completes.
export function refreshDemoDataset(cache: QueryClient) {
  return Promise.all([
    cache.invalidateQueries({queryKey: ["products"]}),
    cache.invalidateQueries({queryKey: ["orders"]}),
    cache.invalidateQueries({queryKey: ["inventory"]}),
    cache.invalidateQueries({queryKey: ["fulfillment-allocations"]}),
    cache.invalidateQueries({queryKey: ["settlements"]}),
    cache.invalidateQueries({queryKey: ["fx-rates"]}),
    cache.invalidateQueries({queryKey: ["payments"]}),
    cache.invalidateQueries({queryKey: ["connector-accounts"]}),
    cache.invalidateQueries({queryKey: ["sync"]}),
    cache.invalidateQueries({queryKey: ["approvals"]}),
    cache.invalidateQueries({queryKey: ["approval-policies"]}),
    cache.invalidateQueries({queryKey: ["notifications"]}),
    cache.invalidateQueries({queryKey: ["shell", "activity"]}),
    cache.invalidateQueries({queryKey: ["dashboard"]}),
    cache.invalidateQueries({queryKey: ["onboarding"]}),
    cache.invalidateQueries({queryKey: ["compliance"]}),
    cache.invalidateQueries({queryKey: ["audit"]}),
  ]);
}
