import {useMemo, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {decodeReturnDetails, decodeReturnPage, type ReturnItemHit, type ReturnSummary} from "../api/decoders";
import {useAuth} from "../auth/AuthProvider";
import {DataTable} from "../components/DataTable";
import {Drawer} from "../components/Drawer";
import {EmptyState} from "../components/EmptyState";
import {ErrorBlock, LoadingBlock} from "../components/ApiState";
import {StatusBadge} from "../components/StatusBadge";
import {useToast} from "../components/Toast";
import {Page} from "./Page";
import {navigate, useLocationPath} from "../shell/useLocationPath";

const statusLabels: Readonly<Record<string, string>> = {
  requested: "Запрошен",
  approved: "Согласован",
  authorized: "Авторизован",
  in_transit: "В пути",
  received: "Получен",
  inspecting: "На проверке",
  accepted: "Принят",
  partially_accepted: "Принят частично",
  rejected: "Отклонён",
  closed: "Закрыт",
  cancelled: "Отменён",
  expired: "Истёк",
};

const dispositionLabels: Readonly<Record<string, string>> = {
  restock: "Вернуть на склад",
  quarantine: "Карантин",
  scrap: "Утилизация",
  replace: "Замена",
};

type ReturnAction = {status: string; label: string; danger?: boolean};

function actionsFor(status: string): ReturnAction[] {
  if (status === "requested") return [{status: "approved", label: "Согласовать"}, {status: "cancelled", label: "Отменить", danger: true}];
  if (status === "approved") return [{status: "authorized", label: "Авторизовать"}, {status: "cancelled", label: "Отменить", danger: true}];
  if (status === "authorized") return [{status: "in_transit", label: "Передать в доставку"}, {status: "cancelled", label: "Отменить", danger: true}];
  if (status === "in_transit") return [{status: "received", label: "Подтвердить получение"}, {status: "cancelled", label: "Отменить", danger: true}];
  if (status === "received") return [{status: "inspecting", label: "Начать проверку"}];
  if (status === "inspecting") return [{status: "accepted", label: "Принять"}, {status: "partially_accepted", label: "Принять частично"}, {status: "rejected", label: "Отклонить", danger: true}];
  if (["accepted", "partially_accepted", "rejected"].includes(status)) return [{status: "closed", label: "Закрыть"}];
  return [];
}

function quantity(value: {coefficient: number; scale: number; unit: string}): string {
  const amount = value.coefficient / (10 ** value.scale);
  return `${amount} ${value.unit}`;
}

function money(minor: number, currency: string): string {
  return new Intl.NumberFormat("ru-RU", {style: "currency", currency}).format(minor / 100);
}

function ReturnItemRow({item}: {item: ReturnItemHit}) {
  return <div className="line-item"><div className="line-product-copy"><strong>{item.order_item_id}</strong><small>Запрошено: {quantity(item.requested)} · Получено: {quantity(item.received)} · Принято: {quantity(item.accepted)}</small></div><div className="line-price"><StatusBadge value={item.disposition}/><small>{dispositionLabels[item.disposition] ?? item.disposition}</small></div></div>;
}

export function ReturnsPage() {
  const api = useApi();
  const auth = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const path = useLocationPath();
  const selectedID = path.startsWith("/returns/") ? decodeURIComponent(path.slice("/returns/".length)) : undefined;
  const [status, setStatus] = useState("");
  const canWrite = auth.session?.capabilities.includes("orders.returns.write") ?? false;
  const returns = useQuery({queryKey: ["returns"], queryFn: async () => decodeReturnPage((await api.listReturns({limit: 200})).body), staleTime: 15_000});
  const details = useQuery({queryKey: ["returns", "detail", selectedID], enabled: !!selectedID, queryFn: async () => decodeReturnDetails((await api.getReturn({returnId: selectedID!})).body)});
  const changeStatus = useMutation({
    mutationFn: async (input: {id: string; status: string; version: number}) => api.changeReturnStatus({returnId: input.id, idempotencyKey: crypto.randomUUID(), body: {status: input.status, version: input.version}}),
    onSuccess: async (_result, input) => {
      toast.push({kind: "success", title: "Статус возврата изменён"});
      await Promise.all([queryClient.invalidateQueries({queryKey: ["returns"]}), queryClient.invalidateQueries({queryKey: ["returns", "detail", input.id]})]);
    },
    onError: (error: unknown) => {
      const statusCode = typeof error === "object" && error !== null && "statusCode" in error ? (error as {statusCode?: number}).statusCode : undefined;
      toast.push({kind: "error", title: "Не удалось изменить статус возврата", body: statusCode === 409 ? "Возврат уже изменился или такой переход недоступен. Обновите карточку." : "Проверьте права и повторите операцию."});
    },
  });
  const rows = useMemo(() => (returns.data?.items ?? []).filter((item) => !status || item.status === status), [returns.data, status]);
  const selected = details.data?.return;
  const actions = selected ? actionsFor(selected.status) : [];
  const columns = [
    {key: "id", label: "Возврат", value: (item: ReturnSummary) => item.id, render: (item: ReturnSummary) => <span className="mono table-primary">{item.id}</span>},
    {key: "order", label: "Заказ", value: (item: ReturnSummary) => item.order_id, render: (item: ReturnSummary) => <span className="mono">{item.order_id}</span>},
    {key: "status", label: "Статус", value: (item: ReturnSummary) => item.status, render: (item: ReturnSummary) => <StatusBadge value={item.status}/>},
    {key: "reason", label: "Причина", value: (item: ReturnSummary) => item.reason_code},
    {key: "amount", label: "Доставка / налог", value: (item: ReturnSummary) => `${item.requested_shipping_minor + item.requested_tax_minor}`, render: (item: ReturnSummary) => <strong>{money(item.requested_shipping_minor + item.requested_tax_minor, item.currency)}</strong>, align: "end" as const},
    {key: "created", label: "Создан", value: (item: ReturnSummary) => item.created_at, render: (item: ReturnSummary) => <time>{new Date(item.created_at).toLocaleString("ru-RU")}</time>},
  ];

  return <Page eyebrow="Commerce Core" title="Возвраты и refunds" description="Единый контур возвратов, отмен и связанных возвратных операций с контролем статуса, версий и аудита.">
    <div className="catalog-tabs" role="tablist" aria-label="Фильтр возвратов">
      <button type="button" role="tab" aria-selected={!status} className={!status ? "active" : ""} onClick={() => setStatus("")}>Все</button>
      {Object.entries(statusLabels).map(([value, label]) => <button type="button" role="tab" aria-selected={status === value} className={status === value ? "active" : ""} key={value} onClick={() => setStatus(value)}>{label}</button>)}
    </div>
    {returns.isPending ? <LoadingBlock/> : returns.isError ? <ErrorBlock retry={() => void returns.refetch()}>Не удалось загрузить возвраты.</ErrorBlock> : rows.length === 0 && !status ? <EmptyState title="Возвратов пока нет" text="Создайте возврат через API или дождитесь события от канала продаж."/> : <DataTable rows={rows} columns={columns} rowKey={(item) => item.id} searchPlaceholder="Возврат, заказ, причина…" empty="По выбранному фильтру возвратов нет" onOpen={(item) => navigate(`/returns/${encodeURIComponent(item.id)}`)}/>} 
    <Drawer open={!!selectedID} title={selected ? `Возврат ${selected.id}` : "Возврат"} subtitle={selected ? `Заказ ${selected.order_id}` : undefined} onClose={() => navigate("/returns")}>
      {details.isPending ? <LoadingBlock/> : details.isError ? <ErrorBlock retry={() => void details.refetch()}>Не удалось открыть возврат.</ErrorBlock> : selected ? <>
        <div className="drawer-kpis"><div><small>Статус</small><StatusBadge value={selected.status}/></div><div><small>Позиции</small><strong>{details.data.items.length}</strong></div><div><small>Доставка и налог</small><strong>{money(selected.requested_shipping_minor + selected.requested_tax_minor, selected.currency)}</strong></div></div>
        {canWrite && actions.length > 0 ? <section className="drawer-section order-actions"><h3>Действия</h3><p className="drawer-help">Переход проверяется по текущей версии возврата, записывается в аудит и публикуется через outbox.</p><div className="button-row">{actions.map((action) => <button type="button" key={action.status} className={`button ${action.danger ? "danger" : "primary"}`} disabled={changeStatus.isPending} onClick={() => changeStatus.mutate({id: selected.id, status: action.status, version: selected.version})}>{changeStatus.isPending ? "Сохраняем…" : action.label}</button>)}</div></section> : null}
        <section className="drawer-section"><h3>Причина и источник</h3><dl className="detail-list"><div><dt>Причина</dt><dd>{selected.reason_code}</dd></div><div><dt>Источник</dt><dd>{selected.source}</dd></div><div><dt>Создан</dt><dd>{new Date(selected.created_at).toLocaleString("ru-RU")}</dd></div><div><dt>Версия</dt><dd className="mono">{selected.version}</dd></div></dl></section>
        <section className="drawer-section"><h3>Позиции возврата</h3>{details.data.items.length > 0 ? <div className="line-items">{details.data.items.map((item) => <ReturnItemRow key={item.id} item={item}/>)}</div> : <p className="drawer-help">Позиции ещё не добавлены.</p>}</section>
      </> : null}
    </Drawer>
  </Page>;
}
