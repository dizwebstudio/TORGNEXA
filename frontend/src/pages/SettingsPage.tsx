import {useState} from "react";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import {useAuth} from "../auth/AuthProvider";
import {useApi} from "../api/ApiProvider";
import {ErrorBlock} from "../components/ApiState";
import {IntegrationCatalog} from "../features/settings/IntegrationCatalog";
import {AIProviderSettings} from "../features/settings/AIProviderSettings";
import {WorkspaceSettings} from "../features/settings/WorkspaceSettings";
import {MemberSettings} from "../features/settings/MemberSettings";
import {NotificationSettings} from "../features/settings/NotificationSettings";
import {SecuritySettings} from "../features/settings/SecuritySettings";
import {IdentityProviderSettings} from "../features/settings/IdentityProviderSettings";
import {settingsTabs, type SettingsTabID} from "../features/settings/settings-tabs";
import {Page} from "./Page";

function formatExpiry(value?: string): string {
  if (!value) return "Срок не передан провайдером";
  return new Intl.DateTimeFormat("ru-RU", {dateStyle: "medium", timeStyle: "short"}).format(new Date(value));
}

export function SettingsPage() {
  const auth = useAuth();
  const api = useApi();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<SettingsTabID>("general");
  const session = auth.session;
  if (!session) return null;
  const isAdmin = session.roles?.includes("admin") ?? false;
  const deleteDemo = useMutation({mutationFn:async()=>api.createApprovalRequest({idempotencyKey:crypto.randomUUID(),body:{action:"demo.dataset.delete",resource_type:"demo_dataset",resource_id:"",risk:"write_sensitive"}}),onSuccess:async()=>{await Promise.all([queryClient.invalidateQueries({queryKey:["approvals"]}),queryClient.invalidateQueries({queryKey:["audit"]})]);}});

  return <Page eyebrow="УЧЁТНАЯ ЗАПИСЬ" title="Настройки" description="Профиль и безопасность текущей OIDC-сессии. Пароль остаётся у провайдера идентификации и никогда не передаётся TORGNEXA.">
    {auth.error ? <ErrorBlock>{auth.error}</ErrorBlock> : null}
    <div className="settings-tabs" role="tablist" aria-label="Разделы настроек">
      {settingsTabs.map((tab) => <button id={`settings-tab-${tab.id}`} type="button" role="tab" aria-selected={activeTab === tab.id} aria-controls={`settings-panel-${tab.id}`} className={`settings-tab ${activeTab === tab.id ? "active" : ""}`} onClick={() => setActiveTab(tab.id)} key={tab.id}>{tab.label}</button>)}
    </div>
    <div id="settings-panel-general" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-general" tabIndex={0} hidden={activeTab !== "general"}>
      <div className="settings-grid">
      <section className="panel settings-card">
        <div className="settings-card-heading"><div><p className="eyebrow">Профиль</p><h2>{session.displayName}</h2></div><span className="avatar settings-avatar">{session.displayName.slice(0, 1).toUpperCase()}</span></div>
        <dl className="settings-facts">
          <div><dt>Сессия действует до</dt><dd>{formatExpiry(session.expiresAt)}</dd></div>
        </dl>
      </section>
      <section className="panel settings-card">
        <p className="eyebrow">Безопасность</p>
        <h2>Пароль и способы входа</h2>
        <p className="settings-copy">Смена пароля, восстановление доступа и привязанные способы входа управляются в защищённом кабинете Keycloak.</p>
        <button className="button primary" onClick={() => void auth.manageAccount()}>Управлять учётной записью</button>
        <small className="settings-note">TORGNEXA не видит и не сохраняет ваш пароль.</small>
      </section>
      </div>
      <section className="panel settings-card">
      <div className="settings-card-heading"><div><p className="eyebrow">Доступ</p><h2>Роли и возможности</h2></div><span className="status status-active">OIDC</span></div>
      <div className="settings-access">
        <div><h3>Роли</h3><div className="chip-list">{(session.roles?.length ? session.roles : ["Роли не переданы"]).map((role) => <span className="chip" key={role}>{role}</span>)}</div></div>
        <div><h3>Capabilities</h3><div className="chip-list">{session.capabilities.map((capability) => <span className="chip" key={capability}>{capability}</span>)}</div></div>
      </div>
      </section>
      <WorkspaceSettings />
      <MemberSettings />
      <SecuritySettings />
      <IntegrationCatalog />
      <AIProviderSettings />
      {isAdmin?<section className="panel settings-card danger-zone"><div><p className="eyebrow">Демо-данные</p><h2>Удалить демонстрационный набор</h2><p className="settings-copy">Операция чувствительная: сначала создаётся запрос, затем его нужно одобрить и выполнить в разделе «Согласования».</p></div><button className="button danger" disabled={deleteDemo.isPending} onClick={()=>deleteDemo.mutate()}>{deleteDemo.isPending?"Создаём запрос…":"Запросить удаление"}</button>{deleteDemo.isSuccess?<span className="status status-active">Запрос создан в «Согласованиях»</span>:null}{deleteDemo.isError?<ErrorBlock>Создайте активную политику удаления в разделе «Согласования».</ErrorBlock>:null}</section>:null}
    </div>
    <div id="settings-panel-identity" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-identity" tabIndex={0} hidden={activeTab !== "identity"}>
      <IdentityProviderSettings />
    </div>
    <div id="settings-panel-notifications" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-notifications" tabIndex={0} hidden={activeTab !== "notifications"}>
      <NotificationSettings />
    </div>
  </Page>;
}
