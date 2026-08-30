import {useMemo,useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {EmptyState} from "../components/EmptyState";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {Page} from "./Page";
import {Icon} from "../components/Icon";
import {AnalyticsChart} from "../components/AnalyticsChart";
import {AskAIPanel} from "../features/reports/AskAI";

interface Definition{id:string;title:string;description:string;source:string;freshness_sla_seconds:number}
interface Data{id:string;generated_at:string;source:string;columns:Array<{key:string;label:string}>;rows:string[][]}
const catalog=(value:unknown)=>{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw Error("invalid reports");return root.items as Definition[]};
const data=(value:unknown)=>{const result=value as Data;if(!result||!Array.isArray(result.columns)||!Array.isArray(result.rows))throw Error("invalid report");return result};
function cell(value:string,key:string,row:string[]){if(value===""||value==="—")return "—";if(key.endsWith("_minor_units")||key==="gross_minor_units")return new Intl.NumberFormat("ru-RU",{style:"currency",currency:row[1]||"RUB",maximumFractionDigits:2}).format(Number(value)/100);if(key==="margin_basis_points")return `${(Number(value)/100).toLocaleString("ru-RU",{maximumFractionDigits:2})}%`;if(key==="coverage_percent")return `${value}%`;if(key.endsWith("_at"))return new Date(value).toLocaleString("ru-RU");return value}
function Chart({report}:{report:Data}){const metricKeys=["gross_minor_units","orders","fulfilled","cancelled","quantity","events"],indexes=metricKeys.map(key=>report.columns.findIndex(column=>column.key===key)).filter(value=>value>=0);if(!indexes.length||!report.rows.length)return null;const labels=report.rows.map(row=>row[0]??"");const series=indexes.slice(0,3).map(index=>({key:report.columns[index].key,label:report.columns[index].label,values:report.rows.map(row=>Math.max(0,Number(row[index])||0))}));const moneySeries=report.columns[indexes[0]]?.key==="gross_minor_units";return <AnalyticsChart labels={labels} series={series} format={value=>moneySeries?new Intl.NumberFormat("ru-RU",{notation:"compact",maximumFractionDigits:1}).format(value/100):new Intl.NumberFormat("ru-RU",{notation:"compact",maximumFractionDigits:1}).format(value)}/>}

function saveDownload(body:BlobPart,type:string,filename:string){
 const href=URL.createObjectURL(new Blob([body],{type}));const link=document.createElement("a");link.href=href;link.download=filename;document.body.appendChild(link);link.click();link.remove();window.setTimeout(()=>URL.revokeObjectURL(href),0);
}

export function ReportsPage(){
 const api=useApi(),[selected,setSelected]=useState<string>(),[from,setFrom]=useState(""),[to,setTo]=useState(""),[search,setSearch]=useState(""),[currency,setCurrency]=useState(""),[basis,setBasis]=useState("order_accrual"),[channelRef,setChannelRef]=useState(""),[exporting,setExporting]=useState<"csv"|"pdf">(),[lastExportFormat,setLastExportFormat]=useState<"csv"|"pdf">("csv"),[exportError,setExportError]=useState("");
 const definitions=useQuery({queryKey:["reports"],queryFn:async()=>catalog((await api.listReports()).body),staleTime:60_000});
 const input={reportId:selected!,from:from?new Date(`${from}T00:00:00Z`).toISOString():undefined,to:to?new Date(`${to}T23:59:59Z`).toISOString():undefined,q:search||undefined,currency:currency||undefined,basis:selected==="unit_economics_by_channel"?basis:undefined,channelRef:selected==="unit_economics_by_channel"?(channelRef||undefined):undefined,limit:200};
 const report=useQuery({queryKey:["reports",selected,from,to,search,currency,basis,channelRef],queryFn:async()=>data((await api.getReportData(input)).body),enabled:!!selected});
 const definition=definitions.data?.find(item=>item.id===selected);
 const preset=(days:number)=>{const end=new Date(),start=new Date(end);start.setUTCDate(end.getUTCDate()-days+1);setFrom(start.toISOString().slice(0,10));setTo(end.toISOString().slice(0,10))};
 const salesSummary=useMemo(()=>{if(selected!=="sales_daily"||!report.data)return null;const cols=Object.fromEntries(report.data.columns.map((c,i)=>[c.key,i]));let orders=0,fulfilled=0,cancelled=0,gross=0;for(const row of report.data.rows){orders+=Number(row[cols.orders]??0);fulfilled+=Number(row[cols.fulfilled]??0);cancelled+=Number(row[cols.cancelled]??0);gross+=Number(row[cols.gross_minor_units]??0)}return {orders,fulfilled,cancelled,gross}},[selected,report.data]);
 const exportFile=async(format:"csv"|"pdf")=>{if(!selected)return;setLastExportFormat(format);setExporting(format);setExportError("");try{const response=await api.getReportData({...input,format});if(format==="pdf"){if(!(response.body instanceof ArrayBuffer))throw Error("invalid PDF response");saveDownload(response.body,"application/pdf",`${selected}.pdf`)}else{saveDownload(String(response.body??""),"text/csv;charset=utf-8",`${selected}.csv`)}}catch{setExportError("Не удалось скачать файл. Повторите экспорт.")}finally{setExporting(undefined)}};
 return <Page eyebrow="Аналитика" title="Отчёты" description="Фильтры, графики и экспорт показателей текущего рабочего пространства.">
    {definitions.isPending?<LoadingBlock/>:definitions.isError?<ErrorBlock retry={()=>void definitions.refetch()}>Не удалось загрузить отчёты.</ErrorBlock>:<>
   <div className="report-grid">{definitions.data.map(item=><button type="button" className="panel report-card report-card-button" key={item.id} onClick={()=>setSelected(item.id)}><div className="report-card-heading"><span className="module-glyph"><Icon name="reports"/></span><div><h2>{item.title}</h2><span className="status status-active">Открыть</span></div></div><p>{item.description}</p><dl><div><dt>Источник</dt><dd>{item.source==="clickhouse"?"ClickHouse":"PostgreSQL"}</dd></div><div><dt>SLA свежести</dt><dd>до {item.freshness_sla_seconds} сек.</dd></div></dl></button>)}</div>
   {selected?<section className="panel report-result">
    <div className="settings-card-heading"><div><p className="eyebrow">Отчёт</p><h2>{definition?.title??selected}</h2></div><button className="button ghost" onClick={()=>setSelected(undefined)}>Закрыть</button></div>
    <div className="analytics-presets"><button className="button ghost" onClick={()=>preset(7)}>7 дней</button><button className="button ghost" onClick={()=>preset(30)}>30 дней</button><button className="button ghost" onClick={()=>preset(90)}>90 дней</button></div>
    <div className="report-filters">
     <label>С даты<input type="date" value={from} onChange={event=>setFrom(event.target.value)}/></label>
     <label>По дату<input type="date" value={to} onChange={event=>setTo(event.target.value)}/></label>
     <label>Поиск<input value={search} maxLength={100} placeholder="SKU, склад или событие" onChange={event=>setSearch(event.target.value)}/></label>
     {selected==="sales_daily"?<label className="report-filter-currency">Валюта<select value={currency} onChange={event=>setCurrency(event.target.value)}><option value="">Все</option><option>RUB</option><option>USD</option><option>EUR</option></select></label>:null}
     {selected==="unit_economics_by_channel"?<><label>База<select value={basis} onChange={event=>setBasis(event.target.value)}><option value="order_accrual">Заказ (accrual)</option><option value="settlement">Взаиморасчёт</option><option value="cash">Выплата (cash)</option></select></label><label>Канал<input value={channelRef} maxLength={192} placeholder="channel_ref" onChange={event=>setChannelRef(event.target.value)}/></label></>:null}
     <div className="report-actions">
      <button className="button secondary" disabled={!!exporting} onClick={()=>void exportFile("csv")}>{exporting==="csv"?"Скачиваем…":"Экспорт CSV"}</button>
      <button className="button secondary" disabled={!!exporting||!report.data?.rows.length} onClick={()=>void exportFile("pdf")}>{exporting==="pdf"?"Скачиваем…":"Экспорт PDF"}</button>
     </div>
    </div>
    {exportError?<ErrorBlock retry={()=>void exportFile(lastExportFormat)}>{exportError}</ErrorBlock>:null}
    {report.isPending?<LoadingBlock/>:report.isError?<ErrorBlock retry={()=>void report.refetch()}>Не удалось сформировать отчёт.</ErrorBlock>:report.data.rows.length===0?<EmptyState title="Данных по фильтрам нет" text="Измените период или фильтры отчёта."/>:<>
     <p className="settings-note">Сформирован {new Date(report.data.generated_at).toLocaleString("ru-RU")} · источник {report.data.source==="clickhouse"?"ClickHouse":"PostgreSQL"}{selected==="unit_economics_by_channel"?" · отсутствие факта отмечено статусом, нулём не подменяется":""}</p>
     {salesSummary?<div className="analytics-kpis"><article><small>Заказы</small><strong>{salesSummary.orders}</strong></article><article><small>Выполнено</small><strong>{salesSummary.fulfilled}</strong></article><article><small>Отменено</small><strong>{salesSummary.cancelled}</strong></article><article><small>Оборот</small><strong>{new Intl.NumberFormat("ru-RU",{style:"currency",currency:currency||report.data.rows[0]?.[1]||"RUB",maximumFractionDigits:0}).format(salesSummary.gross/100)}</strong></article></div>:null}
     <Chart report={report.data}/>
     <div className="table-wrap"><table><thead><tr>{report.data.columns.map(column=><th key={column.key}>{column.label}</th>)}</tr></thead><tbody>{report.data.rows.map((row,i)=><tr key={i}>{report.data!.columns.map((column,j)=><td key={column.key}>{cell(row[j]??"",column.key,row)}</td>)}</tr>)}</tbody></table></div>
     <AskAIPanel reportTitle={definition?.title??selected} report={report.data}/>
    </>}
   </section>:null}
  </>}
 </Page>
}
