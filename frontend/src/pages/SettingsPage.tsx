import {useEffect,useRef,useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
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
import {IdentityProviderSettings} from "../features/settings/IdentityProviderSettings";
import {settingsTabs, type SettingsTabID} from "../features/settings/settings-tabs";
import {Page} from "./Page";
import {capabilityGroupFor, capabilityLabels, roleLabels, type CapabilityGroupID} from "../components/labels";
import {Icon, type IconName} from "../components/Icon";
import type {AuthSession, UserProfile} from "../auth/session-model";
import {UserAvatar} from "../components/UserAvatar";

function formatExpiry(value?: string): string {
  if (!value) return "Срок не передан провайдером";
  return new Intl.DateTimeFormat("ru-RU", {dateStyle: "medium", timeStyle: "short"}).format(new Date(value));
}

const demoProfile: UserProfile = {username: "demo", email: "demo@local.torgnexa", givenName: "Демо", familyName: "Оператор", picture: "/demo-images/demo-avatar.svg", birthdate: "1988-04-17", jobTitle: "Старший операционный менеджер", department: "Коммерческие операции", phoneNumber: "+7 (495) 555-01-42", locale: "ru"};

function profileForDisplay(session: AuthSession): UserProfile {
  const profile = session.profile ?? {};
  if (profile.username?.toLowerCase() !== "demo") return profile;
  const claimed = Object.fromEntries(Object.entries(profile).filter(([, value]) => value !== undefined && value !== ""));
  return {...demoProfile, ...claimed};
}

function profileDisplayName(profile: UserProfile, fallback: string): string {
  const fullName = [profile.givenName, profile.familyName].filter(Boolean).join(" ").trim();
  return fullName || profile.username || fallback;
}

function formatBirthdate(value?: string): string {
  if (!value) return "Не указана в профиле OIDC";
  const parsed = /^\d{4}-\d{2}-\d{2}$/.test(value) ? new Date(`${value}T00:00:00Z`) : new Date(value);
  return Number.isFinite(parsed.getTime()) ? new Intl.DateTimeFormat("ru-RU", {dateStyle: "long", timeZone: "UTC"}).format(parsed) : value;
}

interface BackendUserProfile {
  username: string;
  email: string;
  given_name: string;
  family_name: string;
  birthdate?: string;
  job_title?: string;
  department?: string;
  phone_number?: string;
  picture_url?: string;
  picture_upload_id?: string;
  picture_source: "none" | "identity_provider" | "uploaded";
  version: number;
  created_at: string;
  updated_at: string;
}

type EditableUserProfile = Pick<BackendUserProfile, "given_name" | "family_name" | "birthdate" | "job_title" | "department" | "phone_number">;

function decodeUserProfile(value: unknown): BackendUserProfile {
  if (!value || typeof value !== "object") throw new Error("invalid user profile response");
  const row = value as Record<string, unknown>;
  for (const key of ["username", "email", "given_name", "family_name", "picture_source", "created_at", "updated_at"]) {
    if (typeof row[key] !== "string") throw new Error("invalid user profile response");
  }
  if (typeof row.version !== "number" || !Number.isSafeInteger(row.version) || row.version < 1 || !["none", "identity_provider", "uploaded"].includes(row.picture_source as string)) throw new Error("invalid user profile response");
  return row as unknown as BackendUserProfile;
}

function profileFromBackend(value: BackendUserProfile): UserProfile {
  return {username: value.username || undefined, email: value.email || undefined, givenName: value.given_name || undefined, familyName: value.family_name || undefined, birthdate: value.birthdate || undefined, jobTitle: value.job_title || undefined, department: value.department || undefined, phoneNumber: value.phone_number || undefined, picture: value.picture_url || undefined};
}

function editableFromBackend(value: BackendUserProfile): EditableUserProfile {
  return {given_name: value.given_name, family_name: value.family_name, birthdate: value.birthdate ?? "", job_title: value.job_title ?? "", department: value.department ?? "", phone_number: value.phone_number ?? ""};
}

function bytesToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  return btoa(binary);
}

function wait(milliseconds: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
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
  const isAdmin = session?.roles?.includes("admin") ?? false;
  const roles = session?.roles?.length ? session.roles : [];
  const capabilities = [...new Set(session?.capabilities ?? [])];
  const canReadProfile = capabilities.includes("settings.profile.read");
  const canWriteProfile = capabilities.includes("settings.profile.write");
  const profileQuery = useQuery({queryKey: ["settings", "profile"], enabled: canReadProfile, queryFn: async () => decodeUserProfile((await api.getCurrentUserProfile()).body), staleTime: 30_000});
  const remoteProfile = profileQuery.data;
  const sessionProfile = session ? profileForDisplay(session) : {};
  const profile = remoteProfile ? {...sessionProfile, ...profileFromBackend(remoteProfile)} : sessionProfile;
  const [editingProfile, setEditingProfile] = useState(false);
  const [profileDraft, setProfileDraft] = useState<EditableUserProfile>({given_name: "", family_name: "", birthdate: "", job_title: "", department: "", phone_number: ""});
  const avatarInput = useRef<HTMLInputElement>(null);
  useEffect(() => { if (remoteProfile && !editingProfile) setProfileDraft(editableFromBackend(remoteProfile)); }, [remoteProfile, editingProfile]);
  const updateProfile = useMutation({mutationFn: async () => {
    if (!remoteProfile) throw new Error("Профиль ещё загружается");
    return api.updateCurrentUserProfile({idempotencyKey: crypto.randomUUID(), body: {...profileDraft, version: remoteProfile.version}});
  }, onSuccess: (response) => { const updated = decodeUserProfile(response.body); queryClient.setQueryData(["settings", "profile"], updated); setEditingProfile(false); }});
  const avatarMutation = useMutation({mutationFn: async (file: File) => {
    if (!remoteProfile) throw new Error("Профиль ещё загружается");
    if (!/^image\/(?:png|jpeg|gif)$/.test(file.type) || file.size < 1 || file.size > 5 * 1024 * 1024) throw new Error("Поддерживаются PNG, JPEG и GIF размером до 5 МБ");
    const accepted = await api.uploadCurrentUserAvatar({idempotencyKey: crypto.randomUUID(), body: {filename: file.name, declared_media_type: file.type, content_base64: bytesToBase64(await file.arrayBuffer())}});
    const acceptedBody = accepted.body as {id?: unknown};
    if (typeof acceptedBody?.id !== "string") throw new Error("Сервис загрузки не вернул идентификатор фото");
    for (let attempt = 0; attempt < 30; attempt += 1) {
      const status = (await api.getUpload({uploadId: acceptedBody.id})).body as {state?: unknown};
      if (status.state === "released") return api.updateCurrentUserProfile({idempotencyKey: crypto.randomUUID(), body: {picture_upload_id: acceptedBody.id, version: remoteProfile.version}});
      if (status.state === "rejected") throw new Error("Фото отклонено проверкой безопасности");
      await wait(500);
    }
    throw new Error("Проверка фото занимает больше обычного времени, повторите через несколько секунд");
  }, onSuccess: (response) => { queryClient.setQueryData(["settings", "profile"], decodeUserProfile(response.body)); if (avatarInput.current) avatarInput.current.value = ""; }});
  const removeAvatar = useMutation({mutationFn: async () => {
    if (!remoteProfile) throw new Error("Профиль ещё загружается");
    return api.deleteCurrentUserAvatar({idempotencyKey: crypto.randomUUID(), body: {version: remoteProfile.version}});
  }, onSuccess: (response) => queryClient.setQueryData(["settings", "profile"], decodeUserProfile(response.body))});
  const privacyRequest = useMutation({mutationFn: async (requestType: "access" | "export" | "deletion") => api.createCurrentUserProfilePrivacyRequest({idempotencyKey: crypto.randomUUID(), body: {request_type: requestType}})});
  const primaryRole = roles.find((role) => roleLabels[role]) ?? roles[0];
  const roleTitle = primaryRole ? roleLabels[primaryRole] ?? primaryRole : "Участник рабочего пространства";
  const groupedCapabilities = accessGroups.map((group) => ({...group, items: capabilities.filter((capability) => capabilityGroupFor(capability) === group.id)})).filter((group) => group.items.length > 0);
  const deleteDemo = useMutation({mutationFn:async()=>api.createApprovalRequest({idempotencyKey:crypto.randomUUID(),body:{action:"demo.dataset.delete",resource_type:"demo_dataset",resource_id:"",risk:"write_sensitive"}}),onSuccess:async()=>{await Promise.all([queryClient.invalidateQueries({queryKey:["approvals"]}),queryClient.invalidateQueries({queryKey:["audit"]})]);}});
  if (!session) return null;

  return <Page eyebrow="УЧЁТНАЯ ЗАПИСЬ" title="Настройки" description="Профиль и рабочие параметры текущего контура. Пароль остаётся у провайдера идентификации и никогда не передаётся TORGNEXA.">
    {auth.error ? <ErrorBlock>{auth.error}</ErrorBlock> : null}
    <div className="settings-tabs" role="tablist" aria-label="Разделы настроек">
      {settingsTabs.map((tab) => <button id={`settings-tab-${tab.id}`} type="button" role="tab" aria-selected={activeTab === tab.id} aria-controls={`settings-panel-${tab.id}`} className={`settings-tab ${activeTab === tab.id ? "active" : ""}`} onClick={() => setActiveTab(tab.id)} key={tab.id}>{tab.label}</button>)}
    </div>
    <div id="settings-panel-general" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-general" tabIndex={0} hidden={activeTab !== "general"}>
      <div className="settings-grid">
      <section className="panel settings-card profile-card">
        <div className="profile-hero"><UserAvatar session={session} profile={profile} className="settings-avatar"/><div className="profile-hero-copy"><p className="eyebrow">Профиль пользователя</p><h2>{profileDisplayName(profile, session.displayName)}</h2><p className="profile-title">{profile.jobTitle ?? roleTitle}</p><div className="profile-badges"><span>{roleTitle}</span><span>{profile.username ? `@${profile.username}` : "Профиль пользователя"}</span></div></div><div className="profile-hero-actions">{canWriteProfile && remoteProfile ? <button type="button" className="button ghost compact" onClick={() => setEditingProfile((value) => !value)}>{editingProfile ? "Закрыть" : "Изменить профиль"}</button> : null}</div></div>
        {canWriteProfile && remoteProfile ? <div className="profile-avatar-actions"><input ref={avatarInput} type="file" accept="image/png,image/jpeg,image/gif" hidden onChange={(event) => { const file = event.target.files?.[0]; if (file) avatarMutation.mutate(file); }}/><button type="button" className="button ghost compact" disabled={avatarMutation.isPending || removeAvatar.isPending} onClick={() => avatarInput.current?.click()}>{avatarMutation.isPending ? "Проверяем фото…" : "Загрузить фото"}</button>{remoteProfile.picture_source === "uploaded" ? <button type="button" className="button ghost compact" disabled={avatarMutation.isPending || removeAvatar.isPending} onClick={() => removeAvatar.mutate()}>Удалить фото</button> : null}<small>PNG, JPEG или GIF · до 5 МБ · фото проходит проверку безопасности</small></div> : null}
        {editingProfile && remoteProfile ? <form className="profile-editor" onSubmit={(event) => { event.preventDefault(); updateProfile.mutate(); }}><div className="settings-form"><label className="field"><span>Имя</span><input value={profileDraft.given_name} maxLength={160} autoComplete="given-name" onChange={(event) => setProfileDraft((current) => ({...current, given_name: event.target.value}))}/></label><label className="field"><span>Фамилия</span><input value={profileDraft.family_name} maxLength={160} autoComplete="family-name" onChange={(event) => setProfileDraft((current) => ({...current, family_name: event.target.value}))}/></label><label className="field"><span>Дата рождения</span><input type="date" value={profileDraft.birthdate} onChange={(event) => setProfileDraft((current) => ({...current, birthdate: event.target.value}))}/></label><label className="field"><span>Должность</span><input value={profileDraft.job_title} maxLength={160} autoComplete="organization-title" onChange={(event) => setProfileDraft((current) => ({...current, job_title: event.target.value}))}/></label><label className="field"><span>Отдел</span><input value={profileDraft.department} maxLength={160} autoComplete="organization" onChange={(event) => setProfileDraft((current) => ({...current, department: event.target.value}))}/></label><label className="field"><span>Телефон</span><input value={profileDraft.phone_number} maxLength={64} autoComplete="tel" onChange={(event) => setProfileDraft((current) => ({...current, phone_number: event.target.value}))}/></label></div><div className="profile-editor-actions"><button type="button" className="button ghost" onClick={() => { setProfileDraft(editableFromBackend(remoteProfile)); setEditingProfile(false); }}>Отмена</button><button type="submit" className="button primary" disabled={updateProfile.isPending}>{updateProfile.isPending ? "Сохраняем…" : "Сохранить профиль"}</button></div></form> : null}
        <dl className="settings-facts profile-facts">
          <div><dt>Должность</dt><dd>{profile.jobTitle ?? roleTitle}</dd></div>
          <div><dt>Подразделение</dt><dd>{profile.department ?? "Не указано в профиле OIDC"}</dd></div>
          <div><dt>Электронная почта</dt><dd>{profile.email ?? "Не указана в профиле OIDC"}</dd></div>
          <div><dt>Телефон</dt><dd>{profile.phoneNumber ?? "Не указан в профиле OIDC"}</dd></div>
          <div><dt>Дата рождения</dt><dd>{formatBirthdate(profile.birthdate)}</dd></div>
          <div><dt>Сессия действует до</dt><dd>{formatExpiry(session.expiresAt)}</dd></div>
        </dl>
        <p className="settings-note profile-source-note">Имя пользователя и электронная почта синхронизируются с Keycloak. Личные данные, фото, версии изменений и результаты проверки изображения хранятся в TORGNEXA в текущем рабочем пространстве.</p>
        {canWriteProfile && remoteProfile ? <div className="profile-privacy-actions"><div><strong>Ваши данные</strong><small>Запрос на выгрузку или удаление проходит через защищённый privacy-workflow.</small></div><div className="profile-privacy-buttons"><button type="button" className="button ghost compact" disabled={privacyRequest.isPending} onClick={() => privacyRequest.mutate("export")}>Запросить выгрузку</button><button type="button" className="button danger compact" disabled={privacyRequest.isPending} onClick={() => { if (window.confirm("Создать запрос на удаление профиля и доступа к workspace?")) privacyRequest.mutate("deletion"); }}>Запросить удаление</button></div></div> : null}
        {privacyRequest.isSuccess ? <p className="settings-note">Запрос принят и поставлен в очередь privacy-workflow.</p> : null}
        {privacyRequest.isError ? <ErrorBlock retry={() => privacyRequest.reset()}>Не удалось создать запрос по персональным данным.</ErrorBlock> : null}
        {profileQuery.isError ? <ErrorBlock retry={() => void profileQuery.refetch()}>Не удалось загрузить сохранённый профиль.</ErrorBlock> : null}
        {updateProfile.isError ? <ErrorBlock retry={() => updateProfile.reset()}>Не удалось сохранить профиль. Обновите данные и повторите.</ErrorBlock> : null}
        {avatarMutation.isError ? <ErrorBlock retry={() => avatarMutation.reset()}>Не удалось загрузить фото. Проверьте формат, размер и результат проверки безопасности.</ErrorBlock> : null}
        {removeAvatar.isError ? <ErrorBlock retry={() => removeAvatar.reset()}>Не удалось удалить фото профиля.</ErrorBlock> : null}
      </section>
      <section className="panel settings-card">
        <p className="eyebrow">Безопасность</p>
        <h2>Пароль и способы входа</h2>
        <p className="settings-copy">Смена пароля, восстановление доступа и привязанные способы входа управляются в защищённом кабинете Keycloak.</p>
        <button className="button primary" onClick={() => void auth.manageAccount()}>Управлять учётной записью</button>
        <small className="settings-note">TORGNEXA не видит и не сохраняет ваш пароль.</small>
      </section>
      </div>
      <WorkspaceSettings />
      <IntegrationCatalog />
      <AIProviderSettings />
      {isAdmin?<section className="panel settings-card danger-zone"><div><p className="eyebrow">Демо-данные</p><h2>Удалить демонстрационный набор</h2><p className="settings-copy">Операция чувствительная: сначала создаётся запрос, затем его нужно одобрить и выполнить в разделе «Согласования».</p></div><button className="button danger" disabled={deleteDemo.isPending} onClick={()=>deleteDemo.mutate()}>{deleteDemo.isPending?"Создаём запрос…":"Запросить удаление"}</button>{deleteDemo.isSuccess?<span className="status status-active">Запрос создан в «Согласованиях»</span>:null}{deleteDemo.isError?<ErrorBlock>Создайте активную политику удаления в разделе «Согласования».</ErrorBlock>:null}</section>:null}
    </div>
    <div id="settings-panel-access" className="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-access" tabIndex={0} hidden={activeTab !== "access"}>
      <section className="panel settings-card settings-access-card">
      <div className="settings-access-heading"><div><p className="eyebrow">УПРАВЛЕНИЕ ДОСТУПОМ</p><h2>Роли и права</h2><p className="settings-copy">Доступ собран из ролей текущего рабочего пространства и проверяется сервером при каждом действии.</p></div><span className="settings-access-source"><span className="settings-access-source-dot"/>Единый вход</span></div>
      <div className="settings-access-summary" aria-label="Сводка доступа"><div><strong>{roles.length}</strong><span>ролей назначено</span></div><div><strong>{capabilities.length}</strong><span>прав доступно</span></div><div><strong>OIDC</strong><span>источник авторизации</span></div></div>
      <div className="settings-access-roles"><div className="settings-access-section-heading"><div><h3>Роли рабочего пространства</h3><span>Набор полномочий, выданный вашей учётной записи</span></div></div><div className="settings-role-list">{roles.length ? roles.map((role) => <span className="settings-role-card" title={role} key={role}><span className="settings-role-icon"><Icon name="check" size={14}/></span><span><strong>{roleLabels[role] ?? role}</strong><small>{role === "admin" ? "Полный доступ" : "Назначенная роль"}</small></span></span>) : <span className="settings-empty-access">Роли не переданы провайдером</span>}</div></div>
      <div className="settings-access-divider"/>
      <div className="settings-access-section-heading settings-capabilities-heading"><div><h3>Разрешения</h3><span>Что можно просматривать и изменять в рабочем контуре</span></div><button type="button" className="button ghost compact-action" onClick={() => setShowTechnicalAccess((value) => !value)}>{showTechnicalAccess ? "Скрыть коды" : "Показать технические коды"}</button></div>
      <div className="settings-capability-groups">{groupedCapabilities.map((group) => <section className="settings-capability-group" key={group.id}><header><span className="settings-capability-icon"><Icon name={group.icon} size={16}/></span><div><h4>{group.title}</h4><small>{group.description}</small></div><b>{group.items.length}</b></header><div className="settings-capability-list">{group.items.map((capability) => <div className="settings-capability-item" title={capability} key={capability}><span className="settings-capability-check"><Icon name="check" size={13}/></span><span><strong>{capabilityLabels[capability] ?? capability}</strong>{showTechnicalAccess ? <code>{capability}</code> : null}</span></div>)}</div></section>)}</div>
      </section>
      <MemberSettings />
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
