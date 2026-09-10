import {createRequire} from "node:module";

const require = createRequire(new URL("../../../frontend/package.json", import.meta.url));
const {QueryClient, QueryObserver} = require("@tanstack/react-query");
const cache = new QueryClient({defaultOptions: {queries: {retry: 1, refetchOnWindowFocus: false}, mutations: {retry: false}}});
const key = ["orders", "shell", "", "", ""];
cache.setQueryData(key, {items: [{order_number: "SYNTHETIC-WORKSPACE-A-ORDER"}]});

// App retains this QueryClient while AuthProvider replaces the identity.
// Match OrderList's key and staleTime; no real accounts/network are used.
let secondUserRequests = 0;
const observer = new QueryObserver(cache, {
  queryKey: key,
  queryFn: async () => { secondUserRequests++; return {items: []}; },
  staleTime: 20_000,
});
const unsubscribe = observer.subscribe(() => {});
console.log(JSON.stringify({newIdentitySees: observer.getCurrentResult().data, secondUserRequests}));
unsubscribe();
cache.clear();
