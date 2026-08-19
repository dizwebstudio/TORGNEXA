import type {ReactNode} from "react";
import {Icon} from "./Icon";
import {TableSkeleton} from "./Skeleton";

export function LoadingBlock() { return <TableSkeleton/>; }
export function ErrorBlock({children}: {children: ReactNode}) { return <div className="panel alert error" role="alert"><Icon name="error"/><span>{children}</span></div>; }
