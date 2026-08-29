import {useMemo,useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {useAuth} from "../auth/AuthProvider";
import {Page} from "./Page";
import {DataTable} from "../components/DataTable";
import {Drawer} from "../components/Drawer";
import {StatusBadge} from "../components/StatusBadge";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {navigate,useLocationPath} from "../shell/useLocationPath";
import {Icon} from "../components/Icon";
import {labelFor,connectorHealthLabels} from "../components/labels";

type Incident={id:string;kind:"warehouse"|"drift"|"connector"|"approval";title:string;entity:string;status:string;severity:string;opened:string;detail:any};
const items=(v:any)=>Array.isArray(v?.items)?v.items:[];
const severity=(status:string)=>/lost|unavailable|failed|error|critical/i.test(status)?"critical":/degraded|open|pending|rate|warning/i.test(status)?"warning":"info";
const kindName:Record<Incident["kind"],string>={warehouse:"Склад",drift:"Расхождение",connector:"Интеграция",approval:"Согласование"};
const terminal=(status:string)=>["completed","closed","resolved","cancelled","fulfilled","approved","rejected","failed"].includes(status.toLowerCase());

export function IncidentCenterPage(){
 const api=useApi() as any,{session}=useAuth(),path=useLocationPath(),caps=session?.capabilities??[];
 const [view,setView]=useState<"active"|"history">("active");
 const warehouses=useQuery({queryKey:["incident-center","warehouses"],enabled:caps.includes("stock.read"),queryFn:async()=>items((await api.listWarehouseIncidents()).body)});
 const sync=useQuery({queryKey:["incident-center","sync"],enabled:caps.includes("sync.read"),queryFn:async()=>(await api.getSyncStatus()).body as any});
 const connectors=useQuery({queryKey:["incident-center","connectors"],enabled:caps.includes("connectors.read"),queryFn:async()=>items((await api.listConnectorAccounts({limit:100})).body)});
 const approvals=useQuery({queryKey:["incident-center","approvals"],enabled:caps.includes("approvals.read"),queryFn:async()=>items((await api.listApprovals()).body)});
 const allRows=useMemo<Incident[]>(()=>[
  ...(warehouses.data??[]).map((v:any)=>({id:String(v.ID),kind:"warehouse" as const,title:`Склад ${v.WarehouseID}`,entity:String(v.WarehouseID),status:String(v.Status),severity:severity(`${v.OperationalState} ${v.Status}`),opened:String(v.OpenedAt),detail:v})),
  ...((sync.data?.drifts??[]) as any[]).map(v=>({id:String(v.id),kind:"drift" as const,title:`Расхождение ${v.kind}`,entity:String(v.local_entity_id||v.remote_id||"—"),status:String(v.status),severity:"warning",opened:String(v.detected_at),detail:v})),
  ...(connectors.data??[]).filter((v:any)=>String(v.status)==="active"&&!["healthy","unknown"].includes(String(v.health_status))).map((v:any)=>({id:String(v.id),kind:"connector" as const,title:`${v.connector_id}: ${labelFor(String(v.health_status),connectorHealthLabels)}`,entity:String(v.id),status:String(v.health_status),severity:severity(String(v.health_status)),opened:String(v.health_checked_at??v.updated_at??new Date().toISOString()),detail:v})),
  ...(approvals.data??[]).map((v:any)=>({id:String(v.id),kind:"approval" as const,title:String(v.state)==="pending"?"Требуется согласование":"Согласование завершено",entity:String(v.action??v.resource_id??v.id),status:String(v.state),severity:String(v.state)==="pending"?"warning":"info",opened:String(v.created_at??new Date().toISOString()),detail:v}))
 ].sort((a,b)=>Date.parse(b.opened)-Date.parse(a.opened)),[warehouses.data,sync.data,connectors.data,approvals.data]);
 const activeRows=useMemo(()=>allRows.filter(v=>!terminal(v.status)),[allRows]);
 const historyRows=useMemo(()=>allRows.filter(v=>terminal(v.status)),[allRows]);
 const rows=view==="active"?activeRows:historyRows;
 const parts=path.split("/").filter(Boolean),selected=parts[0]==="incidents"&&parts.length>=3?allRows.find(v=>v.kind===parts[1]&&v.id===decodeURIComponent(parts.slice(2).join("/"))):undefined;
 const loading=[warehouses,sync,connectors,approvals].some(v=>v.isFetching&&!v.data),failed=[warehouses,sync,connectors,approvals].some(v=>v.isError),retry=()=>{void Promise.all([warehouses.refetch(),sync.refetch(),connectors.refetch(),approvals.refetch()])};
 const columns=[{key:"severity",label:"Приоритет",value:(v:Incident)=>v.severity,render:(v:Incident)=><StatusBadge value={v.severity}/>},{key:"kind",label:"Источник",value:(v:Incident)=>v.kind,render:(v:Incident)=><span className="incident-kind"><Icon name={v.kind==="warehouse"?"warehouse":v.kind==="drift"?"sync":v.kind==="connector"?"connectors":"approvals"}/>{kindName[v.kind]}</span>},{key:"title",label:"Проблема",value:(v:Incident)=>`${v.title} ${v.entity}`,render:(v:Incident)=><span><strong>{v.title}</strong><small className="table-subline mono">{v.entity}</small></span>},{key:"status",label:"Статус",value:(v:Incident)=>v.status,render:(v:Incident)=><StatusBadge value={v.status}/>},{key:"opened",label:"Обнаружено",value:(v:Incident)=>v.opened,render:(v:Incident)=><time>{new Date(v.opened).toLocaleString("ru-RU")}</time>}];
 return <Page eyebrow="Операции" title="Центр инцидентов" description="Единая очередь складских сбоев, расхождений синхронизации, проблем интеграций и действий, требующих решения оператора.">
  <div className="incident-view-tabs catalog-tabs" role="tablist" aria-label="Представление инцидентов"><button type="button" role="tab" aria-selected={view==="active"} className={view==="active"?"active":""} onClick={()=>setView("active")}>Активные <span className="tab-count">{activeRows.length}</span></button><button type="button" role="tab" aria-selected={view==="history"} className={view==="history"?"active":""} onClick={()=>setView("history")}>История <span className="tab-count">{historyRows.length}</span></button></div>
  <div className="hero-grid"><article className="metric-card primary-metric"><span>Открыто</span><strong>{activeRows.length}</strong><small>операционных проблем</small></article><article className="metric-card"><span>Критические</span><strong>{activeRows.filter(v=>v.severity==="critical").length}</strong><small>требуют немедленного внимания</small></article><article className="metric-card"><span>Автоматизация</span><strong>{activeRows.filter(v=>v.kind==="warehouse"||v.kind==="drift").length}</strong><small>оркестрация уже работает</small></article></div>
  {loading?<LoadingBlock/>:failed?<ErrorBlock retry={retry}>Часть источников центра инцидентов недоступна. Доступные данные всё равно показаны.</ErrorBlock>:null}
  <DataTable rows={rows} columns={columns} rowKey={v=>`${v.kind}:${v.id}`} searchPlaceholder="Склад, расхождение, кабинет, согласование…" onOpen={v=>navigate(`/incidents/${v.kind}/${encodeURIComponent(v.id)}`)}/>
  <Drawer open={!!selected} title={selected?.title??"Инцидент"} subtitle={selected?`${kindName[selected.kind]} · ${selected.id}`:undefined} onClose={()=>navigate("/incidents")}><>{selected?<IncidentDetail incident={selected}/>:null}</></Drawer>
 </Page>
}
function IncidentDetail({incident}:{incident:Incident}){const v=incident.detail;return <div className="catalog-stack"><div className="drawer-kpis"><div><small>Приоритет</small><StatusBadge value={incident.severity}/></div><div><small>Статус</small><StatusBadge value={incident.status}/></div><div><small>Источник</small><strong>{kindName[incident.kind]}</strong></div></div><section className="drawer-section"><h3>Влияние</h3><dl className="detail-list"><div><dt>Сущность</dt><dd className="mono">{incident.entity}</dd></div><div><dt>Обнаружено</dt><dd>{new Date(incident.opened).toLocaleString("ru-RU")}</dd></div>{incident.kind==="warehouse"?<><div><dt>Операционное состояние</dt><dd>{v.OperationalState}</dd></div><div><dt>Переназначено резервов</dt><dd>{v.ReroutedAllocationCount??0}</dd></div><div><dt>Требует человека</dt><dd>{v.ExecutionAttentionCount??0}</dd></div></>:null}{incident.kind==="drift"?<><div><dt>Локальное состояние</dt><dd className="mono">{v.local_entity_id||"—"} · {v.local_status||"—"}</dd></div><div><dt>Состояние во внешней системе</dt><dd className="mono">{v.remote_id||"—"} · {v.remote_status||"—"}</dd></div><div><dt>Рекомендация</dt><dd>{v.recommended_action||"Проверка"}</dd></div></>:null}{incident.kind==="connector"?<><div><dt>Интеграция</dt><dd>{v.connector_id}</dd></div><div><dt>Проверка соединения</dt><dd>{v.health_status}</dd></div></>:null}</dl></section><section className="drawer-section"><h3>Следующее действие</h3><p className="drawer-help">{incident.kind==="warehouse"?"Проверьте автоматическое переназначение резервов исполнения и очередь, требующую внимания.":incident.kind==="drift"?"Сравните локальное и внешнее состояние и разрешите расхождение в разделе синхронизации.":incident.kind==="connector"?"Запустите проверку соединения, проверьте лимит запросов и авторизацию кабинета.":"Откройте согласование и примите решение с учётом влияния операции."}</p><button className="button primary" onClick={()=>navigate(incident.kind==="warehouse"?"/inventory":incident.kind==="drift"?"/sync":incident.kind==="connector"?"/integrations":"/approvals")}>Перейти к разделу</button></section></div>}
