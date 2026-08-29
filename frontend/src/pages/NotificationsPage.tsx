import {NotificationList} from "../features/notifications/NotificationList";
import {Page} from "./Page";
export function NotificationsPage() { return <Page eyebrow="Операции" title="Уведомления" description="Персональный inbox с серверной дедупликацией и приоритетами."><NotificationList /></Page>; }
