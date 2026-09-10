import {createContext, useContext, useMemo, type ReactNode} from "react";
import type {TorgnexaClient} from "@torgnexa/sdk";
import {useAuth} from "../auth/AuthProvider";
import {createApiClient} from "./client";

const ApiContext = createContext<TorgnexaClient | null>(null);

export function ApiProvider({children}: {children: ReactNode}) {
  const auth = useAuth();
  if (!auth.session || !auth.lifetime) throw new Error("ApiProvider requires authenticated session");
  const client = useMemo(() => {
    const signal = auth.lifetime!.signal;
    return createApiClient(auth.session!,
      () => signal.aborted ? Promise.resolve(null) : auth.refresh({forceRefresh: true}),
      () => signal.aborted ? Promise.resolve() : auth.logout(), signal);
  }, [auth.session, auth.lifetime, auth.refresh, auth.logout]);
  return <ApiContext.Provider value={client}>{children}</ApiContext.Provider>;
}

export function useApi(): TorgnexaClient {
  const client = useContext(ApiContext);
  if (!client) throw new Error("useApi must be used inside ApiProvider");
  return client;
}
