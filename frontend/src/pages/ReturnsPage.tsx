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

const reasonLabels: Readonly<Record<string, string>> = {
  customer_changed_mind: "Покупатель передумал",
  damaged: "Товар повреждён",
  defective: "Товар с дефектом",
  wrong_item: "Доставлен другой товар",
  not_as_described: "Товар не соответствует описанию",
  delivery_delay: "Задержка доставки",
};

const sourceLabels: Readonly<Record<string, string>> = {
  "api.returns": "Раздел возвратов",
  marketplace: "Канал продаж",
  storefront: "Интернет-магазин",
  customer_service: "Клиентский сервис",
};

const unitLabels: Readonly<Record<string, string>> = {
  PCS: "шт.",
  KG: "кг",
  G: "г",
  L: "л",
  ML: "мл",
};

type ReturnAction = {status: string; label: string; danger?: boolean};

type ReturnsClient = {
  createOrderCancellation(input: {idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  createReturn(input: {idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  createReturnItem(input: {returnId: string; idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  createReturnLogistics(input: {returnId: string; idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  recordReturnInspection(input: {returnId: string; idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  createRefundAllocation(input: {idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  changeReturnStatus(input: {returnId: string; idempotencyKey: string; body: unknown}): Promise<{body: unknown}>;
  listReturns(input: {limit: number}): Promise<{body: unknown}>;
  getReturn(input: {returnId: string}): Promise<{body: unknown}>;
};

function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const millis = BigInt(Date.now());
  for (let index = 5; index >= 0; index -= 1) bytes[5 - index] = Number((millis >> BigInt(index * 8)) & 255n);
  bytes[6] = (bytes[6] & 15) | 112;
  bytes[8] = (bytes[8] & 63) | 128;
  const raw = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${raw.slice(0, 8)}-${raw.slice(8, 12)}-${raw.slice(12, 16)}-${raw.slice(16, 20)}-${raw.slice(20)}`;
}

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
  return `${amount} ${unitLabels[value.unit] ?? value.unit}`;
}

function money(minor: number, currency: string): string {
  return new Intl.NumberFormat("ru-RU", {style: "currency", currency}).format(minor / 100);
}

function reasonLabel(value: string): string {
  return reasonLabels[value] ?? value;
}

function source(value: string): string {
  return sourceLabels[value] ?? value;
}

function ReturnItemRow({item}: {item: ReturnItemHit}) {
  return <div className="line-item"><div className="line-product-copy"><strong>{item.order_item_id}</strong><small>Запрошено: {quantity(item.requested)} · Получено: {quantity(item.received)} · Принято: {quantity(item.accepted)}</small></div><div className="line-price"><StatusBadge value={item.disposition}/><small>{dispositionLabels[item.disposition] ?? item.disposition}</small></div></div>;
}

export function ReturnsPage() {
  const api = useApi() as unknown as ReturnsClient;
  const auth = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const path = useLocationPath();
  const selectedID = path.startsWith("/returns/") ? decodeURIComponent(path.slice("/returns/".length)) : undefined;
  const [status, setStatus] = useState("");
  const [tab, setTab] = useState<"returns" | "cancellations">("returns");
  const [orderID, setOrderID] = useState("");
  const [reason, setReason] = useState("customer_changed_mind");
  const [currency, setCurrency] = useState("RUB");
  const [shipping, setShipping] = useState("0");
  const [tax, setTax] = useState("0");
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
  const createCancellation = useMutation({
    mutationFn: () => api.createOrderCancellation({idempotencyKey: uuidV7(), body: {id: uuidV7(), order_id: orderID.trim(), reason_code: reason.trim()}}),
    onSuccess: () => { toast.push({kind: "success", title: "Отмена поставлена в обработку", body: "Дальнейший статус появится после фоновой обработки и сверки."}); setOrderID(""); },
    onError: () => toast.push({kind: "error", title: "Отмена не создана", body: "Проверьте ID заказа, причину и права."}),
  });
  const createReturn = useMutation({
    mutationFn: () => api.createReturn({idempotencyKey: uuidV7(), body: {id: uuidV7(), order_id: orderID.trim(), reason_code: reason.trim(), currency, shipping_minor: Number(shipping), tax_minor: Number(tax)}}),
    onSuccess: async () => { toast.push({kind: "success", title: "Возврат создан", body: "Добавьте строки и проведите приёмку в карточке возврата."}); setOrderID(""); await queryClient.invalidateQueries({queryKey: ["returns"]}); },
    onError: () => toast.push({kind: "error", title: "Возврат не создан", body: "Проверьте ID заказа, валюту и суммы."}),
  });
  const rows = useMemo(() => (returns.data?.items ?? []).filter((item) => !status || item.status === status), [returns.data, status]);
  const selected = details.data?.return;
  const actions = selected ? actionsFor(selected.status) : [];
  const columns = [
    {key: "id", label: "Возврат", value: (item: ReturnSummary) => item.id, render: (item: ReturnSummary) => <span className="mono table-primary">{item.id}</span>},
    {key: "order", label: "Заказ", value: (item: ReturnSummary) => item.order_id, render: (item: ReturnSummary) => <span className="mono">{item.order_id}</span>},
    {key: "status", label: "Статус", value: (item: ReturnSummary) => item.status, render: (item: ReturnSummary) => <StatusBadge value={item.status}/>},
    {key: "reason", label: "Причина", value: (item: ReturnSummary) => reasonLabel(item.reason_code)},
    {key: "amount", label: "Доставка / налог", value: (item: ReturnSummary) => `${item.requested_shipping_minor + item.requested_tax_minor}`, render: (item: ReturnSummary) => <strong>{money(item.requested_shipping_minor + item.requested_tax_minor, item.currency)}</strong>, align: "end" as const},
    {key: "created", label: "Создан", value: (item: ReturnSummary) => item.created_at, render: (item: ReturnSummary) => <time>{new Date(item.created_at).toLocaleString("ru-RU")}</time>},
  ];

  return <Page eyebrow="Операционная работа" title="Возвраты и возврат средств" description="Отмена заказа, физический возврат и возврат оплаты идут раздельно. Каждое действие идемпотентно и фиксируется в журнале аудита и очереди событий.">
    <div className="catalog-tabs" role="tablist" aria-label="Контур возврата">
      <button type="button" role="tab" aria-selected={tab === "returns"} className={tab === "returns" ? "active" : ""} onClick={() => setTab("returns")}>Возвраты</button>
      <button type="button" role="tab" aria-selected={tab === "cancellations"} className={tab === "cancellations" ? "active" : ""} onClick={() => setTab("cancellations")}>Отмены заказов</button>
    </div>
    <section className="panel inline-create">
      <div><h2>{tab === "returns" ? "Оформить возврат" : "Запросить отмену"}</h2><p>{tab === "returns" ? "Создаётся только запрос. Приёмка, способ обработки товара и возврат средств подтверждаются отдельными шагами." : "Отмена не переписывает заказ: фоновая обработка и сверка доводят отдельную операцию отмены."}</p></div>
      <form onSubmit={(event) => { event.preventDefault(); if (tab === "returns") createReturn.mutate(); else createCancellation.mutate(); }}>
        <input required value={orderID} onChange={(event) => setOrderID(event.target.value)} placeholder="ID заказа" aria-label="ID заказа" />
        <input required value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Код причины" aria-label="Код причины" />
        {tab === "returns" ? <>
          <input required value={currency} onChange={(event) => setCurrency(event.target.value.toUpperCase().slice(0, 3))} maxLength={3} placeholder="RUB" aria-label="Валюта" />
          <input type="number" min="0" value={shipping} onChange={(event) => setShipping(event.target.value)} placeholder="Доставка, коп." aria-label="Возврат доставки" />
          <input type="number" min="0" value={tax} onChange={(event) => setTax(event.target.value)} placeholder="Налог, коп." aria-label="Возврат налога" />
        </> : null}
        <button className="button primary" disabled={!orderID.trim() || createReturn.isPending || createCancellation.isPending}>{createReturn.isPending || createCancellation.isPending ? "Сохраняем…" : tab === "returns" ? "Создать возврат" : "Запросить отмену"}</button>
      </form>
    </section>
    {tab === "cancellations" ? <section className="panel social-onboarding"><div><h3>Безопасное выполнение</h3><p>После создания отмена получает статус «Запрошен». Не повторяйте запрос при тайм-ауте: используйте тот же ключ идемпотентности и проверяйте результат сверки.</p></div></section> : null}
    {tab === "returns" ? <>
    <div className="catalog-tabs" role="tablist" aria-label="Фильтр возвратов">
      <button type="button" role="tab" aria-selected={!status} className={!status ? "active" : ""} onClick={() => setStatus("")}>Все</button>
      {Object.entries(statusLabels).map(([value, label]) => <button type="button" role="tab" aria-selected={status === value} className={status === value ? "active" : ""} key={value} onClick={() => setStatus(value)}>{label}</button>)}
    </div>
    {returns.isPending ? <LoadingBlock/> : returns.isError ? <ErrorBlock retry={() => void returns.refetch()}>Не удалось загрузить возвраты.</ErrorBlock> : rows.length === 0 && !status ? <EmptyState title="Возвратов пока нет" text="Создайте возврат через API или дождитесь события от канала продаж."/> : <DataTable rows={rows} columns={columns} rowKey={(item) => item.id} searchPlaceholder="Возврат, заказ, причина…" empty="По выбранному фильтру возвратов нет" onOpen={(item) => navigate(`/returns/${encodeURIComponent(item.id)}`)}/>} 
    <Drawer open={!!selectedID} title={selected ? `Возврат ${selected.id}` : "Возврат"} subtitle={selected ? `Заказ ${selected.order_id}` : undefined} onClose={() => navigate("/returns")}>
      {details.isPending ? <LoadingBlock/> : details.isError ? <ErrorBlock retry={() => void details.refetch()}>Не удалось открыть возврат.</ErrorBlock> : selected ? <>
        <div className="drawer-kpis"><div><small>Статус</small><StatusBadge value={selected.status}/></div><div><small>Позиции</small><strong>{details.data.items.length}</strong></div><div><small>Доставка и налог</small><strong>{money(selected.requested_shipping_minor + selected.requested_tax_minor, selected.currency)}</strong></div></div>
        {canWrite && actions.length > 0 ? <section className="drawer-section order-actions"><h3>Действия</h3><p className="drawer-help">Переход проверяется по текущей версии возврата, записывается в журнал аудита и публикуется в очереди событий.</p><div className="button-row">{actions.map((action) => <button type="button" key={action.status} className={`button ${action.danger ? "danger" : "primary"}`} disabled={changeStatus.isPending} onClick={() => changeStatus.mutate({id: selected.id, status: action.status, version: selected.version})}>{changeStatus.isPending ? "Сохраняем…" : action.label}</button>)}</div></section> : null}
        <section className="drawer-section"><h3>Причина и источник</h3><dl className="detail-list"><div><dt>Причина</dt><dd>{reasonLabel(selected.reason_code)}</dd></div><div><dt>Источник</dt><dd>{source(selected.source)}</dd></div><div><dt>Создан</dt><dd>{new Date(selected.created_at).toLocaleString("ru-RU")}</dd></div><div><dt>Версия</dt><dd className="mono">{selected.version}</dd></div></dl></section>
        <section className="drawer-section"><h3>Позиции возврата</h3>{details.data.items.length > 0 ? <div className="line-items">{details.data.items.map((item) => <ReturnItemRow key={item.id} item={item}/>)}</div> : <p className="drawer-help">Позиции ещё не добавлены.</p>}<ReturnActions api={api} returnID={selected.id} items={details.data.items} onSaved={() => { void details.refetch(); void queryClient.invalidateQueries({queryKey: ["returns"]}); }} /></section>
      </> : null}
    </Drawer>
    </> : null}
  </Page>;
}

function ReturnActions({api, returnID, items, onSaved}: {api: ReturnsClient; returnID: string; items: ReturnItemHit[]; onSaved: () => void}) {
  const toast = useToast();
  const [orderItemID, setOrderItemID] = useState("");
  const [quantity, setQuantity] = useState("1");
  const [disposition, setDisposition] = useState("restock");
  const [accountID, setAccountID] = useState("");
  const [remoteID, setRemoteID] = useState("");
  const [paymentID, setPaymentID] = useState("");
  const [refundID, setRefundID] = useState("");
  const [amount, setAmount] = useState("");
  const mutation = useMutation({mutationFn: (input: {kind: string; body: unknown}) => {
    const key = uuidV7();
    if (input.kind === "item") return api.createReturnItem({returnId: returnID, idempotencyKey: key, body: input.body});
    if (input.kind === "logistics") return api.createReturnLogistics({returnId: returnID, idempotencyKey: key, body: input.body});
    if (input.kind === "inspection") return api.recordReturnInspection({returnId: returnID, idempotencyKey: key, body: input.body});
    return api.createRefundAllocation({idempotencyKey: key, body: input.body});
  }, onSuccess: () => { toast.push({kind: "success", title: "Операция зарегистрирована", body: "Результат внешней операции и статус платежа подтверждаются фоновой обработкой и сверкой."}); onSaved(); }, onError: () => toast.push({kind: "error", title: "Операция отклонена", body: "Проверьте состояние возврата, версию, доступ к операции и суммы."})});
  const submit = (kind: string, body: unknown) => { if (!mutation.isPending) mutation.mutate({kind, body}); };
  return <div className="catalog-stack" style={{marginTop: "1rem"}}>
    <div><strong>Следующие операции</strong><p className="drawer-help">Технические ответы внешних систем не отображаются. Повторяйте операцию только после проверки текущего состояния.</p></div>
    <div className="social-form-grid">
      <input value={orderItemID} onChange={(event) => setOrderItemID(event.target.value)} placeholder="ID позиции заказа" aria-label="ID позиции заказа" />
      <input type="number" min="1" value={quantity} onChange={(event) => setQuantity(event.target.value)} placeholder="Количество" aria-label="Количество" />
      <select value={disposition} onChange={(event) => setDisposition(event.target.value)} aria-label="Способ обработки товара"><option value="restock">На склад</option><option value="quarantine">Карантин</option><option value="scrap">Утилизация</option><option value="replace">Замена</option></select>
      <button type="button" className="button ghost" disabled={!orderItemID.trim() || mutation.isPending} onClick={() => submit("item", {id: uuidV7(), order_item_id: orderItemID.trim(), unit: "PCS", requested_coefficient: Number(quantity), requested_scale: 0, disposition})}>Добавить позицию</button>
    </div>
    {items.length > 0 ? <div className="social-form-grid"><input value={accountID} onChange={(event) => setAccountID(event.target.value)} placeholder="ID кабинета доставки" aria-label="ID кабинета доставки" /><input value={remoteID} onChange={(event) => setRemoteID(event.target.value)} placeholder="Внешний ID отправления" aria-label="Внешний ID отправления" /><button type="button" className="button ghost" disabled={!accountID.trim() || !remoteID.trim() || mutation.isPending} onClick={() => submit("logistics", {id: uuidV7(), connector_account_id: accountID.trim(), original_remote_id: remoteID.trim(), external_id: uuidV7(), mail_type: "RETURN", tariff_code: 0})}>Запросить этикетку возврата</button></div> : null}
    {items.length > 0 ? <div className="social-form-grid"><input value={paymentID} onChange={(event) => setPaymentID(event.target.value)} placeholder="ID платежа" aria-label="ID платежа" /><input value={refundID} onChange={(event) => setRefundID(event.target.value)} placeholder="ID возврата средств" aria-label="ID возврата средств" /><input type="number" min="1" value={amount} onChange={(event) => setAmount(event.target.value)} placeholder="Сумма, коп." aria-label="Сумма возврата" /><button type="button" className="button ghost" disabled={!paymentID.trim() || !refundID.trim() || !amount || mutation.isPending} onClick={() => submit("refund", {id: uuidV7(), payment_id: paymentID.trim(), refund_id: refundID.trim(), return_id: returnID, component: "line", currency: "RUB", amount_minor: Number(amount)})}>Зарезервировать возврат средств</button></div> : null}
  </div>;
}
