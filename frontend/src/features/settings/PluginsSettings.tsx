import {useQuery} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock,LoadingBlock} from "../../components/ApiState";
import {EmptyState} from "../../components/EmptyState";
import {StatusBadge} from "../../components/StatusBadge";

interface Listing{id:string;name:string;family:string;version:string;publisher_id:string;trust:string;license_expression:string;security_contact:string;published_at:string;requested_permissions:{capabilities:string[];secret_classes?:string[];network?:Array<{host:string;port:number}>}}
function decode(value:unknown):Listing[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid plugin marketplace response");return root.items as Listing[]}
const trustLabels:Record<string,string>={official:"Официальный",verified:"Проверенный",community:"Сообщество",private:"Приватный"};
const familyLabels:Record<string,string>={marketplace:"Маркетплейсы",classified:"Объявления",social:"Социальные сети",erp:"ERP",edo:"ЭДО",government:"Госсистемы",payment:"Платежи",logistics:"Доставка",pickup:"ПВЗ",fx:"Курсы валют",notification:"Уведомления"};

export function PluginsSettings(){
 const api=useApi(),auth=useAuth();
 const canRead=auth.session?.capabilities.includes("plugins.read")??false;
 const query=useQuery({queryKey:["settings","plugins"],queryFn:async()=>decode((await api.listPlugins({limit:200})).body),enabled:canRead,staleTime:60_000});
 if(!canRead)return null;
 return <section className="panel settings-card">
  <div className="settings-card-heading"><div><p className="eyebrow">Расширения</p><h2>Маркетплейс плагинов</h2><p className="settings-copy">Проверенные сторонние плагины, изолированные в отдельном процессе с явным набором запрошенных прав. Активация выполняется отдельно от этого обзора — здесь только то, что доступно к рассмотрению.</p></div>{query.data?<StatusBadge value={`${query.data.length}`}/>:null}</div>
  {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock>Не удалось загрузить маркетплейс плагинов.</ErrorBlock>:query.data.length===0?<EmptyState title="Плагинов пока нет" text="Каталог заполняется по мере ревью и публикации сторонних плагинов."/>:<div className="settings-grid">{query.data.map(p=><article className="connector-account" key={p.id}><header><div><strong>{p.name}</strong><small>{familyLabels[p.family]??p.family} · v{p.version} · {p.publisher_id}</small></div><StatusBadge value={trustLabels[p.trust]??p.trust}/></header><div className="chip-list">{p.requested_permissions.capabilities.map(c=><span className="chip" key={c}>{c}</span>)}{(p.requested_permissions.secret_classes??[]).map(c=><span className="chip" key={`secret:${c}`}>secret: {c}</span>)}{(p.requested_permissions.network??[]).map(n=><span className="chip" key={`net:${n.host}:${n.port}`}>{n.host}:{n.port}</span>)}</div><dl className="detail-list"><div><dt>Лицензия</dt><dd>{p.license_expression}</dd></div><div><dt>Контакт безопасности</dt><dd>{p.security_contact}</dd></div><div><dt>Опубликован</dt><dd>{new Date(p.published_at).toLocaleDateString("ru-RU")}</dd></div></dl></article>)}</div>}
 </section>
}
