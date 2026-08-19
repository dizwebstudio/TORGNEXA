import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {decodeNotificationPage} from "../../api/decoders";
import {EmptyState} from "../../components/EmptyState";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";

export function NotificationList() {
  const api = useApi();
  const cache = useQueryClient();
  const toast = useToast();
  const query = useQuery({
    queryKey: ["notifications", "inbox"],
    queryFn: async () => decodeNotificationPage((await api.listNotifications({limit: 50})).body),
    staleTime: 15_000,
  });
  const markRead = useMutation({
    mutationFn: async (notificationId: string) => api.markNotificationRead({notificationId}),
    onSuccess: async () => { toast.push({kind:"success",title:"Уведомление прочитано"}); await cache.invalidateQueries({queryKey: ["notifications", "inbox"]}); },
  });
  if (query.isPending) return <LoadingBlock />;
  if (query.isError) return <ErrorBlock>Не удалось загрузить уведомления.</ErrorBlock>;
  if (query.data.items.length === 0) return <EmptyState title="Новых уведомлений нет" text="Системные события и важные предупреждения появятся здесь." />;
  return <div className="notification-list">{query.data.items.map((item) => <article className={`notification ${item.read_at ? "read" : "unread"}`} key={item.id}><div className="notification-top"><StatusBadge value={item.severity} /><time>{new Date(item.updated_at).toLocaleString("ru-RU")}</time></div><h3>{item.title}</h3><p>{item.body}</p><div className="notification-bottom"><span>Повторов: {item.occurrence_count}</span>{!item.read_at && <button className="button ghost" disabled={markRead.isPending} onClick={() => markRead.mutate(item.id)}>Отметить прочитанным</button>}</div></article>)}</div>;
}
