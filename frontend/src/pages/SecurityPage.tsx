import {SecuritySettings} from "../features/settings/SecuritySettings";
import {Page} from "./Page";

export function SecurityPage() {
  return <Page eyebrow="КОНТРОЛЬ ДОСТУПА" title="Безопасность" description="Активные сессии, история входов и изменения в рабочем пространстве. Пароль и SSO-сессии управляются через защищённый кабинет провайдера идентификации.">
    <SecuritySettings />
  </Page>;
}
