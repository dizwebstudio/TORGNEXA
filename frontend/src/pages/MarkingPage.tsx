import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {Page} from "./Page";
import {DataTable} from "../components/DataTable";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {StatusBadge} from "../components/StatusBadge";
import {useToast} from "../components/Toast";

type Client={getMarkingOverview(x?:object):Promise<{body:any}>;recordMarkingScan(x:object):Promise<{body:any}>};

export function MarkingPage(){
 const api=useApi() as unknown as Client;
 const query=useQuery({queryKey:["marking-overview"],queryFn:async()=>((await api.getMarkingOverview({limit:50})).body as any)});
 const [barcode,setBarcode]=useState(""),[gtin,setGtin]=useState(""),[sku,setSku]=useState(""),[action,setAction]=useState("receiving"),[expected,setExpected]=useState("1");
 const qc=useQueryClient(),toast=useToast();
 const scan=useMutation({mutationFn:()=>api.recordMarkingScan({idempotencyKey:crypto.randomUUID(),body:{barcode,gtin,sku,wms_action:action,expected_quantity:Number(expected)}}),onSuccess:async(result)=>{const value=result.body as any;toast.push({kind:value.result==="accepted"?"success":"warning",title:value.result==="accepted"?"Код принят":"Скан проверен",body:value.reason_code||"Количество обновлено."});setBarcode("");await qc.invalidateQueries({queryKey:["marking-overview"]})},onError:()=>toast.push({kind:"error",title:"Скан не записан",body:"Проверьте GTIN, SKU, количество и права оператора."})});
 if(query.isPending)return <Page eyebrow="Склад" title="Маркировка" description="Коды, печать, сканирование, упаковки и УПД."><LoadingBlock/></Page>;
 if(query.isError)return <Page eyebrow="Склад" title="Маркировка" description="Коды, печать, сканирование, упаковки и УПД."><ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить состояние маркировки.</ErrorBlock></Page>;
 const batches=query.data?.batches??[];
 const columns=[{key:"batch",label:"Партия",value:(v:any)=>`${v.batch.id} ${v.batch.sku}`,render:(v:any)=><span><strong className="mono">{v.batch.id}</strong><small className="table-subline">{v.batch.sku} · GTIN {v.batch.gtin}</small></span>},{key:"status",label:"Состояние",value:(v:any)=>v.batch.status,render:(v:any)=><StatusBadge value={v.batch.status}/>},{key:"quantity",label:"Коды",value:(v:any)=>`${v.code_count}/${v.batch.requested}`,render:(v:any)=><span>{v.code_count} / {v.batch.requested}<small className="table-subline">зарезервировано: {v.batch.reserved}</small></span>},{key:"attention",label:"Внимание",value:(v:any)=>v.open_drifts,render:(v:any)=><strong className={v.open_drifts?"danger-text":""}>{v.open_drifts||"—"}</strong>}];
 return <Page eyebrow="Склад и соответствие" title="Маркировка" description="Операторский контур кодов и упаковок. Исходный код не сохраняется в приложении: в системе остаётся только fingerprint.">
  <section className="integration-center-summary" aria-label="Сводка маркировки"><Metric label="Партии" value={batches.length}/><Metric label="Операции" value={query.data?.open_operations??0}/><Metric label="Расхождения" value={query.data?.open_drifts??0}/></section>
  <section className="panel inline-create"><div><h2>Сканирование</h2><p>Скан сразу проверяется по GTIN/SKU и заданию WMS. Повторный или лишний код не увеличивает количество.</p></div><form onSubmit={e=>{e.preventDefault();if(!scan.isPending)scan.mutate()}}><input required value={barcode} onChange={e=>setBarcode(e.target.value)} placeholder="Data Matrix / штрихкод" autoComplete="off"/><input required value={gtin} onChange={e=>setGtin(e.target.value)} placeholder="GTIN"/><input required value={sku} onChange={e=>setSku(e.target.value)} placeholder="SKU"/><select value={action} onChange={e=>setAction(e.target.value)}><option value="receiving">Приёмка</option><option value="pick">Комплектация</option><option value="pack">Упаковка</option></select><input required type="number" min="1" value={expected} onChange={e=>setExpected(e.target.value)} aria-label="Ожидаемое количество"/><button className="button primary" disabled={scan.isPending}>{scan.isPending?"Проверяем…":"Проверить скан"}</button></form></section>
  <section className="panel"><div className="drawer-section-heading"><div><h2>Партии кодов</h2><p>Количество и статусы из локального ledger; удалённое состояние обновляется reconciliation.</p></div></div><DataTable rows={batches} columns={columns} rowKey={v=>v.batch.id} searchPlaceholder="Партия, SKU или GTIN…" empty="Партии ещё не заведены"/></section>
 </Page>
}
function Metric({label,value}:{label:string;value:number}){return <article className="metric-card"><span>{label}</span><strong>{value}</strong></article>}
