import {ProductList} from "../features/catalog/ProductList";
import {Page} from "./Page";
import {useLocationPath} from "../shell/useLocationPath";
export function CatalogPage() { const path=useLocationPath(); const id=path.startsWith("/catalog/")?decodeURIComponent(path.slice("/catalog/".length)):undefined; return <Page eyebrow="Commerce Core" title="Каталог" description="Товары, предложения, цены, категории и изображения текущего рабочего пространства."><ProductList initialId={id}/></Page>; }
