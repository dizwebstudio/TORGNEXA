import {useMemo,useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {useAuth} from "../auth/AuthProvider";
import {DataTable} from "../components/DataTable";
import {Drawer} from "../components/Drawer";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {Page} from "./Page";
import {StatusBadge} from "../components/StatusBadge";
import {navigate} from "../shell/useLocationPath";

type Conversation = Record<string,any>;
type Thread = {conversation:Conversation;customer?:Record<string,any>;messages?:Record<string,any>[];replies?:Record<string,any>[]};

const stateLabels:Record<string,string>={unread:"Новые",open:"В работе",pending_customer:"Ожидание клиента",pending_internal:"Ожидание внутри",resolved:"Решено",closed:"Закрыто",spam:"Спам"};
const typeLabels:Record<string,string>={message:"Сообщение",review:"Отзыв",question:"Вопрос",claim:"Претензия",return_request:"Возврат",delivery_failure:"Доставка"};

function body(value:any):any{return value?.body??value??{}}
function date(value:any):string{return value?new Date(String(value)).toLocaleString("ru-RU"):"—"}

export function CustomerServicePage(){
 const api=useApi() as any;const auth=useAuth();const cache=useQueryClient();const [selectedID,setSelectedID]=useState<string>();const [filter,setFilter]=useState("all");const [search,setSearch]=useState("");
 const canReply=auth.session?.capabilities.includes("customer_service.reply")??false;const canTriage=auth.session?.capabilities.includes("customer_service.assign")??false;
 const summary=useQuery({queryKey:["customer-service","summary"],queryFn:async()=>body(await api.getCustomerServiceSummary()),staleTime:15000});
 const inbox=useQuery({queryKey:["customer-service","inbox",filter,search],queryFn:async()=>body(await api.listCustomerServiceInbox({type:filter==="all"?undefined:filter,search:search.trim()||undefined,unresolved:true,limit:200})),staleTime:15000});
 const thread=useQuery<Thread>({queryKey:["customer-service","thread",selectedID],enabled:Boolean(selectedID),queryFn:async()=>body(await api.getCustomerServiceThread({conversationId:selectedID!}))});
 const rows=useMemo(()=>Array.isArray(inbox.data?.items)?inbox.data.items:[],[inbox.data]);
 const columns=[{key:"type",label:"Тип",value:(row:Conversation)=>typeLabels[row.type]??row.type},{key:"subject",label:"Обращение",value:(row:Conversation)=>row.subject||row.remote_thread_id},{key:"state",label:"Статус",value:(row:Conversation)=>stateLabels[row.state]??row.state},{key:"priority",label:"Приоритет",value:(row:Conversation)=>row.priority},{key:"sla",label:"SLA",value:(row:Conversation)=>row.sla_state||"new"},{key:"updated",label:"Последнее сообщение",value:(row:Conversation)=>date(row.last_message_at)}];
 return <Page eyebrow="Клиентский сервис" title="Единый inbox" description="Отзывы, вопросы, сообщения и претензии в одной очереди. История клиента минимизирована, внутренние заметки не отправляются наружу.">
  <section className="integration-center-summary"><Metric label="Всего" value={summary.data?.total??0}/><Metric label="Новые" value={summary.data?.unread??0}/><Metric label="В работе" value={summary.data?.open??0}/><Metric label="Нарушен SLA" value={summary.data?.breached??0}/><Metric label="Отзывы" value={summary.data?.reviews??0}/><Metric label="Вопросы" value={summary.data?.questions??0}/><Metric label="Претензии" value={summary.data?.claims??0}/></section>
  <section className="panel">
   <div className="catalog-tabs" role="tablist"><button role="tab" aria-selected={filter==="all"} className={filter==="all"?"active":""} onClick={()=>setFilter("all")}>Все</button><button role="tab" aria-selected={filter==="review"} className={filter==="review"?"active":""} onClick={()=>setFilter("review")}>Отзывы</button><button role="tab" aria-selected={filter==="question"} className={filter==="question"?"active":""} onClick={()=>setFilter("question")}>Вопросы</button><button role="tab" aria-selected={filter==="claim"} className={filter==="claim"?"active":""} onClick={()=>setFilter("claim")}>Претензии</button></div>
   <div className="report-filters"><label>Поиск<input value={search} maxLength={160} placeholder="Тема, заказ или ID" onChange={event=>setSearch(event.target.value)}/></label><div className="settings-note">Quality: {summary.data?.quality??"unknown"} · синхронизация не меняет immutable историю</div></div>
   {inbox.isPending?<LoadingBlock/>:inbox.isError?<ErrorBlock retry={()=>void inbox.refetch()}>Не удалось загрузить inbox.</ErrorBlock>:rows.length===0?<div className="empty-state"><h3>Очередь пока пуста</h3><p>Новые отзывы и сообщения появятся после подключения официальной capability и inbound-синхронизации.</p></div>:<DataTable rows={rows} columns={columns} rowKey={(row)=>String(row.id)} searchPlaceholder="Фильтр в очереди" empty="Обращений не найдено" onOpen={(row)=>setSelectedID(String(row.id))}/>} 
  </section>
  <Drawer open={Boolean(selectedID)} title={thread.data?.conversation?.subject||"Обращение"} subtitle={thread.data?.conversation?`${typeLabels[thread.data.conversation.type]??thread.data.conversation.type} · ${thread.data.conversation.source_system}`:undefined} onClose={()=>{setSelectedID(undefined);navigate("/customer-service")}}>
   {thread.isPending?<LoadingBlock/>:thread.isError?<ErrorBlock retry={()=>void thread.refetch()}>Не удалось загрузить обращение.</ErrorBlock>:thread.data?<ThreadPanel api={api} thread={thread.data} canReply={canReply} canTriage={canTriage} onSaved={()=>{void thread.refetch();void inbox.refetch();void summary.refetch();void cache.invalidateQueries({queryKey:["customer-service"]})}}/>:null}
  </Drawer>
 </Page>
}

function Metric({label,value}:{label:string;value:any}){return <div className="metric-card"><span>{label}</span><strong>{String(value)}</strong></div>}

function ThreadPanel({api,thread,canReply,canTriage,onSaved}:{api:any;thread:Thread;canReply:boolean;canTriage:boolean;onSaved:()=>void}){
 const conversation=thread.conversation;const [text,setText]=useState("");const [visibility,setVisibility]=useState("public");const [assignee,setAssignee]=useState(conversation.assignee_id??"");const [team,setTeam]=useState(conversation.team_id??"");
 const history:Array<Record<string,any>>=[...(thread.messages??[]).map(item=>({...item,_kind:"message",_at:item.occurred_at})),...(thread.replies??[]).map(item=>({...item,_kind:"reply",_at:item.created_at}))];
 const reply=useMutation({mutationFn:()=>api.queueCustomerServiceReply({idempotencyKey:crypto.randomUUID(),body:{conversation_id:conversation.id,visibility,origin:"human",safe_text:text,approval_ref:visibility==="public"?"operator-approved":""}}),onSuccess:()=>{setText("");onSaved()}});
 const assign=useMutation({mutationFn:()=>api.assignCustomerServiceConversation({idempotencyKey:crypto.randomUUID(),body:{conversation_id:conversation.id,assignee_id:assignee||undefined,team_id:team||undefined,expected_version:Number(conversation.version),created_at:new Date().toISOString()}}),onSuccess:onSaved});
 const transition=useMutation({mutationFn:(state:string)=>api.transitionCustomerServiceConversation({idempotencyKey:crypto.randomUUID(),body:{conversation_id:conversation.id,state,expected_version:Number(conversation.version)}}),onSuccess:onSaved});
 return <div className="catalog-stack"><div className="settings-facts"><div><dt>Статус</dt><dd><StatusBadge value={conversation.state}/></dd></div><div><dt>SLA</dt><dd><StatusBadge value={conversation.sla_state||"new"}/></dd></div><div><dt>Клиент</dt><dd>{thread.customer?.display_name_mask||conversation.identity_state}</dd></div><div><dt>Заказ</dt><dd>{conversation.order_id||"—"}</dd></div><div><dt>Товар</dt><dd>{conversation.product_id||"—"}</dd></div><div><dt>Последнее сообщение</dt><dd>{date(conversation.last_message_at)}</dd></div></div>
  <div className="drawer-section"><div className="settings-card-heading"><div><h3>История треда</h3><p className="settings-copy">Входящий текст санитизирован; сообщения и ответы immutable. Delivery `unknown` требует сверки, а не повторной отправки.</p></div></div>{history.sort((a,b)=>String(a._at).localeCompare(String(b._at))).map(item=><article className="activity-item" key={`${item._kind}-${item.id}`}><StatusBadge value={String(item.delivery_state||item._kind)}/><span><strong>{item.visibility==="internal"?"Внутренняя заметка":item.direction==="inbound"?"Клиент":"Ответ оператора"}</strong><small>{String(item.safe_text)}</small><small>{date(item._at)}</small></span></article>)}</div>
  {canTriage?<section className="drawer-section"><h3>Очередь и назначение</h3><div className="report-filters"><label>Оператор<input maxLength={192} value={assignee} onChange={event=>setAssignee(event.target.value)} placeholder="operator-id"/></label><label>Команда<input maxLength={192} value={team} onChange={event=>setTeam(event.target.value)} placeholder="team-id"/></label></div><div className="account-actions"><button className="button ghost" disabled={assign.isPending||(!assignee&&!team)} onClick={()=>assign.mutate()}>Назначить</button><button className="button ghost" disabled={transition.isPending||conversation.state==="resolved"} onClick={()=>transition.mutate("resolved")}>Решить</button><button className="button ghost" disabled={transition.isPending||conversation.state==="open"} onClick={()=>transition.mutate("open")}>Вернуть в работу</button></div>{assign.isError||transition.isError?<p className="settings-note">Изменение не применено: обновите карточку — возможно, версия уже изменилась.</p>:null}</section>:null}
  {canReply?<section className="drawer-section"><h3>Ответ</h3><div className="report-filters"><label>Видимость<select value={visibility} onChange={event=>setVisibility(event.target.value)}><option value="public">Публичный ответ</option><option value="internal">Внутренняя заметка</option></select></label></div><textarea className="field-textarea" maxLength={16000} value={text} placeholder="Напишите ответ или заметку…" onChange={event=>setText(event.target.value)}/><div className="account-actions"><button className="button primary" disabled={reply.isPending||!text.trim()} onClick={()=>reply.mutate()}>{reply.isPending?"Сохраняем…":visibility==="public"?"Поставить ответ в очередь":"Сохранить заметку"}</button></div>{reply.isError?<p className="settings-note">Ответ не сохранён. Проверьте права, модерацию и состояние канала.</p>:<p className="settings-note">Публичная отправка доступна только после connector qualification; сейчас создаётся контролируемый intent.</p>}</section>:<p className="settings-note">У пользователя нет права отвечать в этом обращении.</p>}
 </div>
}
