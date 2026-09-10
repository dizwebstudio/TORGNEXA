import {QueryClient, QueryClientProvider} from "@tanstack/react-query";
import {useLayoutEffect, useState, type ReactNode} from "react";
import {useAuth} from "./AuthProvider";
import type {SessionLifetime} from "./session-lifetime";

export function SessionQueryProvider({children}: {children: ReactNode}) {
  const {lifetime} = useAuth();
  if (!lifetime) return null;
  return <SessionQueries key={lifetime.id} lifetime={lifetime}>{children}</SessionQueries>;
}

function SessionQueries({lifetime, children}: {lifetime: SessionLifetime; children: ReactNode}) {
  const [client] = useState(() => new QueryClient({defaultOptions: {queries: {retry: 1, refetchOnWindowFocus: false}, mutations: {retry: false}}}));
  useLayoutEffect(() => {
    const clear = () => {
      void client.cancelQueries();
      client.clear();
    };
    const unsubscribe = lifetime.onRetire(clear);
    return () => { unsubscribe(); clear(); };
  }, [client, lifetime]);
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
