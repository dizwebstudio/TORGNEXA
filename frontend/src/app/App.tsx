import {QueryClient, QueryClientProvider} from "@tanstack/react-query";
import {useState} from "react";
import {AuthProvider} from "../auth/AuthProvider";
import {AuthBoundary} from "../auth/AuthBoundary";
import {runtimeAuthAdapter} from "../auth/auth-adapter";
import {ApiProvider} from "../api/ApiProvider";
import {AppShell} from "../shell/AppShell";
import {PublicDocumentationPage} from "../pages/PublicDocumentationPage";
import {UiProvider} from "./UiProvider";

export function App() {
  if (window.location.pathname === "/docs" || window.location.pathname.startsWith("/docs/")) return <PublicDocumentationPage />;
  const [queryClient] = useState(() => new QueryClient({defaultOptions: {queries: {retry: 1, refetchOnWindowFocus: false}, mutations: {retry: false}}}));
  const [authAdapter] = useState(runtimeAuthAdapter);
  return <QueryClientProvider client={queryClient}><AuthProvider adapter={authAdapter}><AuthBoundary><ApiProvider><UiProvider><AppShell /></UiProvider></ApiProvider></AuthBoundary></AuthProvider></QueryClientProvider>;
}
