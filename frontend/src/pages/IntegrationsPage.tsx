import {IntegrationCatalog} from "../features/settings/IntegrationCatalog";
import {Page} from "./Page";

export function IntegrationsPage(){return <Page eyebrow="Внешние системы" title="Интеграции" description="Маркетплейсы, доски объявлений, социальные сети, ERP, доставка, платежи и государственные системы."><IntegrationCatalog/></Page>}
