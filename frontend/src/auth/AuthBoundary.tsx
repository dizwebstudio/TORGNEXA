import type {ReactNode} from "react";
import {useAuth} from "./AuthProvider";

export function AuthBoundary({children}: {children: ReactNode}) {
  const auth = useAuth();
  if (auth.status === "loading") {
    return <main className="center-screen" aria-busy="true"><div className="brand-mark">TN</div><p>Проверяем защищённую сессию…</p></main>;
  }
  if (auth.status !== "authenticated" || !auth.session) {
    return (
      <main className="center-screen auth-screen">
        <div className="brand-mark">TN</div>
        <h1>TORGNEXA</h1>
        <p>Единая консоль торговли и интеграций. Используйте корпоративную учётную запись.</p>
        {auth.error && <div className="alert error" role="alert">{auth.error}</div>}
        <button className="button primary" onClick={() => void auth.login()}>Войти</button>
        <a className="button ghost" href="/docs">Открыть документацию</a>
      </main>
    );
  }
  return <>{children}</>;
}
