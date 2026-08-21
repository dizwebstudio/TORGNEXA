import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock,LoadingBlock} from "../../components/ApiState";
import {Dialog} from "../../components/Dialog";
import {EmptyState} from "../../components/EmptyState";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";

interface Subscription{id:string;endpoint:string;event_types:string[];status:string;consecutive_failures:number;version:number;created_at:string;updated_at:string}
interface DeliveryAttempt{delivery_id:string;attempt:number;outcome:string;http_status?:number;duration_ms:number;error_code?:string;completed_at:string}
function decode(value:unknown):Subscription[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid webhook subscription response");return root.items as Subscription[]}
function decodeHistory(value:unknown):DeliveryAttempt[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid webhook delivery history response");return root.items as DeliveryAttempt[]}
function randomSecret():string{const bytes=crypto.getRandomValues(new Uint8Array(32));return Array.from(bytes,b=>b.toString(16).padStart(2,"0")).join("")}
const outcomeLabels:Record<string,string>={succeeded:"Доставлено",retry:"Повтор запланирован",dlq:"В очереди отказов"};

export function WebhookSettings(){
 const api=useApi(),auth=useAuth(),cache=useQueryClient(),toast=useToast();
 const canRead=auth.session?.capabilities.includes("webhooks.read")??false;
 const canWrite=auth.session?.capabilities.includes("webhooks.write")??false;
 const [id,setId]=useState(""),[endpoint,setEndpoint]=useState(""),[eventTypes,setEventTypes]=useState(""),[secret,setSecret]=useState(randomSecret());
 const [rotating,setRotating]=useState<Subscription|null>(null),[rotateSecret,setRotateSecret]=useState(""),[overlap,setOverlap]=useState(3600);
 const [deliveryId,setDeliveryId]=useState(""),[history,setHistory]=useState<DeliveryAttempt[]|null>(null);
 const query=useQuery({queryKey:["settings","webhooks"],queryFn:async()=>decode((await api.listWebhookSubscriptions()).body),enabled:canRead,staleTime:10_000});
 const refresh=()=>cache.invalidateQueries({queryKey:["settings","webhooks"]});
 const reset=()=>{setId("");setEndpoint("");setEventTypes("");setSecret(randomSecret())};
 const create=useMutation({mutationFn:()=>api.createWebhookSubscription({body:{id:id.trim(),endpoint:endpoint.trim(),event_types:eventTypes.split(",").map(v=>v.trim()).filter(Boolean),signing_secret:secret}}),onSuccess:async()=>{toast.push({kind:"success",title:"Подписка создана"});reset();await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось создать подписку",body:"Проверьте ID, HTTPS-адрес и типы событий."})});
 const disable=useMutation({mutationFn:(sub:Subscription)=>api.disableWebhookSubscription({subscriptionId:sub.id}),onSuccess:async()=>{toast.push({kind:"success",title:"Подписка отключена"});await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось отключить подписку"})});
 const rotate=useMutation({mutationFn:()=>api.rotateWebhookSigningSecret({subscriptionId:rotating!.id,body:{signing_secret:rotateSecret,overlap_seconds:overlap}}),onSuccess:async()=>{toast.push({kind:"success",title:"Секрет обновлён"});setRotating(null);await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось обновить секрет"})});
 const lookupHistory=useMutation({mutationFn:async()=>decodeHistory((await api.getWebhookDeliveryHistory({deliveryId:deliveryId.trim(),limit:50})).body),onSuccess:setHistory,onError:()=>toast.push({kind:"error",title:"Доставка не найдена",body:"Проверьте delivery_id."})});
 const replay=useMutation({mutationFn:()=>api.replayWebhookDelivery({deliveryId:deliveryId.trim()}),onSuccess:()=>toast.push({kind:"success",title:"Повторная доставка поставлена в очередь"}),onError:()=>toast.push({kind:"error",title:"Не удалось повторить доставку"})});
 if(!canRead)return null;
 const valid=!!id.trim()&&/^https:\/\//.test(endpoint.trim())&&!!eventTypes.trim()&&secret.length>=32;
 return <section className="panel settings-card">
  <div className="settings-card-heading"><div><p className="eyebrow">Интеграции</p><h2>Webhook-подписки</h2><p className="settings-copy">Внешние получатели событий TORGNEXA. Секрет подписи известен только вам и получателю — TORGNEXA хранит только его ссылку и не показывает значение повторно.</p></div>{query.data?<StatusBadge value={`${query.data.length}`}/>:null}</div>
  {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock>Не удалось загрузить подписки.</ErrorBlock>:query.data.length===0?<EmptyState title="Подписок пока нет" text="Добавьте первую в форме ниже."/>:<div className="settings-grid">{query.data.map(sub=><article className="connector-account" key={sub.id}><header><div><strong className="mono">{sub.id}</strong><small>{sub.endpoint}</small></div><StatusBadge value={sub.status}/></header><div className="chip-list">{sub.event_types.map(t=><span className="chip" key={t}>{t}</span>)}</div>{sub.consecutive_failures>0?<small className="danger-text">Подряд неудач: {sub.consecutive_failures}</small>:null}{sub.status==="active"&&canWrite?<div className="account-actions"><button className="button ghost" onClick={()=>{setRotateSecret(randomSecret());setOverlap(3600);setRotating(sub)}}>Сменить секрет</button><button className="button ghost danger-text" disabled={disable.isPending} onClick={()=>disable.mutate(sub)}>Отключить</button></div>:null}</article>)}</div>}
  {canWrite?<section className="drawer-section">
    <h3>Добавить подписку</h3>
    <div className="settings-grid">
     <label className="field"><span>ID подписки</span><input value={id} maxLength={128} placeholder="orders-to-erp" onChange={e=>setId(e.target.value)}/></label>
     <label className="field"><span>HTTPS-адрес</span><input value={endpoint} maxLength={2048} placeholder="https://erp.example.com/hooks/torgnexa" onChange={e=>setEndpoint(e.target.value)}/></label>
     <label className="field"><span>Типы событий (через запятую)</span><input value={eventTypes} placeholder="commerce.orders.order_changed.v1" onChange={e=>setEventTypes(e.target.value)}/></label>
     <label className="field"><span>Секрет подписи</span><div className="field-with-action"><code className="token-reveal">{secret}</code><button type="button" className="button ghost" onClick={()=>setSecret(randomSecret())}>Сгенерировать заново</button></div></label>
    </div>
    <div className="account-actions"><button className="button primary" disabled={create.isPending||!valid} onClick={()=>create.mutate()}>{create.isPending?"Создаём…":"Создать подписку"}</button></div>
  </section>:null}
  {canRead?<section className="drawer-section">
    <h3>Доставки</h3>
    <p className="settings-copy">История и повторная отправка по known delivery_id (например, из системы-получателя или лога инцидента).</p>
    <div className="settings-grid">
     <label className="field"><span>Delivery ID</span><input value={deliveryId} maxLength={128} onChange={e=>setDeliveryId(e.target.value)}/></label>
    </div>
    <div className="account-actions">
     <button className="button ghost" disabled={!deliveryId.trim()||lookupHistory.isPending} onClick={()=>lookupHistory.mutate()}>{lookupHistory.isPending?"Ищем…":"История попыток"}</button>
     {canWrite?<button className="button ghost" disabled={!deliveryId.trim()||replay.isPending} onClick={()=>replay.mutate()}>{replay.isPending?"Ставим в очередь…":"Повторить доставку"}</button>:null}
    </div>
    {history?<div className="timeline">{history.length===0?<EmptyState title="Попыток не найдено" text="Для этого delivery_id ещё нет истории."/>:history.map(a=><article key={`${a.delivery_id}-${a.attempt}`}><span className="timeline-dot"/><div><strong>{outcomeLabels[a.outcome]??a.outcome}{a.http_status?` · HTTP ${a.http_status}`:""}</strong><small>Попытка {a.attempt} · {a.duration_ms} мс{a.error_code?` · ${a.error_code}`:""}</small><time>{new Date(a.completed_at).toLocaleString("ru-RU")}</time></div></article>)}</div>:null}
  </section>:null}
  <Dialog open={!!rotating} title="Смена секрета подписи" description={rotating?`«${rotating.id}» — сохраните новый секрет и обновите получателя до истечения окна перекрытия.`:undefined} onClose={()=>setRotating(null)}>
   {rotating?<div className="settings-grid">
    <label className="field"><span>Новый секрет</span><div className="field-with-action"><code className="token-reveal">{rotateSecret}</code><button type="button" className="button ghost" onClick={()=>setRotateSecret(randomSecret())}>Сгенерировать заново</button></div></label>
    <label className="field"><span>Окно перекрытия (секунды)</span><input type="number" min={300} max={86400} value={overlap} onChange={e=>setOverlap(Number(e.target.value))}/></label>
    <div className="account-actions"><button className="button primary" disabled={rotate.isPending} onClick={()=>rotate.mutate()}>{rotate.isPending?"Обновляем…":"Активировать новый секрет"}</button><button className="button ghost" onClick={()=>setRotating(null)}>Отмена</button></div>
   </div>:null}
  </Dialog>
 </section>
}
