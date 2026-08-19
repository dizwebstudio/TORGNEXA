import {useEffect, useRef, useState} from "react";
import {useApi} from "../api/ApiProvider";
import {navigate} from "../shell/useLocationPath";

export function ConnectorOAuthCallbackPage() {
  const api = useApi();
  const started = useRef(false);
  const [status, setStatus] = useState<"working"|"done"|"error">("working");

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    const query = new URLSearchParams(window.location.search);
    const code = query.get("code") ?? "";
    const state = query.get("state") ?? "";
    const providerError = query.get("error");
    if (providerError || !code || !state) {
      setStatus("error");
      return;
    }
    const callbackUrl = new URL("/oauth/connectors/callback", window.location.origin).toString();
    void api.completeConnectorOAuth({body:{code,state,callback_url:callbackUrl}},{headers:{"Idempotency-Key":`oauth-callback:${state.slice(0,64)}`}})
      .then(() => {
        window.history.replaceState(null, "", "/oauth/connectors/callback");
        setStatus("done");
      })
      .catch(() => setStatus("error"));
  }, [api]);

  return <section className="panel settings-card">
    <p className="eyebrow">OAuth 2.0</p>
    <h2>{status==="working"?"Завершаем авторизацию…":status==="done"?"Авторизация завершена":"Авторизация не завершена"}</h2>
    <p>{status==="working"?"Одноразовый код обменивается на токен через защищённый серверный канал.":status==="done"?"Токены зашифрованы. Теперь выполните проверку подключения и включите кабинет.":"State истёк, уже использован либо провайдер отклонил запрос. Начните авторизацию заново."}</p>
    {status!=="working"?<button className="button primary" onClick={()=>navigate("/integrations")}>Вернуться к интеграциям</button>:null}
  </section>;
}
