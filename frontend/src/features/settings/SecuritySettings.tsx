import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";
import {StatusBadge} from "../../components/StatusBadge";
import {auditActionLabel,auditResourceLabel,humanizeTechnicalValue} from "../../components/labels";

type Configuration={provider:string;issuer:string;client_id:string;configuration_status:string;configuration_source:string;provider_health:string};
type Session={session_ref:string;user_ref:string;status:"active"|"revoked";client_kind:string;authenticated_at:string;first_seen_at:string;last_seen_at:string;expires_at:string;revoked_at?:string;current:boolean};
type Login={id:string;session_ref:string;event_type:string;client_kind:string;occurred_at:string};
type Audit={id:string;actor_id:string;action:string;resource_type:string;resource_id:string;correlation_id:string;risk:string;created_at:string};
type Page<T>={items:T[];next_cursor:string};

const record=(value:unknown):Record<string,unknown>=>{if(!value||typeof value!=="object"||Array.isArray(value))throw new Error("invalid response");return value as Record<string,unknown>};
const page=<T,>(value:unknown):Page<T>=>{const data=record(value);if(!Array.isArray(data.items)||typeof data.next_cursor!=="string")throw new Error("invalid page");return {items:data.items as T[],next_cursor:data.next_cursor}};
const configuration=(value:unknown):Configuration=>record(value) as Configuration;
const date=(value:string)=>new Intl.DateTimeFormat("ru-RU",{dateStyle:"short",timeStyle:"short"}).format(new Date(value));
const client:Record<string,string>={browser:"Браузер",mobile:"Мобильное устройство",api:"API-клиент",unknown:"Не определён"};
const configurationSourceLabels:Record<string,string>={runtime:"Конфигурация приложения",database:"Сохранённая конфигурация",default:"Значения по умолчанию"};

export function SecuritySettings(){
 const api=useApi(),auth=useAuth(),cache=useQueryClient();
 const canRead=auth.session?.capabilities.includes("settings.security.read")??false;
 const canWrite=auth.session?.capabilities.includes("settings.security.write")??false;
 const query=useQuery({
  queryKey:["settings","security"],enabled:canRead,
  queryFn:async()=>{const [config,sessions,logins,audit]=await Promise.all([api.getSettingsSecurityConfiguration(),api.listSettingsSecuritySessions({limit:50}),api.listSettingsSecurityLogins({limit:50}),api.listSettingsSecurityAudit({limit:50})]);return {config:configuration(config.body),sessions:page<Session>(sessions.body),logins:page<Login>(logins.body),audit:page<Audit>(audit.body)}}
 });
 const revoke=useMutation({
  mutationFn:async(item:Session)=>({item,response:await api.revokeSettingsSecuritySession({sessionRef:item.session_ref,idempotencyKey:crypto.randomUUID()})}),
  onSuccess:async({item})=>{if(item.current){await auth.logout();return}await cache.invalidateQueries({queryKey:["settings","security"]})}
 });
 if(!canRead)return null;
 if(query.isPending)return <section className="panel settings-card security-settings"><LoadingBlock/></section>;
 if(query.isError||!query.data)return <section className="panel settings-card security-settings"><ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить сведения о безопасности.</ErrorBlock></section>;
 const data=query.data;
 return <section className="panel settings-card security-settings">
  <div className="settings-card-heading"><div><p className="eyebrow">Безопасность настроек</p><h2>Сессии, входы и изменения</h2></div><span className="status status-active">Контур рабочего пространства</span></div>
  <div className="security-config"><div><span>Конфигурация</span><strong>{data.config.configuration_status==="configured"?"Настроена":"Не настроена"}</strong><small>{configurationSourceLabels[data.config.configuration_source]??humanizeTechnicalValue(data.config.configuration_source)}</small></div><div><span>Провайдер</span><strong>OIDC · {data.config.client_id}</strong><small>{data.config.issuer}</small></div><div><span>Состояние провайдера</span><strong>Не проверялось</strong><small>Это состояние конфигурации, а не внешняя проверка доступности.</small></div></div>
  <div className="settings-card-heading security-subhead"><div><p className="eyebrow">Активные сессии</p><h3>Доступ к API TORGNEXA</h3></div><button className="button ghost" onClick={()=>void auth.manageAccount()}>Сессии Keycloak</button></div>
  <p className="settings-note">Отзыв блокирует последующие запросы этой сессии в TORGNEXA. Для текущей сессии интерфейс также выполнит выход из Keycloak; остальными SSO-сессиями управляйте в кабинете провайдера.</p>
  {data.sessions.items.length===0?<p className="settings-copy">Сессий пока нет.</p>:<div className="table-wrap security-session-table"><table><thead><tr><th>Сессия</th><th>Клиент</th><th>Последняя активность</th><th>Срок</th><th>Статус</th><th></th></tr></thead><tbody>{data.sessions.items.map(item=><tr key={item.session_ref}><td><strong>{item.current?"Текущая сессия":"Сессия"}</strong><br/><span className="mono">{item.session_ref.slice(0,12)} · user {item.user_ref}</span></td><td>{client[item.client_kind]??item.client_kind}</td><td>{date(item.last_seen_at)}</td><td>{date(item.expires_at)}</td><td><span className={`status status-${item.status}`}>{item.status==="active"?"Активна":"Отозвана"}</span></td><td>{item.status==="active"&&canWrite?<button className="button danger compact" disabled={revoke.isPending} onClick={()=>revoke.mutate(item)}>Отозвать</button>:null}</td></tr>)}</tbody></table></div>}
  {revoke.isError?<ErrorBlock>Не удалось отозвать сессию.</ErrorBlock>:null}
  <div className="security-columns">
   <div><div className="security-subhead"><p className="eyebrow">История входов</p><h3>События, замеченные TORGNEXA</h3></div><p className="settings-note">Это не полный журнал Keycloak: запись появляется при первом подтверждённом запросе с OIDC-сессией.</p><div className="security-timeline security-scroll-list">{data.logins.items.map(item=><article key={item.id}><strong>{item.event_type==="session_observed"?"Сессия впервые замечена":"Сессия отозвана"}</strong><span>{client[item.client_kind]??item.client_kind} · {date(item.occurred_at)}</span><small className="mono">{item.session_ref.slice(0,12)}</small></article>)}</div></div>
   <div><div className="security-subhead"><p className="eyebrow">Журнал настроек</p><h3>Субъект и связь операции</h3></div><p className="settings-note">Система мониторинга получает эти записи асинхронно и не участвует в фиксации бизнес-операции.</p><div className="security-timeline security-scroll-list">{data.audit.items.map(item=><article key={item.id}><strong>{auditActionLabel(item.action)}</strong><span>{auditResourceLabel(item.resource_type)} · {date(item.created_at)} · <StatusBadge value={item.risk}/></span><small className="mono">Субъект: {item.actor_id} · операция: {item.correlation_id}</small></article>)}</div></div>
  </div>
 </section>
}
