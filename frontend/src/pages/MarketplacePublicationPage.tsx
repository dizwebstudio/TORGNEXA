import {useMemo,useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {useAuth} from "../auth/AuthProvider";
import {Page} from "./Page";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {StatusBadge} from "../components/StatusBadge";
import {useToast} from "../components/Toast";

type Operation={
  id:string; state:string; kind:string; target?:{connector_id?:string;connector_account_id?:string;sku?:string};
  snapshot_id:string; snapshot_digest:string; remote_id?:string; remote_operation_id?:string;
  error_code?:string; attempt:number; dry_run:boolean; created_at:string; updated_at:string;
};
type Drift={type:string;snapshot_id:string;remote_id?:string;expected_digest?:string;observed_digest?:string;observed_state?:string;detected_at:string};
type Client={
  listMarketplacePublications():Promise<{body:unknown}>;
  getMarketplacePublication(input:{operationId:string}):Promise<{body:unknown}>;
  listMarketplacePublicationDrifts(input:{operationId:string}):Promise<{body:unknown}>;
  preflightMarketplacePublication(input:{body:unknown}):Promise<{body:unknown}>;
  enqueueMarketplacePublication(input:{idempotencyKey:string;approvalRequestID?:string;body:unknown}):Promise<{body:unknown}>;
  retryMarketplacePublication(input:{operationId:string;idempotencyKey:string}):Promise<{body:unknown}>;
};

const states:Readonly<Record<string,string>>={
  queued:"В очереди",sending:"Отправляется",accepted:"Принято площадкой",processing:"Обрабатывается",
  published:"Опубликовано",rejected:"Отклонено",unknown:"Нужно проверить",needs_attention:"Требует внимания",
  cancelled:"Отменено",draft:"Черновик",preflight:"Предпроверка",
};
const kinds:Readonly<Record<string,string>>={create_product:"Создание карточки",update_product:"Обновление карточки",update_variant:"Обновление варианта",update_attributes:"Характеристики",update_media:"Медиа",archive:"Архивация",unarchive:"Возврат из архива",publish:"Публикация",unpublish:"Снятие с публикации",status_read:"Проверка статуса"};

function decodeOperations(body:unknown):Operation[]{
  const items=(body as {items?:unknown[]}|null)?.items;
  return Array.isArray(items)?items.filter((item):item is Operation=>Boolean(item&&typeof item==="object"&&typeof (item as Operation).id==="string")):[];
}
function decodeDrifts(body:unknown):Drift[]{
  const items=(body as {items?:unknown[]}|null)?.items;
  return Array.isArray(items)?items.filter((item):item is Drift=>Boolean(item&&typeof item==="object"&&typeof (item as Drift).type==="string")):[];
}
function operationState(value:string){return states[value]??value}
function date(value:string){return new Date(value).toLocaleString("ru-RU")}

export function MarketplacePublicationPage(){
  const api=useApi() as unknown as Client;
  const auth=useAuth();
  const cache=useQueryClient();
  const toast=useToast();
  const canWrite=auth.session?.capabilities.includes("products.write")??false;
  const [filter,setFilter]=useState("all"),[selected,setSelected]=useState<string>(),[showStart,setShowStart]=useState(false);
  const [operation,setOperation]=useState("create_product"),[qualityID,setQualityID]=useState(""),[approvalID,setApprovalID]=useState(""),[snapshotText,setSnapshotText]=useState(""),[dryRun,setDryRun]=useState(true);
  const list=useQuery({queryKey:["marketplace-publications"],queryFn:async()=>decodeOperations((await api.listMarketplacePublications()).body),refetchInterval:30_000});
  const detail=useQuery({queryKey:["marketplace-publication",selected],enabled:Boolean(selected),queryFn:async()=>((await api.getMarketplacePublication({operationId:selected!})).body as Operation)});
  const drifts=useQuery({queryKey:["marketplace-publication","drifts",selected],enabled:Boolean(selected),queryFn:async()=>decodeDrifts((await api.listMarketplacePublicationDrifts({operationId:selected!})).body)});
  const refresh=()=>void cache.invalidateQueries({queryKey:["marketplace-publications"]});
  const retry=useMutation({mutationFn:(item:Operation)=>api.retryMarketplacePublication({operationId:item.id,idempotencyKey:crypto.randomUUID()}),onSuccess:async()=>{toast.push({kind:"success",title:"Операция возвращена в очередь"});await refresh()},onError:()=>toast.push({kind:"error",title:"Повторить не удалось",body:"Сначала проверьте внешний кабинет и текущий статус карточки."})});
  const preflight=useMutation({mutationFn:()=>{const body=JSON.parse(snapshotText) as unknown;return api.preflightMarketplacePublication({body:{snapshot:body,quality_receipt_id:qualityID.trim()}})},onSuccess:result=>toast.push({kind:"success",title:"Предпроверка пройдена",body:`Решение: ${String((result.body as {decision?:string}).decision??"ready")}`}),onError:()=>toast.push({kind:"error",title:"Предпроверка не пройдена",body:"Проверьте JSON snapshot, quality receipt и обязательные поля."})});
  const enqueue=useMutation({mutationFn:()=>{const body=JSON.parse(snapshotText) as unknown;return api.enqueueMarketplacePublication({idempotencyKey:crypto.randomUUID(),approvalRequestID:approvalID.trim()||undefined,body:{operation,snapshot:body,quality_receipt_id:qualityID.trim(),dry_run:dryRun}})},onSuccess:async()=>{toast.push({kind:"success",title:dryRun?"Dry-run поставлен в очередь":"Публикация поставлена в очередь"});setShowStart(false);setSnapshotText("");await refresh()},onError:()=>toast.push({kind:"error",title:"Операция не создана",body:"Проверьте snapshot, quality receipt, права и approval для live-записи."})});
  const items=useMemo(()=>{const all=list.data??[];return filter==="all"?all:all.filter(item=>item.state===filter)},[filter,list.data]);
  const counts=useMemo(()=>{const all=list.data??[];return {all:all.length,active:all.filter(item=>["queued","sending","accepted","processing"].includes(item.state)).length,attention:all.filter(item=>["unknown","needs_attention","rejected"].includes(item.state)).length,published:all.filter(item=>item.state==="published").length}},[list.data]);
  if(list.isPending)return <Page eyebrow="Каналы" title="Публикация товаров" description="Очередь и статусы карточек marketplace."><LoadingBlock/></Page>;
  if(list.isError)return <Page eyebrow="Каналы" title="Публикация товаров" description="Очередь и статусы карточек marketplace."><ErrorBlock retry={()=>void list.refetch()}>Не удалось загрузить операции публикации.</ErrorBlock></Page>;
  return <Page eyebrow="Каналы и каталог" title="Публикация товаров" description="Одна очередь для WB, Ozon и Yandex Market: snapshot проходит проверку до внешней записи, а неопределённый результат остаётся на контроле оператора." actions={canWrite?<button type="button" className="button primary" onClick={()=>setShowStart(value=>!value)}>{showStart?"Закрыть форму":"Новая операция"}</button>:null}>
    <section className="integration-center-summary" aria-label="Сводка публикаций"><Metric label="Всего операций" value={counts.all}/><Metric label="В работе" value={counts.active}/><Metric label="Опубликовано" value={counts.published}/><Metric label="Нужна проверка" value={counts.attention}/></section>
    {showStart&&canWrite?<section className="panel inline-create"><div><p className="eyebrow">Проверенный snapshot</p><h2>Запустить публикацию</h2><p>Вставьте JSON snapshot из PIM/catalog pipeline. Токены и внешние URL сюда не добавляются: медиа передаются только как ReleasedObjectRef.</p></div><form onSubmit={event=>{event.preventDefault();if(!preflight.isPending&&!enqueue.isPending)enqueue.mutate()}}><div className="form-grid"><label className="field"><span>Операция</span><select value={operation} onChange={event=>setOperation(event.target.value)}>{Object.entries(kinds).filter(([key])=>key!=="status_read").map(([key,label])=><option value={key} key={key}>{label}</option>)}</select></label><label className="field"><span>ID quality receipt</span><input required value={qualityID} onChange={event=>setQualityID(event.target.value)} placeholder="receipt_…"/></label><label className="field"><span>ID approval (для live)</span><input value={approvalID} onChange={event=>setApprovalID(event.target.value)} placeholder="approval_…" disabled={dryRun}/></label></div><label className="field"><span>Snapshot JSON</span><textarea required rows={8} value={snapshotText} onChange={event=>setSnapshotText(event.target.value)} placeholder={'{"id":"…","target":{…},"version":1,…}'}/></label><label className="checkbox-field"><input type="checkbox" checked={dryRun} onChange={event=>setDryRun(event.target.checked)}/><span>Только dry-run — не вызывать marketplace API</span></label><div className="page-actions"><button type="button" className="button ghost" disabled={preflight.isPending||!snapshotText.trim()||!qualityID.trim()} onClick={()=>preflight.mutate()}>{preflight.isPending?"Проверяем…":"Проверить snapshot"}</button><button type="submit" className="button primary" disabled={enqueue.isPending||!snapshotText.trim()||!qualityID.trim()||(!dryRun&&!approvalID.trim())}>{enqueue.isPending?"Ставим в очередь…":dryRun?"Поставить dry-run":"Запустить публикацию"}</button></div></form></section>:null}
    <section className="panel"><div className="drawer-section-heading"><div><p className="eyebrow">История</p><h2>Операции публикации</h2></div><label className="field compact-field"><span className="sr-only">Фильтр</span><select value={filter} onChange={event=>setFilter(event.target.value)}><option value="all">Все состояния</option>{Object.entries(states).map(([key,label])=><option value={key} key={key}>{label}</option>)}</select></label></div>{items.length===0?<div className="empty-state"><strong>Операций пока нет</strong><span>После preflight здесь появятся попытки, статусы обработки и ошибки без raw provider payload.</span></div>:<div className="table-wrap"><table><thead><tr><th>Товар</th><th>Канал</th><th>Операция</th><th>Состояние</th><th>Попытка</th><th>Обновлено</th><th/></tr></thead><tbody>{items.map(item=><tr key={item.id}><td><strong>{item.target?.sku||item.snapshot_id}</strong><small className="table-subline mono">{item.snapshot_id}</small></td><td>{item.target?.connector_id||"—"}</td><td>{kinds[item.kind]??item.kind}{item.dry_run?<small className="table-subline">dry-run</small>:null}</td><td><StatusBadge value={operationState(item.state)}/>{item.error_code?<small className="table-subline danger-text">{item.error_code}</small>:null}</td><td>{item.attempt}</td><td>{date(item.updated_at)}</td><td><button type="button" className="button ghost" onClick={()=>setSelected(item.id)}>Открыть</button></td></tr>)}</tbody></table></div>}</section>
    {selected&&detail.data?<section className="panel report-result"><div className="settings-card-heading"><div><p className="eyebrow">Детали операции</p><h2>{kinds[detail.data.kind]??detail.data.kind}</h2></div><button type="button" className="button ghost" onClick={()=>setSelected(undefined)}>Закрыть</button></div><div className="settings-facts"><div><dt>Состояние</dt><dd><StatusBadge value={operationState(detail.data.state)}/></dd></div><div><dt>Канал</dt><dd>{detail.data.target?.connector_id||"—"}</dd></div><div><dt>Snapshot digest</dt><dd className="mono">{detail.data.snapshot_digest}</dd></div><div><dt>Внешний ID</dt><dd className="mono">{detail.data.remote_id||"Не получен"}</dd></div><div><dt>Операция провайдера</dt><dd className="mono">{detail.data.remote_operation_id||"—"}</dd></div><div><dt>Попытки</dt><dd>{detail.data.attempt}</dd></div><div><dt>Создана</dt><dd>{date(detail.data.created_at)}</dd></div><div><dt>Обновлена</dt><dd>{date(detail.data.updated_at)}</dd></div></div>{detail.data.error_code?<p className="danger-text">Код ошибки: {detail.data.error_code}</p>:null}{canWrite&&["unknown","needs_attention"].includes(detail.data.state)?<button type="button" className="button primary" disabled={retry.isPending} onClick={()=>retry.mutate(detail.data!)}>{retry.isPending?"Возвращаем…":"Повторить после проверки"}</button>:null}<div className="drawer-section"><div className="drawer-section-heading"><div><h3>Reconciliation</h3><p>Только нормализованные расхождения; ответ marketplace не сохраняется.</p></div></div>{drifts.isPending?<LoadingBlock/>:drifts.data?.length?<div className="table-wrap"><table><thead><tr><th>Тип</th><th>Состояние</th><th>Remote ID</th><th>Обнаружено</th></tr></thead><tbody>{drifts.data.map((drift,index)=><tr key={`${drift.type}-${drift.detected_at}-${index}`}><td><strong>{drift.type}</strong></td><td>{drift.observed_state?<StatusBadge value={operationState(drift.observed_state)}/> :"—"}</td><td className="mono">{drift.remote_id||"—"}</td><td>{date(drift.detected_at)}</td></tr>)}</tbody></table></div>:<p className="drawer-help">Расхождений не обнаружено.</p>}</div></section>:null}
  </Page>;
}
function Metric({label,value}:{label:string;value:number}){return <article className="metric-card"><span>{label}</span><strong>{value}</strong></article>}
