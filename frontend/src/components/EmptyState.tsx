import type {ReactNode} from "react";
export function EmptyState({title, text, children}: {title: string; text: string; children?: ReactNode}) {
  return <div className="empty"><div className="empty-icon">◇</div><h3>{title}</h3><p>{text}</p>{children}</div>;
}
