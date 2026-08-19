import {useEffect,useMemo,useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {allowedNavigation} from "../shell/navigation";
import {navigate} from "../shell/useLocationPath";
import {useAuth} from "../auth/AuthProvider";
import {useApi} from "../api/ApiProvider";
import {Icon} from "./Icon";

type SearchItem={id:string;label:string;meta:string;path:string;kind:"product"|"order"|"connector"};
function items(value:unknown):any[]{return value&&typeof value==="object"&&Array.isArray((value as {items?:unknown}).items)?(value as {items:any[]}).items:[]}
export function CommandPalette({open,onClose}:{open:boolean;onClose:()=>void}){
 const {session}=useAuth(),api=useApi() as any,[query,setQuery]=useState(""),[serverQuery,setServerQuery]=useState("");const caps=session?.capabilities??[];
 useEffect(()=>{if(open){setQuery("");setServerQuery("")}},[open]);
 useEffect(()=>{const timer=window.setTimeout(()=>setServerQuery(query.trim()),250);return()=>window.clearTimeout(timer)},[query]);
 const enabled=open&&serverQuery.length>=2;
 const products=useQuery({queryKey:["command","products",serverQuery],enabled:enabled&&caps.includes("products.read"),queryFn:async()=>items((await api.listProducts({q:serverQuery,limit:8})).body)});
 const orders=useQuery({queryKey:["command","orders",serverQuery],enabled:enabled&&caps.includes("orders.read"),queryFn:async()=>items((await api.listOrders({q:serverQuery,limit:8})).body)});
 const connectors=useQuery({queryKey:["command","connectors"],enabled:open&&caps.includes("connectors.read"),queryFn:async()=>items((await api.listConnectorAccounts({limit:100})).body),staleTime:30_000});
 const nav=useMemo(()=>allowedNavigation(caps).filter(item=>`${item.label} ${item.id}`.toLowerCase().includes(query.trim().toLowerCase())),[query,caps]);
 const entities=useMemo<SearchItem[]>(()=>{const q=serverQuery.toLowerCase();if(q.length<2)return[];const all:SearchItem[]=[...(products.data??[]).map((v:any)=>({id:v.id,label:v.title??v.code,meta:`Товар · ${v.code??""}`,path:`/catalog/${encodeURIComponent(v.id)}`,kind:"product" as const})),...(orders.data??[]).map((v:any)=>({id:v.id,label:v.order_number??v.id,meta:`Заказ · ${v.status??""}`,path:`/orders/${encodeURIComponent(v.id)}`,kind:"order" as const})),...(connectors.data??[]).filter((v:any)=>`${v.id} ${v.connector_id} ${v.health_status??""}`.toLowerCase().includes(q)).slice(0,5).map((v:any)=>({id:v.id,label:v.id,meta:`Интеграция · ${v.connector_id} · ${v.health_status??v.status??""}`,path:"/integrations",kind:"connector" as const}))];return all.slice(0,16)},[products.data,orders.data,connectors.data,serverQuery]);
 const searching=enabled&&(products.isFetching||orders.isFetching);
 if(!open)return null;return <div className="command-layer" role="dialog" aria-modal="true" aria-label="Поиск и быстрый переход"><button className="command-backdrop" onClick={onClose} aria-label="Закрыть"/><div className="command-palette"><div className="command-search"><Icon name="search"/><input autoFocus value={query} onChange={event=>setQuery(event.target.value)} placeholder="Раздел, заказ, товар или интеграция…" aria-label="Глобальный поиск на сервере"/><kbd>Esc</kbd></div><div className="command-results">{nav.length?<div className="command-group-label">Разделы</div>:null}{nav.map(item=><button key={item.id} onClick={()=>{navigate(item.path);onClose()}}><span><Icon name={item.icon}/><strong>{item.label}</strong></span><small>{item.shortcut??""}</small></button>)}{enabled?<div className="command-group-label">Данные · server search {searching?"…":""}</div>:null}{entities.map(item=><button key={`${item.kind}-${item.id}`} onClick={()=>{navigate(item.path);onClose()}}><span><Icon name={item.kind==="product"?"catalog":item.kind==="order"?"orders":"connectors"}/><span className="command-entity"><strong>{item.label}</strong><small>{item.meta}</small></span></span><Icon name="chevron"/></button>)}{nav.length===0&&entities.length===0&&!searching?<p>{query.trim().length<2?"Введите минимум 2 символа для поиска по серверным данным.":"Ничего не найдено"}</p>:null}</div></div></div>
}
