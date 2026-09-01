import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {decodeItems} from "../api/decoders";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {DataTable} from "../components/DataTable";
import {EmptyState} from "../components/EmptyState";
import {StatusBadge} from "../components/StatusBadge";
import {useToast} from "../components/Toast";
import {connectorCatalog} from "../generated/connector-catalog";
import {Page} from "./Page";
import {Drawer} from "../components/Drawer";
import {formatMoneyValue as money} from "../lib/formatters";
import {uuidV7} from "../lib/ids";

interface Settlement{id:string;source_system:string;source_account_id:string;source_entry_ref:string;order_id?:string;adjusts_entry_id?:string;fee_code?:string;fx_rate_ref?:string;kind:string;amount:{minor_units:number;currency:string};occurred_at:string;imported_at:string;disputed:boolean}
interface FXRate{id:string;base_currency:string;quote_currency:string;rate:string;source:string;source_reference?:string;rate_type:string;observed_at:string;effective_at:string}
interface Payment{id:string;connector_account_id:string;external_id:string;remote_id?:string;purpose?:string;amount:{minor_units:number;currency:string};commission_minor_units:number;status:string;reason_code?:string;version:number;created_at:string;updated_at:string;succeeded_at?:string}
interface ConnectorAccount{id:string;connector_id:string;family:string;status:string;health_status:string;capabilities:{capability:string;enabled:boolean}[]}
type R={body:any};
type Client={
 listSettlementEntries(x?:object):Promise<R>;listFXRates(x?:object):Promise<R>;
 listPayments(x?:object):Promise<R>;getPayment(x:{paymentId:string}):Promise<R>;
 createPayment(x:{idempotencyKey:string;body:unknown}):Promise<R>;
 refundPayment(x:{paymentId:string;idempotencyKey:string;body:unknown}):Promise<R>;
 listConnectorAccounts(x?:object):Promise<R>;
};

function decodeSettlements(value:unknown):Settlement[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid settlement response");return root.items as Settlement[]}
function decodeFX(value:unknown):FXRate[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid FX rate response");return root.items as FXRate[]}
function decodePayments(value:unknown):Payment[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid payment response");return root.items as Payment[]}
const kindLabels:Record<string,string>={sale:"Продажа",fee:"Комиссия",refund:"Возврат",payout:"Выплата",adjustment:"Корректировка"};
const rateTypeLabels:Record<string,string>={official:"Официальный",mid:"Средний",bid:"Bid",ask:"Ask",closing:"Закрытие",indicative:"Индикативный"};
const paymentStatusLabels:Record<string,string>={pending:"В обработке",created:"Ожидает оплаты",succeeded:"Оплачен",failed:"Отклонён",canceled:"Отменён",refunded:"Возвращён",partially_refunded:"Частично возвращён"};
export function FinancePage(){
 const api=useApi() as unknown as Client;
 const [tab,setTab]=useState<"settlements"|"fx"|"payments">("settlements");
 return <Page eyebrow="Commerce Core" title="Финансы" description="Неизменяемый журнал расчётов площадок, курсы валют и платежи через СБП/YooKassa.">
  <div className="catalog-tabs" role="tablist">
   <button role="tab" aria-selected={tab==="settlements"} className={tab==="settlements"?"active":""} onClick={()=>setTab("settlements")}>Расчёты</button>
   <button role="tab" aria-selected={tab==="fx"} className={tab==="fx"?"active":""} onClick={()=>setTab("fx")}>Курсы валют</button>
   <button role="tab" aria-selected={tab==="payments"} className={tab==="payments"?"active":""} onClick={()=>setTab("payments")}>Платежи</button>
  </div>
  {tab==="settlements"?<Settlements api={api}/>:tab==="fx"?<FXRates api={api}/>:<Payments api={api}/>}
 </Page>
}

function Settlements({api}:{api:Client}){
 const q=useQuery({queryKey:["settlements"],queryFn:async()=>decodeSettlements((await api.listSettlementEntries({limit:200})).body),staleTime:15_000});
 if(q.isPending)return <LoadingBlock/>;
 if(q.isError)return <ErrorBlock retry={()=>void q.refetch()}>Не удалось загрузить расчёты.</ErrorBlock>;
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
 if(q.isError)return <ErrorBlock retry={()=>void q.refetch()}>Не удалось загрузить курсы валют.</ErrorBlock>;
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

function Payments({api}:{api:Client}){
 const cache=useQueryClient(),toast=useToast();
 const [accountId,setAccountId]=useState(""),[purpose,setPurpose]=useState(""),[amount,setAmount]=useState(""),[currency,setCurrency]=useState("RUB"),[selected,setSelected]=useState<Payment|null>(null);
 const accounts=useQuery({queryKey:["payments","connector-accounts"],queryFn:async()=>decodeItems<ConnectorAccount>((await api.listConnectorAccounts({limit:100})).body,"invalid connector account response"),staleTime:10_000});
 const payments=useQuery({queryKey:["payments"],queryFn:async()=>decodePayments((await api.listPayments({limit:200})).body),refetchInterval:10_000});
 const detail=useQuery({queryKey:["payment",selected?.id],enabled:!!selected,queryFn:async()=>((await api.getPayment({paymentId:selected!.id})).body as Payment)});
 const paymentAccounts=(accounts.data??[]).filter(value=>value.family==="payment"&&value.status==="active"&&value.health_status==="healthy"&&value.capabilities.some(c=>c.capability==="payments.create"&&c.enabled));
 const connectorName=(connectorId:string)=>connectorCatalog.find(v=>v.id===connectorId)?.name??connectorId;

 const create=useMutation({
  mutationFn:async()=>{
   const id=uuidV7(),minorUnits=Math.round(Number(amount)*100);
   return api.createPayment({idempotencyKey:id,body:{id,connector_account_id:accountId,purpose:purpose.trim()||undefined,amount:{minor_units:minorUnits,currency},expires_in_seconds:900}});
  },
  onSuccess:async()=>{toast.push({kind:"success",title:"Платёж создан"});setPurpose("");setAmount("");await cache.invalidateQueries({queryKey:["payments"]})},
  onError:()=>toast.push({kind:"error",title:"Не удалось создать платёж",body:"Проверьте кабинет, сумму и статус подключения шлюза."}),
 });
 const refund=useMutation({
  mutationFn:async(payment:Payment)=>{
   const id=uuidV7();
   return api.refundPayment({paymentId:payment.id,idempotencyKey:id,body:{id,amount:payment.amount}});
  },
  onSuccess:async()=>{toast.push({kind:"success",title:"Возврат создан"});await cache.invalidateQueries({queryKey:["payments"]})},
  onError:()=>toast.push({kind:"error",title:"Не удалось создать возврат"}),
 });

 if(accounts.isPending||payments.isPending)return <LoadingBlock/>;
 if(accounts.isError||payments.isError)return <ErrorBlock retry={()=>{void Promise.all([accounts.refetch(),payments.refetch()])}}>Не удалось загрузить платежи.</ErrorBlock>;

 const validAmount=/^\d+([.,]\d{1,2})?$/.test(amount.trim())&&Number(amount.replace(",","."))>0;
 const columns=[
  {key:"created",label:"Создан",value:(v:Payment)=>v.created_at,render:(v:Payment)=><time>{new Date(v.created_at).toLocaleString("ru-RU")}</time>},
  {key:"connector",label:"Шлюз",value:(v:Payment)=>connectorName((accounts.data??[]).find(a=>a.id===v.connector_account_id)?.connector_id??""),render:(v:Payment)=><span><strong>{connectorName((accounts.data??[]).find(a=>a.id===v.connector_account_id)?.connector_id??"")}</strong><small className="table-subline mono">{v.connector_account_id}</small></span>},
  {key:"purpose",label:"Назначение",value:(v:Payment)=>v.purpose||"",render:(v:Payment)=>v.purpose?<span>{v.purpose}</span>:<span>—</span>},
  {key:"amount",label:"Сумма",value:(v:Payment)=>v.amount.minor_units,render:(v:Payment)=><strong>{money(v.amount)}</strong>,align:"end" as const},
  {key:"status",label:"Статус",value:(v:Payment)=>paymentStatusLabels[v.status]??v.status,render:(v:Payment)=><StatusBadge value={paymentStatusLabels[v.status]??v.status}/>},
  {key:"actions",label:"",value:()=>"",render:(v:Payment)=>v.status==="succeeded"?<button className="button ghost" disabled={refund.isPending} onClick={()=>refund.mutate(v)}>Вернуть</button>:<span>—</span>},
 ];

 return <>
  {paymentAccounts.length===0?<section className="panel social-onboarding"><div><h2>Сначала подключите платёжный шлюз</h2><p>Добавьте кабинет СБП или YooKassa, сохраните учётные данные, включите payments.create и активируйте кабинет.</p></div><button className="button primary" onClick={()=>window.location.assign("/integrations")}>Открыть интеграции</button></section>:<section className="panel social-setup"><div className="section-heading"><div><p className="eyebrow">Новый платёж</p><h2>Создать платёж</h2></div></div><div className="social-form-grid"><label className="field"><span>Кабинет</span><select value={accountId} onChange={e=>setAccountId(e.target.value)}><option value="">Выберите кабинет</option>{paymentAccounts.map(a=><option key={a.id} value={a.id}>{connectorName(a.connector_id)} · {a.id}</option>)}</select></label><label className="field"><span>Сумма</span><input inputMode="decimal" value={amount} onChange={e=>setAmount(e.target.value)} placeholder="1500.00"/></label><label className="field"><span>Валюта</span><input value={currency} onChange={e=>setCurrency(e.target.value.toUpperCase().slice(0,3))} maxLength={3}/></label><label className="field"><span>Назначение</span><input maxLength={210} value={purpose} onChange={e=>setPurpose(e.target.value)} placeholder="Заказ #42"/></label><button className="button primary" disabled={!accountId||!validAmount||create.isPending} onClick={()=>create.mutate()}>Создать платёж</button></div></section>}
  {payments.data.length===0?<EmptyState title="Платежей пока нет" text="Появятся после первого списания через подключённый шлюз."/>:<DataTable rows={payments.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Назначение, статус…" onOpen={setSelected}/>}
  <Drawer open={!!selected} title="Платёж" subtitle={selected?.external_id} onClose={()=>setSelected(null)}>{detail.isPending?<LoadingBlock/>:detail.isError?<ErrorBlock retry={()=>void detail.refetch()}>Не удалось открыть платёж.</ErrorBlock>:detail.data?<PaymentDetail payment={detail.data}/>:null}</Drawer>
 </>;
}

function PaymentDetail({payment}:{payment:Payment}){return <div className="catalog-stack"><div className="drawer-kpis"><div><small>Сумма</small><strong>{money(payment.amount)}</strong></div><div><small>Статус</small><StatusBadge value={paymentStatusLabels[payment.status]??payment.status}/></div><div><small>Комиссия</small><strong>{money({minor_units:payment.commission_minor_units,currency:payment.amount.currency})}</strong></div></div><dl className="detail-list"><div><dt>Внешний ID</dt><dd className="mono">{payment.external_id}</dd></div><div><dt>Назначение</dt><dd>{payment.purpose||"—"}</dd></div><div><dt>Причина</dt><dd>{payment.reason_code||"—"}</dd></div><div><dt>Создан</dt><dd>{new Date(payment.created_at).toLocaleString("ru-RU")}</dd></div><div><dt>Обновлён</dt><dd>{new Date(payment.updated_at).toLocaleString("ru-RU")}</dd></div><div><dt>Версия</dt><dd>{payment.version}</dd></div></dl></div>}
