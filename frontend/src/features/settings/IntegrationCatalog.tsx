import {useEffect,useMemo,useState,type CSSProperties} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {connectorCatalog,type ConnectorCatalogEntry} from "../../generated/connector-catalog";
import {ConnectorBootstrapControls} from "./ConnectorBootstrapControls";
import {Drawer} from "../../components/Drawer";
import {Icon} from "../../components/Icon";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";

interface CapabilitySetting {capability:string;direction:"read"|"write";risk:"read"|"write_sensitive";approval_required:boolean;enabled:boolean}
interface ConnectorAccount {id:string;connector_id:string;status:string;version:number;health_status:string;health_reason_code?:string;capabilities:CapabilitySetting[]}

const accountIDPattern=/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/;
const familyLabels:Readonly<Record<string,string>>={marketplace:"Маркетплейсы",classified:"Объявления",social:"Социальные сети",storefront:"Интернет-магазины",erp:"ERP",crm:"CRM",logistics:"Доставка",pickup:"ПВЗ",payment:"Платежи",edo:"ЭДО",government:"Госсистемы",fx:"Курсы валют",ai:"AI-провайдеры",notification:"Уведомления"};
const familyDescriptions:Readonly<Record<string,string>>={
 marketplace:"Каталог, цены, остатки и заказы торговой площадки — в одном подключении.",
 classified:"Публикуйте объявления и обрабатывайте обращения из классифайда.",
 social:"Управляйте публикациями, сообщениями и аналитикой социальных каналов.",
 storefront:"Синхронизируйте каталог, заказы и остатки собственного интернет-магазина.",
 erp:"Свяжите мастер-данные, складские остатки и заказы с учётной системой.",
 crm:"Синхронизируйте клиентов, сделки и товарные строки с CRM.",
 logistics:"Рассчитывайте доставку, создавайте отправления и отслеживайте статусы.",
 pickup:"Подключите пункты выдачи и операции последней мили.",
 payment:"Принимайте платежи и сверяйте финансовые операции.",
 edo:"Обменивайтесь документами и управляйте запросами на подпись.",
 government:"Работайте с регулируемыми данными через защищённый контур.",
 fx:"Получайте проверенные курсы валют для расчётов и отчётности.",
 ai:"Подключите модель для управляемого анализа без обхода политик TORGNEXA.",
 notification:"Доставляйте операционные уведомления по выбранному каналу.",
};
const authLabels:Readonly<Record<string,string>>={api_key:"API key",oauth2:"OAuth 2.0",bearer:"Bearer token",basic:"Логин и пароль",certificate:"Сертификат",none:"Без авторизации"};
const capabilityAreaLabels:Readonly<Record<string,string>>={products:"Товары",inventory:"Остатки",orders:"Заказы",returns:"Возвраты",prices:"Цены",catalog:"Каталог",listings:"Объявления",publications:"Публикации",messages:"Сообщения",leads:"Лиды",stats:"Статистика",analytics:"Аналитика",post:"Контент",webhooks:"Вебхуки",entities:"Сделки",productrows:"Товарные строки",rates:"Тарифы",shipment:"Отправления",track:"Отслеживание",label:"Этикетки",points:"Пункты выдачи",documents:"Документы",references:"Справочники",reconciliation:"Сверка",status:"Статусы",completion:"Генерация",payments:"Платежи"};

function decodeAccounts(value:unknown):ConnectorAccount[]{if(!value||typeof value!=="object"||!Array.isArray((value as {items?:unknown}).items))throw new Error("invalid connector account response");return (value as {items:ConnectorAccount[]}).items}
function capabilityLabel(value:string){const [scope,action]=value.split(".").slice(-2);const actionLabel:Record<string,string>={read:"Получать данные",write:"Передавать данные",reply:"Отвечать",create:"Создавать",cancel:"Отменять",verify:"Проверять",send:"Отправлять",sign_request:"Запрашивать подпись"};return `${actionLabel[action]??action} · ${scope??value}`}
function connectorInitials(name:string){return name.split(/\s+/).map(value=>value[0]).join("").slice(0,2).toUpperCase()}
function connectorCardStyle(entry:ConnectorCatalogEntry):CSSProperties{
 let hash=0;
 for(const symbol of entry.id)hash=(hash*31+symbol.charCodeAt(0))|0;
 const hue=Math.abs(hash)%360,presentation=entry.presentation;
 return {
  "--connector-surface":presentation?.surface??`hsl(${hue} 84% 92%)`,
  "--connector-surface-alt":presentation?.surfaceAlt??`hsl(${(hue+38)%360} 82% 84%)`,
  "--connector-on-surface":presentation?.foreground??`hsl(${hue} 45% 22%)`,
  "--connector-accent":presentation?.accent??`hsl(${hue} 62% 42%)`,
 } as unknown as CSSProperties;
}
function capabilityAreas(entry:ConnectorCatalogEntry){
 const capabilities=entry.runtime.stage==="planned"?entry.capabilities:entry.runtime.operationalCapabilities;
 return [...new Set(capabilities.map(value=>{const parts=value.split(".");const area=parts.at(-2)??parts[0]??value;return capabilityAreaLabels[area]??area.replaceAll("_"," ")}))];
}
function accountCountLabel(count:number){const mod100=count%100,mod10=count%10;if(mod100>=11&&mod100<=14)return `${count} кабинетов`;if(mod10===1)return `${count} кабинет`;if(mod10>=2&&mod10<=4)return `${count} кабинета`;return `${count} кабинетов`}
function connectionState(entry:ConnectorCatalogEntry,linked:number,healthy:number,pending:boolean){
 if(entry.runtime.stage==="planned")return {tone:"planned",label:"В плане · подключение закрыто"};
 if(entry.runtime.stage==="separate_surface")return {tone:"separate",label:"Доступно в отдельном разделе"};
 if(pending)return {tone:"loading",label:"Проверяем подключение"};
 if(linked===0)return {tone:"available",label:"Можно подключить"};
 if(healthy===linked)return {tone:"healthy",label:`${accountCountLabel(linked)} · работают`};
 if(healthy>0)return {tone:"warning",label:`${healthy} из ${linked} работают`};
 return {tone:"warning",label:`${accountCountLabel(linked)} · нужна проверка`};
}

function ConnectorLogo({entry}:{entry:ConnectorCatalogEntry}){
 return entry.presentation
  ?<span className="connector-logo connector-logo-branded" aria-hidden="true"><img src={entry.presentation.logo} alt=""/></span>
  :<span className="connector-logo" aria-hidden="true">{connectorInitials(entry.name)}</span>;
}

export function IntegrationCatalog(){
 const api=useApi(),cache=useQueryClient(),toast=useToast();
 const families=useMemo(()=>[...new Set(connectorCatalog.map(entry=>entry.family))].sort(),[]);
 const [family,setFamily]=useState("marketplace"),[search,setSearch]=useState(""),[selected,setSelected]=useState<ConnectorCatalogEntry|null>(null),[accountId,setAccountId]=useState(""),[credentials,setCredentials]=useState<Record<string,string>>({}),[capDrafts,setCapDrafts]=useState<Record<string,readonly string[]>>({});
 const accounts=useQuery({queryKey:["connector-accounts"],queryFn:async()=>decodeAccounts((await api.listConnectorAccounts({limit:100})).body),staleTime:10_000,retry:3,refetchOnWindowFocus:true});
 const refresh=()=>cache.invalidateQueries({queryKey:["connector-accounts"]});
 const create=useMutation({mutationFn:async(connectorId:string)=>{const id=accountId.trim();if(!accountIDPattern.test(id))throw new Error("invalid-account-id");await api.createConnectorAccount({idempotencyKey:id,body:{account_id:id,connector_id:connectorId}})},onSuccess:async()=>{toast.push({kind:"success",title:"Кабинет добавлен",body:"Теперь сохраните учётные данные и разрешённые операции."});setAccountId("");await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось добавить кабинет",body:"Проверьте идентификатор и права доступа."})});
 const enroll=useMutation({mutationFn:async(account:ConnectorAccount)=>{const material=credentials[account.id]??"";if(!material)throw new Error("empty credentials");const bytes=new TextEncoder().encode(material);let binary="";for(const byte of bytes)binary+=String.fromCharCode(byte);await api.enrollConnectorCredentials({idempotencyKey:`credentials:${account.id}:${account.version}`,body:{account_id:account.id,expected_version:account.version,material_base64:btoa(binary)}})},onSuccess:async(_d,account)=>{setCredentials(current=>({...current,[account.id]:""}));toast.push({kind:"success",title:"Учётные данные зашифрованы"});await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось сохранить учётные данные"})});
 const capabilities=useMutation({mutationFn:async({account,enabled}:{account:ConnectorAccount;enabled:readonly string[]})=>api.replaceConnectorAccountCapabilities({idempotencyKey:`capabilities:${account.id}:${account.version}`,body:{account_id:account.id,expected_version:account.version,enabled:[...enabled]}}),onSuccess:async(_r,value)=>{setCapDrafts(current=>{const next={...current};delete next[value.account.id];return next});toast.push({kind:"success",title:"Возможности обновлены"});await refresh()}});
 const check=useMutation({mutationFn:(account:ConnectorAccount)=>api.checkConnectorAccount({idempotencyKey:`check:${account.id}:${account.version}`,body:{account_id:account.id,expected_version:account.version}}),onSuccess:async()=>{toast.push({kind:"success",title:"Проверка подключения завершена"});await refresh()}});
 const enable=useMutation({mutationFn:(account:ConnectorAccount)=>api.enableConnectorAccount({idempotencyKey:`enable:${account.id}:${account.version}`,body:{account_id:account.id,expected_version:account.version}}),onSuccess:refresh});
 const disable=useMutation({mutationFn:(account:ConnectorAccount)=>api.disableConnectorAccount({idempotencyKey:`disable:${account.id}:${account.version}`,body:{account_id:account.id,expected_version:account.version}}),onSuccess:refresh});
 const sync=useMutation({mutationFn:(account:ConnectorAccount)=>api.syncConnectorAccount({idempotencyKey:`sync:${account.id}:${Date.now()}`,body:{account_id:account.id,expected_version:account.version}}),onSuccess:()=>toast.push({kind:"success",title:"Синхронизация поставлена в очередь"})});
 const startOAuth=useMutation({mutationFn:async(account:ConnectorAccount)=>{const callbackUrl=new URL("/oauth/connectors/callback",window.location.origin).toString();const response=await api.startConnectorOAuth({idempotencyKey:`oauth-start:${account.id}:${account.version}`,body:{account_id:account.id,expected_version:account.version,callback_url:callbackUrl}});const body=response.body as {authorization_url?:unknown};if(!body||typeof body.authorization_url!=="string"||!body.authorization_url.startsWith("https://"))throw new Error("invalid OAuth response");window.location.assign(body.authorization_url)}});
 const visible=connectorCatalog.filter(entry=>entry.family===family&&`${entry.name} ${entry.capabilities.join(" ")}`.toLowerCase().includes(search.trim().toLowerCase()));
 const readyCount=connectorCatalog.filter(entry=>entry.runtime.stage==="ready").length,separateCount=connectorCatalog.filter(entry=>entry.runtime.stage==="separate_surface").length;

 return <section className="settings-section">
  <div className="section-heading integration-catalog-heading">
   <div><p className="eyebrow">Интеграции</p><h2>Каталог интеграций</h2><p>Каталог показывает только фактически подключённые к runtime операции. Возможности из манифеста без исполняемого маршрута нельзя включить.</p></div>
   <div className="integration-readiness-summary"><span className="status status-active">{readyCount} работают</span><span className="status status-info">{separateCount} в отдельных разделах</span><span className="status status-disabled">{connectorCatalog.length-readyCount-separateCount} в плане</span></div>
  </div>
  <div className="integration-toolbar">
   <label className="table-search"><Icon name="search"/><input value={search} onChange={event=>setSearch(event.target.value)} placeholder="Поиск по каталогу…" aria-label="Поиск по каталогу интеграций"/></label>
   <div className="family-tabs" role="tablist" aria-label="Семейства интеграций">{families.map(value=><button key={value} role="tab" aria-selected={family===value} className={`family-tab ${family===value?"active":""}`} onClick={()=>setFamily(value)}>{familyLabels[value]??value}</button>)}</div>
  </div>
  {accounts.isError?<div className="alert error"><span>Не удалось загрузить кабинеты.</span><button className="button ghost" onClick={()=>void accounts.refetch()}>Повторить</button></div>:null}
  {visible.length===0?<div className="integration-no-results"><Icon name="search"/><strong>Ничего не найдено</strong><span>Попробуйте изменить запрос или выбрать другую категорию.</span></div>:<div className="integration-grid integration-overview">{visible.map(entry=>{
   const linked=(accounts.data??[]).filter(account=>account.connector_id===entry.id),healthy=linked.filter(value=>value.health_status==="healthy").length,areas=capabilityAreas(entry),state=connectionState(entry,linked.length,healthy,accounts.isPending);
   return <article className={`panel integration-summary-card integration-market-card runtime-${entry.runtime.stage}`} style={connectorCardStyle(entry)} key={entry.id}>
    <button type="button" className="integration-card-hit-target" onClick={()=>setSelected(entry)} aria-label={`Открыть настройки ${entry.name}`}/>
    <div className="integration-card-visual">
     <ConnectorLogo entry={entry}/>
     <span className="integration-family-badge">{familyLabels[entry.family]??entry.family}</span>
    </div>
    <div className="integration-card-content">
     <div className="integration-card-title"><div><h3>{entry.name}</h3><small>{authLabels[entry.authKinds[0]]??entry.authKinds[0]}</small></div></div>
     <p className="integration-card-description">{familyDescriptions[entry.family]??"Подключите внешний сервис к рабочему пространству TORGNEXA."}</p>
     <div className="integration-capability-tags" aria-label={entry.runtime.stage==="planned"?"Возможности по контракту адаптера":"Работающие возможности"}>{areas.slice(0,3).map(area=><span key={area}>{entry.runtime.stage==="planned"?`Заявлено: ${area}`:area}</span>)}{areas.length>3?<span className="integration-capability-more">+{areas.length-3}</span>:null}</div>
     <div className="integration-card-footer"><span className={`integration-connection integration-connection-${state.tone}`}><i/>{state.label}</span><span className="integration-card-action">{entry.runtime.stage==="ready"?"Настроить":"Подробнее"} <Icon name="chevron"/></span></div>
    </div>
   </article>;
  })}</div>}
  <Drawer open={!!selected} title={selected?.name??"Интеграция"} subtitle={selected?`${familyLabels[selected.family]??selected.family} · ${authLabels[selected.authKinds[0]]??selected.authKinds[0]}`:undefined} onClose={()=>setSelected(null)}><>{selected?<div className="connector-drawer">{selected.runtime.stage==="planned"?<section className="drawer-section runtime-availability-note"><StatusBadge value="planned"/><h3>Подключение пока недоступно</h3><p className="drawer-help">Для адаптера проверены манифест и SDK-контракт, но в текущем production runtime нет исполняемого domain bridge. Создать кабинет или включить заявленные возможности нельзя.</p></section>:selected.runtime.stage==="separate_surface"&&selected.runtime.surface!=="social"&&!(selected.runtime.surface==="finance"&&selected.runtime.operationalCapabilities.includes("payments.create"))?<SeparateSurfaceNotice entry={selected} close={()=>setSelected(null)}/>:<>{selected.runtime.surface==="social"?<section className="drawer-section runtime-availability-note"><StatusBadge value="active"/><h3>Текстовые публикации работают</h3><p className="drawer-help">Сохраните bot token, заполните chat_id из шаблона настройки, проверьте права бота, включите social.post.text и активируйте кабинет. Точный лимит текста зависит от провайдера.</p><button className="button secondary" onClick={()=>window.location.assign("/social")}>Открыть публикации</button></section>:selected.runtime.surface==="finance"&&selected.runtime.operationalCapabilities.includes("payments.create")?<section className="drawer-section runtime-availability-note"><StatusBadge value="active"/><h3>Платежи работают</h3><p className="drawer-help">Сохраните учётные данные платёжного шлюза, заполните шаблон настройки (для СБП — хост эквайера), включите payments.create и активируйте кабинет.</p><button className="button secondary" onClick={()=>window.location.assign("/finance")}>Открыть платежи</button></section>:null}<section className="drawer-section"><h3>Добавить кабинет</h3><p className="drawer-help">Создайте логическое подключение. Секреты будут сохранены отдельно в SecretProvider.</p><div className="drawer-inline"><input value={accountId} onChange={event=>setAccountId(event.target.value)} placeholder="main-account"/><button className="button primary" disabled={create.isPending||!accountIDPattern.test(accountId.trim())} onClick={()=>create.mutate(selected.id)}>Добавить</button></div></section>{(accounts.data??[]).filter(account=>account.connector_id===selected.id).map(account=><AccountCard key={account.id} account={account} entry={selected} credential={credentials[account.id]??""} setCredential={value=>setCredentials(current=>({...current,[account.id]:value}))} enabled={capDrafts[account.id]??account.capabilities.filter(value=>value.enabled).map(value=>value.capability)} setEnabled={value=>setCapDrafts(current=>({...current,[account.id]:value}))} enroll={()=>enroll.mutate(account)} saveCapabilities={enabled=>capabilities.mutate({account,enabled})} check={()=>check.mutate(account)} enable={()=>enable.mutate(account)} disable={()=>disable.mutate(account)} sync={()=>sync.mutate(account)} oauth={()=>startOAuth.mutate(account)} busy={enroll.isPending||capabilities.isPending||check.isPending||enable.isPending||disable.isPending||sync.isPending||startOAuth.isPending}/>)}</>}</div>:null}</></Drawer>
 </section>;
}

function SeparateSurfaceNotice({entry,close}:{entry:ConnectorCatalogEntry;close:()=>void}){
 const finance=entry.runtime.surface==="finance";
 return <section className="drawer-section runtime-availability-note"><StatusBadge value="active"/><h3>Работает в отдельном разделе</h3><p className="drawer-help">{finance?"Worker автоматически получает официальные курсы Банка России и сохраняет неизменяемые факты. Учётные данные не требуются.":"Этот провайдер подключается через управляемый контур AI-аналитики, а не через общую синхронизацию кабинетов."}</p><button className="button primary" onClick={()=>{close();if(finance){window.location.assign("/finance");return}document.getElementById("ai-provider-settings")?.scrollIntoView({behavior:"smooth",block:"start"})}}>{finance?"Перейти к курсам валют":"Перейти к AI-провайдерам"}</button></section>;
}

function AccountCard({account,entry,credential,setCredential,enabled,setEnabled,enroll,saveCapabilities,check,enable,disable,sync,oauth,busy}:{account:ConnectorAccount;entry:ConnectorCatalogEntry;credential:string;setCredential:(value:string)=>void;enabled:readonly string[];setEnabled:(value:readonly string[])=>void;enroll:()=>void;saveCapabilities:(value:readonly string[])=>void;check:()=>void;enable:()=>void;disable:()=>void;sync:()=>void;oauth:()=>void;busy:boolean}){
 const [advanced,setAdvanced]=useState(false);
 const operational=new Set(entry.runtime.operationalCapabilities),safeEnabled=enabled.filter(capability=>operational.has(capability));
 const needsLogin=account.health_reason_code==="oauth_reauthorization_required";
 return <section className="connector-account"><header><div><strong>{account.id}</strong><small>версия {account.version}</small></div><div className="account-status"><StatusBadge value={account.health_status}/><StatusBadge value={account.status}/></div></header>{entry.authKinds.includes("none")?<p className="drawer-help">Авторизация для этой интеграции не требуется.</p>:<div className="credential-box">{entry.oauthGrantType==="authorization_code"?<p className={needsLogin?"drawer-help error-text":"drawer-help"}>{needsLogin?"Срок авторизации закончился или доступ был отозван. Войдите снова.":"После первого входа токен обновляется автоматически. Повторный вход нужен только после отзыва доступа."}</p>:null}<label className="field"><span>Учётные данные</span><textarea value={credential} maxLength={65536} autoComplete="off" placeholder={entry.authKinds.includes("oauth2")?"OAuth client JSON":"Секретный материал"} onChange={event=>setCredential(event.target.value)}/></label><div className="button-row"><button className="button secondary" disabled={busy||!credential} onClick={enroll}>Сохранить зашифрованно</button>{entry.oauthGrantType==="authorization_code"?<button className="button primary" disabled={busy} onClick={oauth}>{needsLogin?"Войти снова":"Войти"}</button>:null}</div></div>}{entry.runtime.runtimeConfigTemplate?<RuntimeConfigEditor account={account} template={entry.runtime.runtimeConfigTemplate}/>:null}<button className="advanced-toggle" onClick={()=>setAdvanced(value=>!value)}><span><Icon name="settings"/>Разрешённые возможности</span><span>{safeEnabled.length}/{operational.size}<Icon name="chevron"/></span></button>{advanced?<div className="capability-list">{entry.capabilities.map(capability=>{const available=operational.has(capability),on=available&&safeEnabled.includes(capability),policy=account.capabilities.find(value=>value.capability===capability);return <label className={available?undefined:"capability-unavailable"} key={capability}><input type="checkbox" disabled={!available} checked={on} onChange={event=>setEnabled(event.target.checked?[...safeEnabled,capability]:safeEnabled.filter(value=>value!==capability))}/><span><strong>{capabilityLabel(capability)}</strong><small>{available?(policy?.direction==="write"?"Работает · запись требует политики":"Работает · чтение"):"Заявлено в манифесте · runtime-маршрут не подключён"}</small></span></label>})}<button className="button secondary" disabled={busy} onClick={()=>saveCapabilities(safeEnabled)}>Сохранить работающие возможности</button></div>:null}<div className="account-actions"><button className="button ghost" disabled={busy} onClick={check}><Icon name="refresh"/> Проверить</button>{account.status==="disabled"&&account.health_status==="healthy"?<button className="button primary" disabled={busy||!safeEnabled.length} onClick={enable}>Включить</button>:null}{account.status==="active"&&entry.runtime.sync.length?<button className="button primary" disabled={busy} onClick={sync}>Синхронизировать</button>:null}{account.status!=="disabled"?<button className="button ghost danger-text" disabled={busy} onClick={disable}>Отключить</button>:null}</div><ConnectorBootstrapControls account={account}/></section>;
}

function RuntimeConfigEditor({account,template}:{account:ConnectorAccount;template:Readonly<Record<string,unknown>>}){
 const api=useApi(),toast=useToast();
 const query=useQuery({queryKey:["connector-runtime-config",account.id],retry:false,queryFn:async()=>{try{const response=await api.getConnectorRuntimeConfig({accountId:account.id});const body=response.body as {version?:unknown;config?:unknown};if(typeof body?.version!=="number"||!body.config||typeof body.config!=="object")throw new Error("invalid runtime config");return {version:body.version,config:body.config as Record<string,unknown>}}catch(error){if((error as {statusCode?:number}).statusCode===404)return {version:0,config:template as Record<string,unknown>};throw error}}});
 const [draft,setDraft]=useState("");
 useEffect(()=>{if(query.data)setDraft(JSON.stringify(query.data.config,null,2))},[query.data]);
 const save=useMutation({mutationFn:async()=>{const config=JSON.parse(draft) as Record<string,unknown>;return api.putConnectorRuntimeConfig({idempotencyKey:crypto.randomUUID(),body:{account_id:account.id,expected_version:query.data?.version??0,config}})},onSuccess:async()=>{toast.push({kind:"success",title:"Параметры runtime сохранены"});await query.refetch()},onError:()=>toast.push({kind:"error",title:"Не удалось сохранить параметры",body:"Проверьте JSON и актуальную версию конфигурации."})});
 if(query.isPending)return <p className="drawer-help">Загружаем параметры runtime…</p>;
 if(query.isError)return <p className="drawer-help error-text">Не удалось загрузить параметры runtime.</p>;
 return <div className="credential-box runtime-config-editor"><label className="field"><span>Параметры runtime · без секретов</span><textarea value={draft} spellCheck={false} onChange={event=>setDraft(event.target.value)}/></label><button className="button secondary" disabled={save.isPending||!draft.trim()} onClick={()=>save.mutate()}>{save.isPending?"Сохраняем…":"Сохранить параметры"}</button></div>;
}
