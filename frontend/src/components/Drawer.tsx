import type {ReactNode} from "react";
import {useEffect} from "react";
import {Icon} from "./Icon";

export function Drawer({open,title,subtitle,onClose,children}:{open:boolean;title:string;subtitle?:string;onClose:()=>void;children:ReactNode}) {
  useEffect(()=>{if(!open)return;const handler=(event:KeyboardEvent)=>{if(event.key==="Escape")onClose()};window.addEventListener("keydown",handler);return()=>window.removeEventListener("keydown",handler)},[open,onClose]);
  if(!open)return null;
  return <div className="drawer-layer" role="presentation"><button className="drawer-backdrop" aria-label="Закрыть панель" onClick={onClose}/><aside className="drawer" role="dialog" aria-modal="true" aria-label={title}><header className="drawer-header"><div><h2>{title}</h2>{subtitle?<p>{subtitle}</p>:null}</div><button className="icon-button" onClick={onClose} aria-label="Закрыть"><Icon name="close"/></button></header><div className="drawer-body">{children}</div></aside></div>;
}
