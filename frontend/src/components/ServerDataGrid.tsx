import {useEffect,useState} from "react";
import type {ReactNode} from "react";
import {Icon} from "./Icon";

export type ServerColumn<T>={key:string;label:string;render:(row:T)=>ReactNode;align?:"start"|"end"};
export type GridFilter={value:string;label:string};

export function ServerDataGrid<T>({rows,columns,rowKey,query,onQuery,filter,filterOptions=[],onFilter,onOpen,loading=false,empty="Нет данных",hasNext=false,hasPrevious=false,onNext,onPrevious,rangeLabel}: {
 rows:T[];columns:ServerColumn<T>[];rowKey:(row:T)=>string;query:string;onQuery:(value:string)=>void;filter?:string;filterOptions?:GridFilter[];onFilter?:(value:string)=>void;onOpen?:(row:T)=>void;loading?:boolean;empty?:string;hasNext?:boolean;hasPrevious?:boolean;onNext?:()=>void;onPrevious?:()=>void;rangeLabel?:string;
}){
 const [draft,setDraft]=useState(query);
 useEffect(()=>setDraft(query),[query]);
 useEffect(()=>{const timer=window.setTimeout(()=>{if(draft!==query)onQuery(draft)},300);return()=>window.clearTimeout(timer)},[draft,query,onQuery]);
 return <section className="data-table server-grid" aria-busy={loading}>
  <div className="data-toolbar"><label className="table-search"><Icon name="search"/><input value={draft} onChange={e=>setDraft(e.target.value)} placeholder="Поиск на сервере…" aria-label="Поиск по данным на сервере"/></label>{onFilter?<select className="grid-filter" value={filter??""} onChange={e=>onFilter(e.target.value)} aria-label="Фильтр"><option value="">Все статусы</option>{filterOptions.map(v=><option key={v.value} value={v.value}>{v.label}</option>)}</select>:null}<span className="server-grid-badge"><Icon name="sync" size={14}/> на сервере</span></div>
  <div className="table-wrap"><table><thead><tr>{columns.map(col=><th key={col.key} className={col.align==="end"?"text-end":""}>{col.label}</th>)}{onOpen?<th/>:null}</tr></thead><tbody>{rows.map(row=><tr key={rowKey(row)}>{columns.map(col=><td key={col.key} className={col.align==="end"?"text-end":""}>{col.render(row)}</td>)}{onOpen?<td className="row-action"><button className="icon-button" onClick={()=>onOpen(row)} aria-label="Открыть"><Icon name="chevron"/></button></td>:null}</tr>)}</tbody></table>{!loading&&rows.length===0?<div className="table-empty">{empty}</div>:null}{loading?<div className="grid-loading">Обновляем данные…</div>:null}</div>
  <footer className="table-footer"><span>{rangeLabel??`${rows.length} записей на странице`}</span><div><button className="button ghost" disabled={!hasPrevious||loading} onClick={onPrevious}>Назад</button><button className="button ghost" disabled={!hasNext||loading} onClick={onNext}>Далее</button></div></footer>
 </section>
}
