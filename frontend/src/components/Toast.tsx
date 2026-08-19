import {createContext,useCallback,useContext,useMemo,useState} from "react";
import type {ReactNode} from "react";
import {Icon} from "./Icon";

type Kind="success"|"error"|"warning"|"info";
type Toast={id:string;kind:Kind;title:string;body?:string};
type ToastAPI={push:(toast:Omit<Toast,"id">)=>void};
const Context=createContext<ToastAPI>({push:()=>undefined});
export function ToastProvider({children}:{children:ReactNode}){const [items,setItems]=useState<Toast[]>([]);const push=useCallback((toast:Omit<Toast,"id">)=>{const id=crypto.randomUUID();setItems(current=>[...current,{...toast,id}].slice(-4));window.setTimeout(()=>setItems(current=>current.filter(item=>item.id!==id)),4500)},[]);const value=useMemo(()=>({push}),[push]);return <Context.Provider value={value}>{children}<div className="toast-region" role="region" aria-label="Системные сообщения" aria-live="polite">{items.map(item=><div className={`toast toast-${item.kind}`} key={item.id}><Icon name={item.kind==="success"?"check":item.kind==="error"?"error":item.kind==="warning"?"warning":"info"}/><div><strong>{item.title}</strong>{item.body?<p>{item.body}</p>:null}</div><button className="icon-button" onClick={()=>setItems(current=>current.filter(v=>v.id!==item.id))} aria-label="Закрыть"><Icon name="close" size={15}/></button></div>)}</div></Context.Provider>}
export function useToast(){return useContext(Context)}
