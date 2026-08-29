import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {ErrorBlock} from "./ApiState";

type LineageRecord = {id:string;source:string;operation:string;result:string;occurred_at:string;output?:{field?:string}};

export function LineageTimeline({system,entityType,entityId}:{system:string;entityType:string;entityId:string}) {
  const api=useApi();
  const [open,setOpen]=useState(false);
  const querySystem=system==="catalog"?"torgnexa":system;
  const query=useQuery({queryKey:["lineage",querySystem,entityType,entityId],enabled:open,queryFn:async()=>{const body=(await api.getLineageTimeline({system:querySystem,entityType,entityId,limit:20})).body as {items?:unknown};return Array.isArray(body.items)?body.items as LineageRecord[]:[]}});
  return <section className="lineage-timeline drawer-section"><div className="drawer-section-heading"><div><h3>История происхождения данных</h3><p className="drawer-help">Неизменяемые записи источника, операции и результата для этой сущности.</p></div><button type="button" className="button ghost" onClick={()=>setOpen(value=>!value)}>{open?"Скрыть":"Показать"}</button></div>{open?(query.isPending?<p className="drawer-help">Загружаем историю…</p>:query.isError?<ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить историю происхождения.</ErrorBlock>:query.data.length===0?<p className="drawer-help">Записей происхождения пока нет.</p>:<div className="lineage-list">{query.data.map(item=><article key={item.id}><div><strong>{item.operation}</strong><small>{item.source}{item.output?.field?` · поле ${item.output.field}`:""}</small></div><span className={`status status-${item.result}`}>{item.result==="applied"?"Применено":item.result==="observed"?"Зафиксировано":"Отклонено"}</span><time>{new Date(item.occurred_at).toLocaleString("ru-RU")}</time></article>)}</div>):null}</section>;
}
