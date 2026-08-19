import type {NavigationItem} from "../shell/navigation";
import {Page} from "./Page";
import {EmptyState} from "../components/EmptyState";
export function PlaceholderPage({item}: {item: NavigationItem}) { return <Page eyebrow={item.risk} title={item.label} description="Раздел уже встроен в capability-aware frontend shell; функциональный экран подключится в своей atomic-задаче."><EmptyState title="Контур готов" text="Навигация, auth boundary и route guard уже работают. Бизнес-функции этого раздела не дублируются раньше соответствующей задачи." /></Page>; }
