import {NotificationList} from "../features/notifications/NotificationList";
import {Page} from "./Page";
export function NotificationsPage() { return <Page eyebrow="Operations" title="Уведомления" description="Персональный tenant-scoped inbox с серверной дедупликацией и severity."><NotificationList /></Page>; }
