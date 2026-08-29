import {useEffect,useState} from "react";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import {useAuth} from "../auth/AuthProvider";
import {useApi} from "../api/ApiProvider";
import {ErrorBlock} from "../components/ApiState";
import {IntegrationCatalog} from "../features/settings/IntegrationCatalog";
import {AIProviderSettings} from "../features/settings/AIProviderSettings";
import {MCPAccountSettings} from "../features/settings/MCPAccountSettings";
import {TrustControlSettings} from "../features/settings/TrustControlSettings";
import {WebhookSettings} from "../features/settings/WebhookSettings";
import {PluginsSettings} from "../features/settings/PluginsSettings";
import {WorkspaceSettings} from "../features/settings/WorkspaceSettings";
import {MemberSettings} from "../features/settings/MemberSettings";
import {NotificationSettings} from "../features/settings/NotificationSettings";
import {SecuritySettings} from "../features/settings/SecuritySettings";
import {IdentityProviderSettings} from "../features/settings/IdentityProviderSettings";
import {settingsTabs, type SettingsTabID} from "../features/settings/settings-tabs";
import {Page} from "./Page";
import {capabilityGroupFor, capabilityLabels, roleLabels, type CapabilityGroupID} from "../components/labels";
import {Icon, type IconName} from "../components/Icon";

function formatExpiry(value?: string): string {
  if (!value) return "Срок не передан провайдером";
  return new Intl.DateTimeFormat("ru-RU", {dateStyle: "medium", timeStyle: "short"}).format(new Date(value));
}

const accessGroups: readonly {id: CapabilityGroupID; title: string; description: string; icon: IconName}[] = [
  {id: "commerce", title: "Продажи и данные", description: "Каталог, заказы, остатки и аналитика", icon: "catalog"},
  {id: "integrations", title: "Интеграции и обмен", description: "Каналы, синхронизация и автоматизация", icon: "connectors"},
  {id: "control", title: "Контроль и безопасность", description: "Согласования, аудит и соответствие", icon: "approvals"},
  {id: "workspace", title: "Настройки workspace", description: "Провайдеры, участники и доступ", icon: "settings"},
  {id: "other", title: "Другие разрешения", description: "Дополнительные права текущей роли", icon: "info"},
];

export function SettingsPage() {
  const auth = useAuth();
  const api = useApi();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<SettingsTabID>("general");
  const [showTechnicalAccess, setShowTechnicalAccess] = useState(false);
  useEffect(() => {
    const focusAIProviders = () => {
      if (window.location.hash !== "#ai-provider-settings") return;
      const scrollToProviders = () => document.getElementById("ai-provider-settings")?.scrollIntoView({behavior: "smooth", block: "start"});
      window.requestAnimationFrame(() => window.requestAnimationFrame(scrollToProviders));
    };
    focusAIProviders();
    window.addEventListener("popstate", focusAIProviders);
    window.addEventListener("hashchange", focusAIProviders);
    return () => {
      window.removeEventListener("popstate", focusAIProviders);
      window.removeEventListener("hashchange", focusAIProviders);
    };
  }, []);
  const session = auth.session;
  if (!session) return null;
  const isAdmin = session.roles?.includes("admin") ?? false;
  const roles = session.roles?.length ? session.roles : [];
  const capabilities = [...new Set(session.capabilities)];
  const groupedCapabilities = accessGroups.map((group) => ({...group, items: capabilities.filter((capability) => capabilityGroupFor(capability) === group.id)})).filter((group) => group.items.length > 0);
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
      <section className="panel settings-card settings-access-card">
      <div className="settings-access-heading"><div><p className="eyebrow">УПРАВЛЕНИЕ ДОСТУПОМ</p><h2>Роли и права</h2><p className="settings-copy">Доступ собран из ролей текущего workspace и проверяется сервером при каждом действии.</p></div><span className="settings-access-source"><span className="settings-access-source-dot"/>Единый вход</span></div>
      <div className="settings-access-summary" aria-label="Сводка доступа"><div><strong>{roles.length}</strong><span>ролей назначено</span></div><div><strong>{capabilities.length}</strong><span>прав доступно</span></div><div><strong>OIDC</strong><span>источник авторизации</span></div></div>
      <div className="settings-access-roles"><div className="settings-access-section-heading"><div><h3>Роли workspace</h3><span>Набор полномочий, выданный вашей учётной записи</span></div></div><div className="settings-role-list">{roles.length ? roles.map((role) => <span className="settings-role-card" title={role} key={role}><span className="settings-role-icon"><Icon name="check" size={14}/></span><span><strong>{roleLabels[role] ?? role}</strong><small>{role === "admin" ? "Полный доступ" : "Назначенная роль"}</small></span></span>) : <span className="settings-empty-access">Роли не переданы провайдером</span>}</div></div>
      <div className="settings-access-divider"/>
      <div className="settings-access-section-heading settings-capabilities-heading"><div><h3>Разрешения</h3><span>Что можно просматривать и изменять в рабочем контуре</span></div><button type="button" className="button ghost compact-action" onClick={() => setShowTechnicalAccess((value) => !value)}>{showTechnicalAccess ? "Скрыть коды" : "Показать технические коды"}</button></div>
      <div className="settings-capability-groups">{groupedCapabilities.map((group) => <section className="settings-capability-group" key={group.id}><header><span className="settings-capability-icon"><Icon name={group.icon} size={16}/></span><div><h4>{group.title}</h4><small>{group.description}</small></div><b>{group.items.length}</b></header><div className="settings-capability-list">{group.items.map((capability) => <div className="settings-capability-item" title={capability} key={capability}><span className="settings-capability-check"><Icon name="check" size={13}/></span><span><strong>{capabilityLabels[capability] ?? capability}</strong>{showTechnicalAccess ? <code>{capability}</code> : null}</span></div>)}</div></section>)}</div>
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
    <div id="settings-panel-mcp" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-mcp" tabIndex={0} hidden={activeTab !== "mcp"}>
      <MCPAccountSettings />
    </div>
    <div id="settings-panel-trust" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-trust" tabIndex={0} hidden={activeTab !== "trust"}>
      <TrustControlSettings />
    </div>
    <div id="settings-panel-webhooks" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-webhooks" tabIndex={0} hidden={activeTab !== "webhooks"}>
      <WebhookSettings />
    </div>
    <div id="settings-panel-plugins" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-plugins" tabIndex={0} hidden={activeTab !== "plugins"}>
      <PluginsSettings />
    </div>
  </Page>;
}
