import {useId, type ReactNode} from "react";
import {Icon} from "./Icon";
import {useFocusTrap} from "./useFocusTrap";

export function Dialog({open,title,description,onClose,children}:{open:boolean;title:string;description?:string;onClose:()=>void;children:ReactNode}){
  const dialogRef=useFocusTrap(open,onClose);
  const id=useId();
  if(!open)return null;
  const titleId=`dialog-title-${id}`;
  const descriptionId=description?`dialog-description-${id}`:undefined;
  return <div className="dialog-layer" role="presentation"><button className="dialog-backdrop" tabIndex={-1} onClick={onClose} aria-label="Закрыть"/><section ref={dialogRef} className="dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId} tabIndex={-1}><header><div><h2 id={titleId}>{title}</h2>{description?<p id={descriptionId}>{description}</p>:null}</div><button className="icon-button" onClick={onClose} aria-label="Закрыть"><Icon name="close"/></button></header><div className="dialog-body">{children}</div></section></div>
}
