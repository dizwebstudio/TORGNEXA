import {useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {decodeOrderPage} from "../../api/decoders";
import {EmptyState} from "../../components/EmptyState";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";
import {StatusBadge} from "../../components/StatusBadge";
import {ServerDataGrid} from "../../components/ServerDataGrid";
import {navigate} from "../../shell/useLocationPath";
import {Drawer} from "../../components/Drawer";
import {useToast} from "../../components/Toast";
import {Icon} from "../../components/Icon";
import {ProductImage} from "../../components/ProductImage";
import {useAuth} from "../../auth/AuthProvider";
import {formatQuantityUnit} from "../../components/quantity";
import {LineageTimeline} from "../../components/LineageTimeline";
import {refreshDemoDataset} from "../demoDataset";

function money(minor: number, currency: string): string {return new Intl.NumberFormat("ru-RU", {style: "currency", currency}).format(minor / 100)}
function ProductThumbnail({api,src}:{api:ReturnType<typeof useApi>;src?:string}){return src?<ProductImage api={api} className="order-product-thumbnail" src={src} alt=""/>:<span className="order-product-thumbnail order-product-thumbnail-empty" aria-hidden="true"><Icon name="catalog" size={18}/></span>}
type OrderStatus="pending"|"confirmed"|"processing"|"fulfilled"|"cancelled";
type OrderAction={status:OrderStatus;label:string;pendingLabel:string;danger?:boolean};
function actionsFor(status:string):OrderAction[]{
  if(status==="pending")return [{status:"confirmed",label:"Подтвердить заказ",pendingLabel:"Подтверждаем…"},{status:"cancelled",label:"Отменить заказ",pendingLabel:"Отменяем…",danger:true}];
  if(status==="confirmed")return [{status:"processing",label:"Передать в обработку",pendingLabel:"Передаём…"},{status:"cancelled",label:"Отменить заказ",pendingLabel:"Отменяем…",danger:true}];
  if(status==="processing")return [{status:"fulfilled",label:"Передать в исполнение",pendingLabel:"Передаём…"},{status:"cancelled",label:"Отменить заказ",pendingLabel:"Отменяем…",danger:true}];
  return [];
}

export function OrderList({initialId}:{initialId?:string}) {
  const api=useApi(),toast=useToast(),auth=useAuth();
  const [selected,setSelected]=useState<string|undefined>(initialId),[q,setQ]=useState(""),[status,setStatus]=useState(""),[cursor,setCursor]=useState(""),[history,setHistory]=useState<string[]>([]);
  const queryClient=useQueryClient();
  const canChangeStatus=auth.session?.capabilities.includes("orders.status.write")??false;
  const demo=useMutation({mutationFn:async()=>{await api.createDemoOrders({idempotencyKey:"demo-dataset:create"})},onSuccess:async()=>{toast.push({kind:"success",title:"Демо-контур создан",body:"Каталог, финансы, подключения, синхронизация и согласования заполнены."});await refreshDemoDataset(queryClient)},onError:()=>toast.push({kind:"error",title:"Не удалось создать демо-контур",body:"Проверьте права и повторите операцию."})});
  const query=useQuery({queryKey:["orders","shell",q,status,cursor],queryFn:async()=>decodeOrderPage((await api.listOrders({limit:25,q:q||undefined,status:status||undefined,cursor:cursor||undefined})).body),staleTime:20_000});
  const detail=useQuery({queryKey:["orders","detail",selected],queryFn:async()=>(await api.getOrder({orderId:selected!})).body as any,enabled:!!selected});
  const changeStatus=useMutation({mutationFn:async(input:{orderId:string;status:OrderStatus;version:number})=>api.changeOrderStatus({orderId:input.orderId,idempotencyKey:crypto.randomUUID(),body:{status:input.status,version:input.version}}),onSuccess:async(_result,input)=>{toast.push({kind:"success",title:"Статус заказа изменён"});await Promise.all([queryClient.invalidateQueries({queryKey:["orders"]}),queryClient.invalidateQueries({queryKey:["orders","detail",input.orderId]})])},onError:(error:unknown)=>{const statusCode=typeof error==="object"&&error!==null&&"statusCode" in error?(error as {statusCode?:number}).statusCode:undefined;toast.push({kind:"error",title:"Не удалось изменить статус",body:statusCode===409?"Заказ уже изменился или такой переход недоступен. Обновите карточку заказа.":"Проверьте права и повторите операцию."})}});
  if(query.isPending&&!query.data)return <LoadingBlock/>;
  if(query.isError&&!query.data)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить заказы.</ErrorBlock>;
  const page=query.data??{items:[]};
  if(page.items.length===0&&!q&&!status&&!history.length)return <EmptyState title="Создайте демонстрационные данные" text="Добавьте 26 товаров, заказы и остатки, чтобы посмотреть рабочий поток."><button className="button primary" disabled={demo.isPending} onClick={()=>demo.mutate()}>{demo.isPending?"Создаём…":"Создать демо-набор"}</button></EmptyState>;
  const reset=(kind:"q"|"status",value:string)=>{if(kind==="q")setQ(value);else setStatus(value);setCursor("");setHistory([])};
  const columns=[{key:"product",label:"Товар",render:(v:any)=><span className="order-product-cell"><ProductThumbnail api={api} src={v.product_image_url}/><span className="order-product-copy"><strong>{v.product_title||v.product_sku||"Товар"}</strong><small className="mono">{v.product_sku||"Без SKU"}</small></span></span>},{key:"number",label:"Номер",render:(v:any)=><span className="mono table-primary">{v.order_number}</span>},{key:"status",label:"Статус",render:(v:any)=><StatusBadge value={v.status}/>},{key:"amount",label:"Сумма",render:(v:any)=><strong>{money(v.grand_minor_units,v.currency)}</strong>,align:"end" as const},{key:"created",label:"Создан",render:(v:any)=><time>{new Date(v.placed_at).toLocaleString("ru-RU")}</time>}];
  const order=detail.data as any;
  const actions=order?actionsFor(order.status):[];
  return <><ServerDataGrid rows={page.items as any[]} columns={columns} rowKey={v=>v.id} query={q} onQuery={v=>reset("q",v)} filter={status} filterOptions={[{value:"pending",label:"Ожидают"},{value:"confirmed",label:"Подтверждены"},{value:"processing",label:"В работе"},{value:"fulfilled",label:"Выполнены"},{value:"cancelled",label:"Отменены"}]} onFilter={v=>reset("status",v)} loading={query.isFetching} hasPrevious={history.length>0} hasNext={!!page.next_cursor} onPrevious={()=>{const prev=[...history];setCursor(prev.pop()??"");setHistory(prev)}} onNext={()=>{if(page.next_cursor){setHistory(v=>[...v,cursor]);setCursor(page.next_cursor)}}} onOpen={v=>{setSelected(v.id);navigate(`/orders/${encodeURIComponent(v.id)}`)}}/>
    <Drawer open={!!selected} onClose={()=>{setSelected(undefined);navigate("/orders")}} title={order?.order_number??"Заказ"} subtitle="Карточка заказа и исполнение"><>{detail.isPending?<LoadingBlock/>:detail.isError?<ErrorBlock retry={()=>void detail.refetch()}>Не удалось открыть заказ.</ErrorBlock>:order?<>
    <div className="drawer-kpis"><div><small>Итого</small><strong>{money(order.grand_minor_units,order.currency)}</strong></div><div><small>Статус</small><StatusBadge value={order.status}/></div><div><small>Канал</small><strong>{order.sources[0]?.provider??"TORGNEXA"}</strong></div></div>
    {canChangeStatus&&actions.length>0?<section className="drawer-section order-actions"><h3>Действия</h3><p className="drawer-help">Изменение статуса проверяется по текущей версии заказа и записывается в журнал аудита.</p><div className="button-row">{actions.map(action=><button key={action.status} className={`button ${action.danger?"danger":"primary"}`} disabled={changeStatus.isPending} onClick={()=>changeStatus.mutate({orderId:order.id,status:action.status,version:order.version})}>{changeStatus.isPending?"Сохраняем…":action.label}</button>)}</div>{changeStatus.isError?<ErrorBlock>Не удалось изменить статус. Заказ мог измениться в другой сессии — обновите карточку.</ErrorBlock>:null}</section>:null}
    <section className="drawer-section"><h3>Позиции заказа</h3><div className="line-items">{order.items.map((line:any,index:number)=><div className="line-item" key={`${line.sku}-${index}`}><ProductThumbnail api={api} src={line.product_image_url}/><div className="line-product-copy"><strong>{line.product_title||line.sku}</strong><small>{line.product_title?`${line.sku} · `:""}{line.quantity_coefficient/(10**line.quantity_scale)} {formatQuantityUnit(line.unit,line.quantity_coefficient/(10**line.quantity_scale))}</small></div><div className="line-price"><strong>{money(line.line_total_minor_units,order.currency)}</strong><small>{money(line.unit_price_minor_units,order.currency)} / ед.</small></div></div>)}</div></section>
    <section className="drawer-section"><h3>Контекст</h3><dl className="detail-list"><div><dt>Создан</dt><dd>{new Date(order.placed_at).toLocaleString("ru-RU")}</dd></div><div><dt>Доставка</dt><dd>{money(order.shipping_minor_units,order.currency)}</dd></div><div><dt>Внешний ID</dt><dd className="mono">{order.sources[0]?.remote_id??"—"}</dd></div></dl></section><LineageTimeline system="torgnexa" entityType="order" entityId={order.id}/>
  </>:null}</></Drawer></>;
}
