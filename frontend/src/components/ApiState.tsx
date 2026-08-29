import type {ReactNode} from "react";
import {Icon} from "./Icon";
import {TableSkeleton} from "./Skeleton";

export function LoadingBlock() { return <TableSkeleton/>; }
export function ErrorBlock({children, retry, retryLabel = "Повторить"}: {children: ReactNode; retry?: () => void; retryLabel?: string}) {
  return <div className="panel alert error" role="alert"><Icon name="error"/><span>{children}</span>{retry ? <button type="button" className="button ghost" onClick={retry}>{retryLabel}</button> : null}</div>;
}
