import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {DataTable} from "../components/DataTable";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {EmptyState} from "../components/EmptyState";
import {StatusBadge} from "../components/StatusBadge";
import {useToast} from "../components/Toast";
import {Page} from "./Page";

type Response={body:any};
type Client={
 listProcurementSuppliers():Promise<Response>;
 createProcurementSupplier(input:{idempotencyKey:string;body:unknown}):Promise<Response>;
 listProcurementOffers(input:{supplierId?:string}):Promise<Response>;
 previewProcurementPriceList(input:{idempotencyKey:string;body:unknown}):Promise<Response>;
 commitProcurementPriceList(input:{idempotencyKey:string;body:unknown}):Promise<Response>;
 listProcurementPurchaseOrders(input:{status?:string;supplierId?:string}):Promise<Response>;
 listProcurementReconciliationFindings():Promise<Response>;
 createProcurementPurchaseOrder(input:{idempotencyKey:string;body:unknown}):Promise<Response>;
};
type Supplier={id:string;legal_party_id:string;name:string;status:string;currency:string;lead_time_days:number;minimum_order_minor:number;version:number};
type Offer={id:string;supplier_id:string;sku:string;supplier_sku?:string;gtin?:string;unit_price_minor:number;currency:string;moq:{value?:string;unit?:string};case_pack:{value?:string;unit?:string};lead_time_days:number;priority:number};
type Order={id:string;supplier_id:string;warehouse_id:string;status:string;currency:string;version:number;lines:any[];send_state:string;expected_receipt_at?:string};
type Finding={id:string;kind:string;purchase_order_id?:string;expected:string;observed:string;status:string;detected_at:string};

function items<T>(body:unknown):T[]{const value=body as {items?:unknown};return Array.isArray(value?.items)?value.items as T[]:[];}
function idempotency(){return crypto.randomUUID();}
const statusLabels:Record<string,string>={active:"Активен",blocked:"Заблокирован",archived:"Архив",draft:"Черновик",approved:"Согласован",sent:"Отправлен",partially_received:"Частично принят",received:"Принят",cancelled:"Отменён",open:"Открыто",unknown_send_outcome:"Неизвестный результат"};
function money(minor:number|undefined,currency:string|undefined){return `${((minor??0)/100).toLocaleString("ru-RU",{minimumFractionDigits:2})} ${currency??""}`.trim();}

export function ProcurementPage(){
 const api=useApi() as unknown as Client;
 const [tab,setTab]=useState<"suppliers"|"offers"|"orders"|"attention">("suppliers");
 return <Page eyebrow="Операции закупок" title="Закупки" description="Поставщики, условия, прайс-листы и заказы поставщикам в одном рабочем контуре. Складские остатки меняются только через WMS ledger.">
  <div className="catalog-tabs" role="tablist">
   <button role="tab" aria-selected={tab==="suppliers"} className={tab==="suppliers"?"active":""} onClick={()=>setTab("suppliers")}>Поставщики</button>
   <button role="tab" aria-selected={tab==="offers"} className={tab==="offers"?"active":""} onClick={()=>setTab("offers")}>Предложения</button>
   <button role="tab" aria-selected={tab==="orders"} className={tab==="orders"?"active":""} onClick={()=>setTab("orders")}>Заказы поставщикам</button>
   <button role="tab" aria-selected={tab==="attention"} className={tab==="attention"?"active":""} onClick={()=>setTab("attention")}>Внимание</button>
  </div>
  {tab==="suppliers"?<Suppliers api={api}/>:tab==="offers"?<Offers api={api}/>:tab==="orders"?<Orders api={api}/>:<Attention api={api}/>} 
 </Page>;
}

function Suppliers({api}:{api:Client}){
 const qc=useQueryClient(),toast=useToast();
 const query=useQuery({queryKey:["procurement","suppliers"],queryFn:async()=>items<Supplier>((await api.listProcurementSuppliers()).body),staleTime:15000});
 const [name,setName]=useState(""),[legalPartyID,setLegalPartyID]=useState(""),[currency,setCurrency]=useState("RUB");
 const create=useMutation({mutationFn:()=>api.createProcurementSupplier({idempotencyKey:idempotency(),body:{id:`sup_${idempotency().replaceAll("-","").slice(0,20)}`,legal_party_id:legalPartyID.trim(),name:name.trim(),currency}}),onSuccess:async()=>{setName("");setLegalPartyID("");toast.push({kind:"success",title:"Поставщик добавлен",body:"Профиль связан с канонической LegalParty."});await qc.invalidateQueries({queryKey:["procurement","suppliers"]})},onError:()=>toast.push({kind:"error",title:"Поставщик не добавлен",body:"Проверьте LegalParty и права на изменение справочника."})});
 if(query.isPending)return <LoadingBlock/>;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить поставщиков.</ErrorBlock>;
 const columns=[{key:"name",label:"Поставщик",value:(v:Supplier)=>v.name,render:(v:Supplier)=><span><strong>{v.name}</strong><small className="table-subline mono">{v.id} · LegalParty {v.legal_party_id}</small></span>},{key:"status",label:"Статус",value:(v:Supplier)=>statusLabels[v.status]??v.status,render:(v:Supplier)=><StatusBadge value={statusLabels[v.status]??v.status}/>},{key:"terms",label:"Условия",value:(v:Supplier)=>`${v.lead_time_days} дн. ${money(v.minimum_order_minor,v.currency)}`,render:(v:Supplier)=><span>{v.lead_time_days} дн.<small className="table-subline">минимум: {money(v.minimum_order_minor,v.currency)}</small></span>},{key:"version",label:"Версия",value:(v:Supplier)=>v.version,render:(v:Supplier)=><span className="mono">v{v.version}</span>}];
 return <div className="catalog-stack"><section className="panel inline-create"><div><h2>Новый поставщик</h2><p>Укажите ID записи LegalParty из раздела «Контрагенты». Юридические реквизиты здесь не дублируются.</p></div><form onSubmit={e=>{e.preventDefault();if(!create.isPending)create.mutate()}}><input required value={name} onChange={e=>setName(e.target.value)} placeholder="Название поставщика"/><input required value={legalPartyID} onChange={e=>setLegalPartyID(e.target.value)} placeholder="LegalParty ID"/><select value={currency} onChange={e=>setCurrency(e.target.value)}><option>RUB</option><option>USD</option><option>EUR</option></select><button className="button primary" disabled={create.isPending}>{create.isPending?"Сохраняем…":"Добавить"}</button></form></section>{query.data.length===0?<EmptyState title="Поставщиков пока нет" text="Добавьте связь с канонической LegalParty, чтобы завести офферы и заказы."/>:<section className="panel"><div className="drawer-section-heading"><div><h2>Справочник поставщиков</h2><p>Версионные условия и статус отношений.</p></div></div><DataTable rows={query.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Поставщик, LegalParty или статус…"/></section>}</div>;
}

function Offers({api}:{api:Client}){
 const toast=useToast(),[supplierID,setSupplierID]=useState(""),[uploadID,setUploadID]=useState(""),[format,setFormat]=useState("csv"),[preview,setPreview]=useState<any>(null);
 const query=useQuery({queryKey:["procurement","offers"],queryFn:async()=>items<Offer>((await api.listProcurementOffers({})).body),staleTime:15000});
 const previewImport=useMutation({mutationFn:()=>api.previewProcurementPriceList({idempotencyKey:idempotency(),body:{supplier_id:supplierID.trim(),upload_id:uploadID.trim(),format,mapping:{fields:{supplier_sku:"supplier_sku",gtin:"gtin",sku:"sku",unit_price_minor:"unit_price_minor",currency:"currency",moq:"moq",case_pack:"case_pack",lead_time_days:"lead_time_days",priority:"priority",unit:"unit"}}}}),onSuccess:(result)=>{setPreview(result.body);toast.push({kind:"success",title:"Прайс-лист проверен",body:"Импорт пока не применён. Проверьте сопоставление и ошибки строк."})},onError:()=>toast.push({kind:"error",title:"Прайс-лист не проверен",body:"Нужен released upload и корректные названия колонок."})});
 const commit=useMutation({mutationFn:()=>api.commitProcurementPriceList({idempotencyKey:idempotency(),body:{preview_id:preview?.id,allow_partial:false}}),onSuccess:(result)=>{setPreview(result.body);toast.push({kind:"success",title:"Цены обновлены",body:"Изменения записаны с новой версией истории."})},onError:()=>toast.push({kind:"error",title:"Импорт остановлен",body:"Исправьте ошибки или неоднозначные строки в preview."})});
 if(query.isPending)return <LoadingBlock/>;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить предложения поставщиков.</ErrorBlock>;
 const columns=[{key:"sku",label:"Товар",value:(v:Offer)=>v.sku,render:(v:Offer)=><span><strong>{v.sku}</strong><small className="table-subline">оффер {v.id} · поставщик {v.supplier_id}</small></span>},{key:"price",label:"Цена",value:(v:Offer)=>v.unit_price_minor,render:(v:Offer)=><strong>{money(v.unit_price_minor,v.currency)}</strong>},{key:"conditions",label:"Условия",value:(v:Offer)=>`${v.moq?.value??"1"} / ${v.case_pack?.value??"1"}`,render:(v:Offer)=><span>MOQ {v.moq?.value??"1"}<small className="table-subline">короб: {v.case_pack?.value??"1"} · срок {v.lead_time_days} дн.</small></span>},{key:"priority",label:"Приоритет",value:(v:Offer)=>v.priority,render:(v:Offer)=><span className="mono">{v.priority}</span>}];
 return <div className="catalog-stack"><section className="panel inline-create"><div><h2>Проверить прайс-лист</h2><p>Укажите ID уже released upload. Сначала строится preview с сопоставлением GTIN → SKU поставщика → ручной маппинг, затем отдельным действием применяются цены.</p></div><form onSubmit={e=>{e.preventDefault();if(!previewImport.isPending)previewImport.mutate()}}><input required value={supplierID} onChange={e=>setSupplierID(e.target.value)} placeholder="Supplier ID"/><input required value={uploadID} onChange={e=>setUploadID(e.target.value)} placeholder="Released upload ID"/><select value={format} onChange={e=>setFormat(e.target.value)}><option value="csv">CSV</option><option value="xlsx">XLSX</option></select><button className="button primary" disabled={previewImport.isPending}>{previewImport.isPending?"Проверяем…":"Собрать preview"}</button></form>{preview?<div className="inline-notice"><strong>Preview {preview.id}: {preview.status}</strong><span>{preview.valid_rows??0} валидных · {preview.invalid_rows??0} ошибочных · {preview.unresolved_rows??0} требуют маппинга</span>{preview.status!=="committed"?<button className="button ghost" disabled={commit.isPending||preview.status!=="ready"} onClick={()=>commit.mutate()}>{commit.isPending?"Применяем…":"Применить без пропусков"}</button>:null}</div>:null}</section><section className="panel"><div className="drawer-section-heading"><div><h2>Текущие предложения</h2><p>Цена, MOQ, упаковка и срок поставки. История изменений хранится отдельно.</p></div></div>{query.data.length===0?<EmptyState title="Предложений пока нет" text="Офферы добавляются через API или импорт прайс-листа после проверки."/>:<DataTable rows={query.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="SKU, GTIN или поставщик…"/>}</section></div>;
}

function Orders({api}:{api:Client}){
 const qc=useQueryClient(),toast=useToast();
 const query=useQuery({queryKey:["procurement","orders"],queryFn:async()=>items<Order>((await api.listProcurementPurchaseOrders({})).body),staleTime:10000});
 const [supplierID,setSupplierID]=useState(""),[warehouseID,setWarehouseID]=useState(""),[offerID,setOfferID]=useState(""),[sku,setSku]=useState(""),[quantity,setQuantity]=useState("1"),[price,setPrice]=useState("0");
 const create=useMutation({mutationFn:()=>{const key=idempotency();return api.createProcurementPurchaseOrder({idempotencyKey:key,body:{id:`po_${key.replaceAll("-","").slice(0,20)}`,supplier_id:supplierID.trim(),warehouse_id:warehouseID.trim(),currency:"RUB",lines:[{id:`line_${key.replaceAll("-","").slice(0,20)}`,offer_id:offerID.trim(),sku:sku.trim(),quantity,unit:"PCS",unit_price_minor:Number(price)}]}})},onSuccess:async()=>{toast.push({kind:"success",title:"Черновик заказа создан",body:"Следующий шаг — согласование и отправка."});setSupplierID("");setOfferID("");setSku("");await qc.invalidateQueries({queryKey:["procurement","orders"]})},onError:()=>toast.push({kind:"error",title:"Заказ не создан",body:"Проверьте поставщика, склад, оффер и цену."})});
 if(query.isPending)return <LoadingBlock/>;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить заказы поставщикам.</ErrorBlock>;
 const columns=[{key:"id",label:"Заказ",value:(v:Order)=>v.id,render:(v:Order)=><span><strong className="mono">{v.id}</strong><small className="table-subline">поставщик {v.supplier_id} · склад {v.warehouse_id}</small></span>},{key:"status",label:"Статус",value:(v:Order)=>statusLabels[v.status]??v.status,render:(v:Order)=><StatusBadge value={statusLabels[v.status]??v.status}/>},{key:"send_state",label:"Отправка",value:(v:Order)=>v.send_state,render:(v:Order)=><StatusBadge value={statusLabels[v.send_state]??v.send_state}/>},{key:"lines",label:"Строки",value:(v:Order)=>v.lines?.length??0,render:(v:Order)=><span>{v.lines?.length??0}<small className="table-subline">версия v{v.version}</small></span>}];
 return <div className="catalog-stack"><section className="panel inline-create"><div><h2>Черновик заказа</h2><p>Создание не меняет остатки. Приёмка создаёт факт для WMS, а не прямую корректировку stock.</p></div><form onSubmit={e=>{e.preventDefault();if(!create.isPending)create.mutate()}}><input required value={supplierID} onChange={e=>setSupplierID(e.target.value)} placeholder="Supplier ID"/><input required value={warehouseID} onChange={e=>setWarehouseID(e.target.value)} placeholder="Warehouse ID"/><input required value={offerID} onChange={e=>setOfferID(e.target.value)} placeholder="Offer ID"/><input required value={sku} onChange={e=>setSku(e.target.value)} placeholder="SKU"/><input required type="number" min="0" value={quantity} onChange={e=>setQuantity(e.target.value)} placeholder="Количество"/><input required type="number" min="0" value={price} onChange={e=>setPrice(e.target.value)} placeholder="Цена, коп."/><button className="button primary" disabled={create.isPending}>{create.isPending?"Создаём…":"Создать черновик"}</button></form></section>{query.data.length===0?<EmptyState title="Заказов пока нет" text="Черновики можно создать из формы или из рекомендации по пополнению."/>:<section className="panel"><div className="drawer-section-heading"><div><h2>Очередь заказов</h2><p>Lifecycle draft → approved → sent → receiving. Неизвестный remote result уходит в reconciliation.</p></div></div><DataTable rows={query.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Номер, поставщик или статус…"/></section>}</div>;
}

function Attention({api}:{api:Client}){
 const query=useQuery({queryKey:["procurement","findings"],queryFn:async()=>items<Finding>((await api.listProcurementReconciliationFindings()).body),staleTime:10000});
 if(query.isPending)return <LoadingBlock/>;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить расхождения закупок.</ErrorBlock>;
 const columns=[{key:"kind",label:"Тип",value:(v:Finding)=>statusLabels[v.kind]??v.kind,render:(v:Finding)=><StatusBadge value={statusLabels[v.kind]??v.kind}/>},{key:"order",label:"Заказ",value:(v:Finding)=>v.purchase_order_id??"—",render:(v:Finding)=><span className="mono">{v.purchase_order_id??"—"}</span>},{key:"expected",label:"Ожидалось",value:(v:Finding)=>v.expected},{key:"observed",label:"Наблюдение",value:(v:Finding)=>v.observed},{key:"detected",label:"Обнаружено",value:(v:Finding)=>v.detected_at,render:(v:Finding)=><span>{new Date(v.detected_at).toLocaleString("ru-RU")}</span>}];
 return <section className="panel"><div className="drawer-section-heading"><div><h2>Reconciliation закупок</h2><p>Здесь видны просроченная приёмка и неизвестный результат отправки. Данные обезличены.</p></div></div>{query.data.length===0?<EmptyState title="Расхождений нет" text="Состояние заказов и поставок сейчас согласовано."/>:<DataTable rows={query.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Тип, заказ или статус…"/>}</section>;
}
