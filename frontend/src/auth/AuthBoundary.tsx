import type {ReactNode} from "react";
import {useAuth} from "./AuthProvider";

export function AuthBoundary({children}: {children: ReactNode}) {
  const auth = useAuth();
  if (auth.status === "loading") {
    return <main className="center-screen" aria-busy="true"><img className="auth-brand-logo" src="/brand/torgnexa-logo.png" alt="TORGNEXA"/><p>Проверяем защищённую сессию…</p></main>;
  }
  if (auth.status !== "authenticated" || !auth.session) {
    return (
      <main className="center-screen auth-screen">
        <img className="auth-brand-logo" src="/brand/torgnexa-logo.png" alt="TORGNEXA"/>
        <h1 className="visually-hidden">TORGNEXA</h1>
        <p>Единая консоль торговли и интеграций. Используйте корпоративную учётную запись.</p>
        {auth.error && <div className="alert error" role="alert">{auth.error}</div>}
        <button className="button primary" onClick={() => void auth.login()}>Войти</button>
        <a className="button ghost" href="/docs">Открыть документацию</a>
      </main>
    );
  }
  return <>{children}</>;
}
