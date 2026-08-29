import {useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {decodeNotificationPage} from "../../api/decoders";
import {EmptyState} from "../../components/EmptyState";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";
import {Drawer} from "../../components/Drawer";

export function NotificationList() {
  const api = useApi();
  const cache = useQueryClient();
  const toast = useToast();
  const [selected, setSelected] = useState<string|null>(null);
  const query = useQuery({
    queryKey: ["notifications", "inbox"],
    queryFn: async () => decodeNotificationPage((await api.listNotifications({limit: 50})).body),
    staleTime: 15_000,
  });
  const markRead = useMutation({
    mutationFn: async (notificationId: string) => api.markNotificationRead({notificationId}),
    onSuccess: async () => { toast.push({kind:"success",title:"Уведомление прочитано"}); await cache.invalidateQueries({queryKey: ["notifications", "inbox"]}); },
  });
  const deliveries = useQuery({
    queryKey: ["notifications", "deliveries", selected],
    enabled: Boolean(selected),
    queryFn: async () => ((await api.listNotificationDeliveries({notificationId: selected!})).body as {items?: unknown}).items ?? [],
  });
  if (query.isPending) return <LoadingBlock />;
  if (query.isError) return <ErrorBlock retry={() => void query.refetch()}>Не удалось загрузить уведомления.</ErrorBlock>;
  if (query.data.items.length === 0) return <EmptyState title="Новых уведомлений нет" text="Системные события и важные предупреждения появятся здесь." />;
  return <><div className="notification-list">{query.data.items.map((item) => <article className={`notification ${item.read_at ? "read" : "unread"}`} key={item.id}><div className="notification-top"><StatusBadge value={item.severity} /><time>{new Date(item.updated_at).toLocaleString("ru-RU")}</time></div><h3>{item.title}</h3><p>{item.body}</p><div className="notification-bottom"><span>Повторов: {item.occurrence_count}</span><button type="button" className="button ghost" onClick={() => setSelected(item.id)}>История доставки</button>{!item.read_at && <button type="button" className="button ghost" disabled={markRead.isPending} onClick={() => markRead.mutate(item.id)}>Отметить прочитанным</button>}</div></article>)}</div><Drawer open={!!selected} title="История доставки" subtitle={selected??undefined} onClose={() => setSelected(null)}>{deliveries.isPending?<LoadingBlock/>:deliveries.isError?<ErrorBlock retry={() => void deliveries.refetch()}>Не удалось загрузить историю доставки.</ErrorBlock>:<div className="delivery-history">{(deliveries.data as any[]).length===0?<p className="drawer-help">Попыток доставки пока нет.</p>:(deliveries.data as any[]).map((item:any,index:number)=><article className="delivery-attempt" key={item.id??index}><div><strong>{item.channel??"Канал"}</strong><small>{item.status??item.outcome??"—"}</small></div><time>{item.created_at?new Date(item.created_at).toLocaleString("ru-RU"):item.delivered_at?new Date(item.delivered_at).toLocaleString("ru-RU"):"—"}</time></article>)}</div>}</Drawer></>;
}
