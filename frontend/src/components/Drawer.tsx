import {useId, type ReactNode} from "react";
import {Icon} from "./Icon";
import {useFocusTrap} from "./useFocusTrap";

export function Drawer({open,title,subtitle,onClose,children}:{open:boolean;title:string;subtitle?:string;onClose:()=>void;children:ReactNode}) {
  const drawerRef=useFocusTrap(open,onClose);
  const id=useId();
  if(!open)return null;
  const titleId=`drawer-title-${id}`;
  return <div className="drawer-layer" role="presentation"><button className="drawer-backdrop" tabIndex={-1} aria-label="Закрыть панель" onClick={onClose}/><aside ref={drawerRef} className="drawer" role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}><header className="drawer-header"><div><h2 id={titleId}>{title}</h2>{subtitle?<p>{subtitle}</p>:null}</div><button className="icon-button" onClick={onClose} aria-label="Закрыть"><Icon name="close"/></button></header><div className="drawer-body">{children}</div></aside></div>;
}
