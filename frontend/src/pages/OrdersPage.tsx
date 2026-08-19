import {OrderList} from "../features/orders/OrderList";
import {Page} from "./Page";
import {useLocationPath} from "../shell/useLocationPath";
export function OrdersPage() { const path=useLocationPath(); const id=path.startsWith("/orders/")?decodeURIComponent(path.slice("/orders/".length)):undefined; return <Page eyebrow="Commerce Core" title="Заказы" description="Единый список заказов независимо от внешнего канала."><OrderList initialId={id}/></Page>; }
