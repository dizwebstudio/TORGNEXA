import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {EmptyState} from "../components/EmptyState";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {Page} from "./Page";

interface Column {key:string;label:string}
interface Report {id:string;generated_at:string;source:string;columns:Column[];rows:string[][]}
type Tab="pnl"|"cash"|"unit"|"quality";

function read(value:unknown):Report {const report=value as Report;if(!report||!Array.isArray(report.columns)||!Array.isArray(report.rows))throw new Error("invalid financial report");return report}
function money(value:string,currency:string){if(value==="—"||value==="")return "—";return new Intl.NumberFormat("ru-RU",{style:"currency",currency:currency||"RUB",maximumFractionDigits:2}).format(Number(value)/100)}
function formatCell(value:string,column:Column,row:string[]){if(value===""||value==="—")return "—";if(column.key.endsWith("_minor_units"))return money(value,row[1]||"RUB");if(column.key.endsWith("_basis_points"))return `${(Number(value)/100).toLocaleString("ru-RU",{maximumFractionDigits:2})}%`;if(column.key==="coverage_percent")return `${value}%`;return value}
function makeRange(days:number){const end=new Date();const start=new Date(end);start.setUTCDate(end.getUTCDate()-days+1);return {from:start.toISOString(),to:end.toISOString()}}

export function FinancialAnalyticsPage(){
 const api=useApi();const [tab,setTab]=useState<Tab>("pnl");const [from,setFrom]=useState("");const [to,setTo]=useState("");const [currency,setCurrency]=useState("");
 const range=from&&to?{from:new Date(`${from}T00:00:00Z`).toISOString(),to:new Date(`${to}T23:59:59Z`).toISOString()}:{};
 const query=useQuery({queryKey:["seller-financial",tab,from,to,currency],queryFn:async()=>{const input={...range,currency:currency||undefined,limit:200};const response=tab==="pnl"?await api.getSellerProfitAndLoss(input):tab==="cash"?await api.getSellerCashFlow(input):tab==="unit"?await api.getSellerUnitEconomics(input):await api.getSellerFinancialQuality(input);return read(response.body)},staleTime:30_000});
 const preset=(days:number)=>{const value=makeRange(days);setFrom(value.from.slice(0,10));setTo(value.to.slice(0,10))};
 return <Page eyebrow="Управленческий контур" title="Финансовая аналитика" description="P&L, денежный поток и юнит-экономика продавца на основе сохранённого расчётного снимка.">
  <div className="catalog-tabs" role="tablist"><button role="tab" aria-selected={tab==="pnl"} className={tab==="pnl"?"active":""} onClick={()=>setTab("pnl")}>P&L</button><button role="tab" aria-selected={tab==="cash"} className={tab==="cash"?"active":""} onClick={()=>setTab("cash")}>Денежный поток</button><button role="tab" aria-selected={tab==="unit"} className={tab==="unit"?"active":""} onClick={()=>setTab("unit")}>Юнит-экономика</button><button role="tab" aria-selected={tab==="quality"} className={tab==="quality"?"active":""} onClick={()=>setTab("quality")}>Качество данных</button></div>
  <section className="panel report-result"><div className="analytics-presets"><button className="button ghost" onClick={()=>preset(7)}>7 дней</button><button className="button ghost" onClick={()=>preset(30)}>30 дней</button><button className="button ghost" onClick={()=>preset(90)}>90 дней</button></div><div className="report-filters"><label>С даты<input type="date" value={from} onChange={event=>setFrom(event.target.value)}/></label><label>По дату<input type="date" value={to} onChange={event=>setTo(event.target.value)}/></label><label>Валюта<select value={currency} onChange={event=>setCurrency(event.target.value)}><option value="">Все валюты</option><option>RUB</option><option>USD</option><option>EUR</option></select></label></div>
   {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить финансовый снимок. Сначала дождитесь ежедневного расчёта.</ErrorBlock>:!query.data?.rows.length?<EmptyState title="Расчётных данных пока нет" text="Worker формирует снимок за предыдущий UTC-день. Для ручного запуска используйте API финансовых расчётов."/>:<><p className="settings-note">Снимок от {new Date(query.data.generated_at).toLocaleString("ru-RU")} · {query.data.source} · отсутствующие факты отмечены статусом и не заменены нулём.</p><div className="table-wrap"><table><thead><tr>{query.data.columns.map(column=><th key={column.key}>{column.label}</th>)}</tr></thead><tbody>{query.data.rows.map((row,index)=><tr key={index}>{query.data!.columns.map((column,columnIndex)=><td key={column.key}>{formatCell(row[columnIndex]??"",column,row)}</td>)}</tr>)}</tbody></table></div></>}
  </section>
 </Page>
}
