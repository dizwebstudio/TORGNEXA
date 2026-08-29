import type {ReactNode} from "react";
import {Icon} from "./Icon";
import {TableSkeleton} from "./Skeleton";

export function LoadingBlock() { return <TableSkeleton/>; }
export function ErrorBlock({children, retry, retryLabel = "Повторить"}: {children: ReactNode; retry?: () => void; retryLabel?: string}) {
  const retryRequest = retry ?? (() => window.location.reload());
  return <div className="panel alert error" role="alert"><Icon name="error"/><span>{children}</span><button type="button" className="button ghost" onClick={retryRequest}>{retryLabel}</button></div>;
}
