import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {DataTable} from "../components/DataTable";
import {EmptyState} from "../components/EmptyState";
import {StatusBadge} from "../components/StatusBadge";
import {Page} from "./Page";

interface Settlement{id:string;source_system:string;source_account_id:string;source_entry_ref:string;order_id?:string;adjusts_entry_id?:string;fee_code?:string;fx_rate_ref?:string;kind:string;amount:{minor_units:number;currency:string};occurred_at:string;imported_at:string;disputed:boolean}
interface FXRate{id:string;base_currency:string;quote_currency:string;rate:string;source:string;source_reference?:string;rate_type:string;observed_at:string;effective_at:string}
type R={body:any};type Client={listSettlementEntries(x?:object):Promise<R>;listFXRates(x?:object):Promise<R>};

function decodeSettlements(value:unknown):Settlement[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid settlement response");return root.items as Settlement[]}
function decodeFX(value:unknown):FXRate[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid FX rate response");return root.items as FXRate[]}
const kindLabels:Record<string,string>={sale:"Продажа",fee:"Комиссия",refund:"Возврат",payout:"Выплата",adjustment:"Корректировка"};
const rateTypeLabels:Record<string,string>={official:"Официальный",mid:"Средний",bid:"Bid",ask:"Ask",closing:"Закрытие",indicative:"Индикативный"};
function money(v:{minor_units:number;currency:string}){return new Intl.NumberFormat("ru-RU",{style:"currency",currency:v.currency}).format(v.minor_units/100)}

export function FinancePage(){
 const api=useApi() as unknown as Client;
 const [tab,setTab]=useState<"settlements"|"fx">("settlements");
 return <Page eyebrow="Commerce Core" title="Финансы" description="Неизменяемый журнал расчётов площадок и курсы валют, на которые он ссылается.">
  <div className="catalog-tabs" role="tablist">
   <button role="tab" aria-selected={tab==="settlements"} className={tab==="settlements"?"active":""} onClick={()=>setTab("settlements")}>Расчёты</button>
   <button role="tab" aria-selected={tab==="fx"} className={tab==="fx"?"active":""} onClick={()=>setTab("fx")}>Курсы валют</button>
  </div>
  {tab==="settlements"?<Settlements api={api}/>:<FXRates api={api}/>}
 </Page>
}

function Settlements({api}:{api:Client}){
 const q=useQuery({queryKey:["settlements"],queryFn:async()=>decodeSettlements((await api.listSettlementEntries({limit:200})).body),staleTime:15_000});
 if(q.isPending)return <LoadingBlock/>;
 if(q.isError)return <ErrorBlock>Не удалось загрузить расчёты.</ErrorBlock>;
 if(q.data.length===0)return <EmptyState title="Записей о расчётах пока нет" text="Появляются после импорта отчётов площадок."/>;
 const columns=[
  {key:"occurred",label:"Дата",value:(v:Settlement)=>v.occurred_at,render:(v:Settlement)=><time>{new Date(v.occurred_at).toLocaleString("ru-RU")}</time>},
  {key:"source",label:"Источник",value:(v:Settlement)=>`${v.source_system} ${v.source_account_id}`,render:(v:Settlement)=><span><strong>{v.source_system}</strong><small className="table-subline mono">{v.source_account_id}</small></span>},
  {key:"kind",label:"Тип",value:(v:Settlement)=>kindLabels[v.kind]??v.kind,render:(v:Settlement)=><StatusBadge value={kindLabels[v.kind]??v.kind}/>},
  {key:"amount",label:"Сумма",value:(v:Settlement)=>v.amount.minor_units,render:(v:Settlement)=><strong className={v.amount.minor_units<0?"danger-text":""}>{money(v.amount)}</strong>,align:"end" as const},
  {key:"order",label:"Заказ",value:(v:Settlement)=>v.order_id||"",render:(v:Settlement)=>v.order_id?<span className="mono">{v.order_id}</span>:<span>—</span>},
  {key:"disputed",label:"Оспорено",value:(v:Settlement)=>v.disputed?"да":"",render:(v:Settlement)=>v.disputed?<StatusBadge value="disputed"/>:<span>—</span>},
 ];
 return <DataTable rows={q.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Источник, тип, заказ…"/>;
}

function FXRates({api}:{api:Client}){
 const q=useQuery({queryKey:["fx-rates"],queryFn:async()=>decodeFX((await api.listFXRates({limit:200})).body),staleTime:15_000});
 if(q.isPending)return <LoadingBlock/>;
 if(q.isError)return <ErrorBlock>Не удалось загрузить курсы валют.</ErrorBlock>;
 if(q.data.length===0)return <EmptyState title="Курсов пока нет" text="Появляются по мере поступления сведений от источника курсов."/>;
 const columns=[
  {key:"pair",label:"Пара",value:(v:FXRate)=>`${v.base_currency}/${v.quote_currency}`,render:(v:FXRate)=><strong className="mono">{v.base_currency}/{v.quote_currency}</strong>},
  {key:"rate",label:"Курс",value:(v:FXRate)=>v.rate,align:"end" as const},
  {key:"type",label:"Тип",value:(v:FXRate)=>rateTypeLabels[v.rate_type]??v.rate_type,render:(v:FXRate)=><StatusBadge value={rateTypeLabels[v.rate_type]??v.rate_type}/>},
  {key:"source",label:"Источник",value:(v:FXRate)=>v.source},
  {key:"effective",label:"Действует с",value:(v:FXRate)=>v.effective_at,render:(v:FXRate)=><time>{new Date(v.effective_at).toLocaleString("ru-RU")}</time>},
 ];
 return <DataTable rows={q.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Валютная пара, источник…"/>;
}
