import type {ReactNode} from "react";

export function Page({eyebrow, title, description, actions, children}: {eyebrow?: string; title: string; description: string; actions?: ReactNode; children: ReactNode}) {
  return <section className="page"><header className="page-header"><div>{eyebrow && <p className="eyebrow">{eyebrow}</p>}<h1>{title}</h1><p className="page-description">{description}</p></div>{actions && <div className="page-actions">{actions}</div>}</header>{children}</section>;
}
