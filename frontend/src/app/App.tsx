import {SessionQueryProvider} from "../auth/SessionQueryProvider";
import {lazy,Suspense,useState} from "react";
import {AuthProvider} from "../auth/AuthProvider";
import {AuthBoundary} from "../auth/AuthBoundary";
import {runtimeAuthAdapter} from "../auth/auth-adapter";
import {ApiProvider} from "../api/ApiProvider";
import {AppShell} from "../shell/AppShell";
import {UiProvider} from "./UiProvider";

const PublicDocumentationPage = lazy(() => import("../pages/PublicDocumentationPage").then(module => ({default: module.PublicDocumentationPage})));

export function App() {
  if (window.location.pathname === "/docs" || window.location.pathname.startsWith("/docs/")) return <Suspense fallback={<main className="page-loading" aria-busy="true">Загрузка документации…</main>}><PublicDocumentationPage /></Suspense>;
  const [authAdapter] = useState(runtimeAuthAdapter);
  return <AuthProvider adapter={authAdapter}><AuthBoundary><SessionQueryProvider><ApiProvider><UiProvider><AppShell /></UiProvider></ApiProvider></SessionQueryProvider></AuthBoundary></AuthProvider>;
}
